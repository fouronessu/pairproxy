"""
vLLM 长短请求分流路由（带会话亲和性）
====================================
路由策略:
  短请求 (< SPLIT_THRESHOLD token): 最少连接数路由到短池，无会话绑定
  长请求 (>= SPLIT_THRESHOLD token): 会话亲和路由到长池，新会话按最少连接数选实例

启动方式:
    pip install fastapi uvicorn httpx
    python vllm_router.py
"""

import asyncio
import json
import logging
import time
from collections import OrderedDict
from contextlib import asynccontextmanager

import httpx
from fastapi import FastAPI, Request, Response
from fastapi.responses import StreamingResponse

# ============================================================
# CONFIG - 根据实际环境修改
# ============================================================

# 短请求池（input < SPLIT_THRESHOLD token）
# 建议配置: max-model-len 32768, max-num-seqs 20
SHORT_POOL = [
    "http://10.195.176.130:1025",
]

# 长请求池（input >= SPLIT_THRESHOLD token）
# 建议配置: max-model-len 163840, max-num-seqs 8
LONG_POOL = [
    "http://10.195.176.45:1025",
    "http://10.195.176.192:1025",
]

# 分流阈值（估算 token 数）
SPLIT_THRESHOLD = 30000

# 每个字符约等于多少 token（代码约 0.3，中文约 0.6）
CHARS_PER_TOKEN = 0.3

# 路由服务监听
LISTEN_HOST = "0.0.0.0"
LISTEN_PORT = 3035

# 超时（秒）
REQUEST_TIMEOUT = 600

# 健康检查间隔（秒）
HEALTH_CHECK_INTERVAL = 10

# 长请求会话亲和性
SESSION_TTL = 3600          # 会话过期时间（秒）
SESSION_TABLE_MAX = 10000   # 最大会话条目数

# ============================================================

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
)
logger = logging.getLogger("vllm-router")

# 全局共享 HTTP client（lifespan 初始化）
http_client: httpx.AsyncClient | None = None

# 各后端当前活跃请求数（用于最少连接数路由）
active_conns: dict[str, int] = {}


def conn_acquire(backend: str) -> None:
    active_conns[backend] = active_conns.get(backend, 0) + 1


def conn_release(backend: str) -> None:
    active_conns[backend] = max(0, active_conns.get(backend, 0) - 1)


# ============================================================
# 会话亲和表（LRU + TTL）—— 仅用于长请求
# ============================================================

class SessionTable:
    """
    会话路由表: session_id → backend_url
    - LRU 淘汰：超过 max_size 时删最久未用的
    - TTL 过期：超过 ttl 秒没请求自动释放
    - 后端故障：自动清除该后端的绑定
    """

    def __init__(self, max_size: int = SESSION_TABLE_MAX, ttl: int = SESSION_TTL):
        self.max_size = max_size
        self.ttl = ttl
        self._table: OrderedDict[str, tuple[str, float]] = OrderedDict()
        self.hits = 0
        self.misses = 0

    def get(self, session_id: str) -> str | None:
        if session_id not in self._table:
            self.misses += 1
            return None
        backend, last_access = self._table[session_id]
        if time.time() - last_access > self.ttl:
            del self._table[session_id]
            self.misses += 1
            return None
        self._table[session_id] = (backend, time.time())
        self._table.move_to_end(session_id)
        self.hits += 1
        return backend

    def set(self, session_id: str, backend: str):
        if session_id not in self._table and len(self._table) >= self.max_size:
            self._table.popitem(last=False)
        self._table[session_id] = (backend, time.time())
        self._table.move_to_end(session_id)

    def remove_backend(self, backend: str):
        to_remove = [s for s, (b, _) in self._table.items() if b == backend]
        for s in to_remove:
            del self._table[s]
        if to_remove:
            logger.info(f"清除 {len(to_remove)} 个绑定到 {backend} 的会话")

    def cleanup_expired(self):
        now = time.time()
        to_remove = [s for s, (_, t) in self._table.items() if now - t > self.ttl]
        for s in to_remove:
            del self._table[s]

    @property
    def size(self) -> int:
        return len(self._table)

    @property
    def hit_rate(self) -> float:
        total = self.hits + self.misses
        return self.hits / total if total > 0 else 0.0


session_table = SessionTable()
healthy_short: set[str] = set()
healthy_long: set[str] = set()


# ============================================================
# 会话 ID 提取（仅用于长请求）
# ============================================================

def extract_session_id(request: Request) -> str | None:
    """
    优先级:
    1. X-Session-ID  — sproxy 从 X-Claude-Code-Session-Id 转换透传，精确到单个对话
    2. X-User-ID     — sproxy 始终透传的用户身份，无会话时做保底亲和（同用户共享前缀缓存）
    3. None          — 两者均缺失，长请求降级为最少连接数路由，不绑定
    """
    if sid := request.headers.get("X-Session-ID"):
        return sid
    if uid := request.headers.get("X-User-ID"):
        return f"user:{uid}"
    return None


# ============================================================
# Token 估算 & 路由
# ============================================================

def _count_chars(content) -> int:
    """统计 content 字段的字符数，支持字符串和 text block 列表。"""
    if isinstance(content, str):
        return len(content)
    if isinstance(content, list):
        return sum(
            len(block.get("text", ""))
            for block in content
            if isinstance(block, dict) and block.get("type") == "text"
        )
    return 0


def estimate_tokens(body: dict) -> int:
    total_chars = 0

    # system prompt（Anthropic 格式顶层字段；OpenAI 格式 system 在 messages 里）
    total_chars += _count_chars(body.get("system", ""))

    for msg in body.get("messages", []):
        # content 字段：字符串或 text block 列表
        total_chars += _count_chars(msg.get("content", ""))
        # OpenAI 格式工具调用（sproxy AtoO 转换后 tool_use → tool_calls）
        for tc in msg.get("tool_calls", []):
            fn = tc.get("function", {})
            total_chars += len(fn.get("name", ""))
            total_chars += len(fn.get("arguments", ""))

    # tools 定义（name + description + parameters schema）
    for tool in body.get("tools", []):
        total_chars += len(json.dumps(tool))

    return int(total_chars * CHARS_PER_TOKEN)


def pick_least_conn(pool: list[str], healthy: set[str]) -> str | None:
    """从健康实例中选活跃连接数最少的后端。"""
    candidates = [u for u in pool if u in healthy]
    if not candidates:
        return None
    return min(candidates, key=lambda u: active_conns.get(u, 0))


def pick_target(est_tokens: int, session_id: str | None) -> str | None:
    """
    路由决策:
    - 短请求: 最少连接数选短池实例，不做会话绑定
    - 长请求: 查会话亲和 → 命中且健康则复用；未命中则最少连接数选长池实例并绑定
    """
    if est_tokens < SPLIT_THRESHOLD:
        # 短请求：直接按连接数路由，不涉及会话表
        return pick_least_conn(SHORT_POOL, healthy_short)

    # 长请求：优先复用已绑定实例（prefix cache 命中）
    if session_id:
        cached = session_table.get(session_id)
        if cached and cached in healthy_long:
            return cached

    target = pick_least_conn(LONG_POOL, healthy_long)
    if target and session_id:
        session_table.set(session_id, target)
        logger.info(
            f"新绑定: session={session_id[:24]}, target={target}, tokens≈{est_tokens}"
        )
    return target


# ============================================================
# 健康检查
# ============================================================

async def check_health(url: str) -> bool:
    try:
        resp = await http_client.get(f"{url}/health", timeout=5)
        return resp.status_code == 200
    except Exception:
        return False


async def run_health_checks():
    for url in SHORT_POOL:
        if await check_health(url):
            healthy_short.add(url)
        elif url in healthy_short:
            healthy_short.discard(url)
            logger.warning(f"实例不健康: {url}")

    for url in LONG_POOL:
        if await check_health(url):
            healthy_long.add(url)
        elif url in healthy_long:
            healthy_long.discard(url)
            session_table.remove_backend(url)
            logger.warning(f"实例不健康: {url}")

    session_table.cleanup_expired()

    logger.info(
        f"状态 - 短池:{len(healthy_short)}/{len(SHORT_POOL)} "
        f"长池:{len(healthy_long)}/{len(LONG_POOL)} "
        f"会话:{session_table.size} "
        f"亲和命中:{session_table.hit_rate:.0%} "
        f"连接数:{dict(active_conns)}"
    )


async def health_check_loop():
    while True:
        await asyncio.sleep(HEALTH_CHECK_INTERVAL)
        try:
            await run_health_checks()
        except Exception:
            logger.exception("健康检查异常，下次重试")


# ============================================================
# FastAPI
# ============================================================

@asynccontextmanager
async def lifespan(app: FastAPI):
    global http_client
    http_client = httpx.AsyncClient(timeout=REQUEST_TIMEOUT)
    logger.info(f"路由启动 | 短池:{SHORT_POOL} 长池:{LONG_POOL} 阈值:{SPLIT_THRESHOLD}tok")
    await run_health_checks()
    task = asyncio.create_task(health_check_loop())
    yield
    task.cancel()
    try:
        await task
    except asyncio.CancelledError:
        pass
    await http_client.aclose()


app = FastAPI(lifespan=lifespan)


@app.post("/v1/chat/completions")
async def chat_completions(request: Request):
    body = await request.json()
    est_tokens = estimate_tokens(body)

    # 短请求不提取 session_id；长请求无 X-Session-ID 时也不绑定（降级为最少连接数）
    is_long = est_tokens >= SPLIT_THRESHOLD
    session_id = extract_session_id(request) if is_long else None

    target = pick_target(est_tokens, session_id)

    if not target:
        pool_name = "长" if is_long else "短"
        return Response(
            content=json.dumps({"error": f"{pool_name}池没有可用的后端实例"}),
            status_code=503,
            media_type="application/json",
        )

    extra_headers = {
        "X-Routed-To": target,
        "X-Est-Tokens": str(est_tokens),
    }
    if session_id:
        extra_headers["X-Session-ID"] = session_id[:50]

    stream = body.get("stream", False)

    # 透传客户端的 Authorization 头，vLLM 如果配置了 --api-key 或 VLLM_API_KEY 环境变量时需要
    upstream_headers: dict[str, str] = {"Content-Type": "application/json"}
    if auth := request.headers.get("Authorization"):
        upstream_headers["Authorization"] = auth

    conn_acquire(target)

    if stream:
        stream_client = httpx.AsyncClient(timeout=REQUEST_TIMEOUT)
        try:
            resp = await stream_client.send(
                stream_client.build_request(
                    "POST",
                    f"{target}/v1/chat/completions",
                    json=body,
                    headers=upstream_headers,
                ),
                stream=True,
            )
        except Exception:
            conn_release(target)
            await stream_client.aclose()
            raise

        if resp.status_code != 200:
            body_bytes = await resp.aread()
            await resp.aclose()
            await stream_client.aclose()
            conn_release(target)
            return Response(
                content=body_bytes,
                status_code=resp.status_code,
                media_type="application/json",
                headers=extra_headers,
            )

        async def gen():
            try:
                async for chunk in resp.aiter_bytes():
                    yield chunk
            finally:
                await resp.aclose()
                await stream_client.aclose()
                conn_release(target)

        return StreamingResponse(
            gen(),
            status_code=resp.status_code,
            media_type="text/event-stream",
            headers={"Cache-Control": "no-cache", **extra_headers},
        )
    else:
        try:
            resp = await http_client.post(
                f"{target}/v1/chat/completions",
                json=body,
                headers=upstream_headers,
            )
        finally:
            conn_release(target)
        return Response(
            content=resp.content,
            status_code=resp.status_code,
            media_type="application/json",
            headers=extra_headers,
        )


@app.get("/v1/models")
async def list_models():
    all_h = list(healthy_short) + list(healthy_long)
    if not all_h:
        return Response(content='{"error":"no backends"}', status_code=503)
    resp = await http_client.get(f"{all_h[0]}/v1/models", timeout=10)
    return Response(content=resp.content, status_code=resp.status_code,
                    media_type="application/json")


@app.get("/health")
async def health():
    total = len(healthy_short) + len(healthy_long)
    if total == 0:
        return Response(content='{"status":"unhealthy"}', status_code=503)
    return {
        "status": "healthy",
        "short_pool": {"total": len(SHORT_POOL), "healthy": len(healthy_short)},
        "long_pool": {"total": len(LONG_POOL), "healthy": len(healthy_long)},
        "sessions": session_table.size,
        "affinity_hit_rate": f"{session_table.hit_rate:.0%}",
    }


@app.get("/stats")
async def stats():
    return {
        "split_threshold": SPLIT_THRESHOLD,
        "sessions": session_table.size,
        "session_max": SESSION_TABLE_MAX,
        "session_ttl": SESSION_TTL,
        "affinity_hits": session_table.hits,
        "affinity_misses": session_table.misses,
        "affinity_hit_rate": f"{session_table.hit_rate:.0%}",
        "short_pool": {
            "instances": SHORT_POOL,
            "healthy": list(healthy_short),
            "active_conns": {u: active_conns.get(u, 0) for u in SHORT_POOL},
        },
        "long_pool": {
            "instances": LONG_POOL,
            "healthy": list(healthy_long),
            "active_conns": {u: active_conns.get(u, 0) for u in LONG_POOL},
        },
    }


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host=LISTEN_HOST, port=LISTEN_PORT,
                log_level="info", timeout_keep_alive=600)
