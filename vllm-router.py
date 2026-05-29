"""
vLLM 长短请求分流路由（带会话亲和性 + 短池空闲借用）
====================================================
路由策略:
  短请求 (< SPLIT_THRESHOLD token):
    → 短池，最少总连接数

  长请求 (>= SPLIT_THRESHOLD token):
    1. 会话亲和优先（短池或长池均可）
       - 亲和节点在长池且健康 → 直接复用
       - 亲和节点在短池且健康且 short_pool_long_conns < MAX_LONG_OVERFLOW → 复用
       - 否则重新选
    2. 无亲和（或亲和节点不可用）→ 构建合并候选池：
         长池健康节点
       + 短池健康节点（当且仅当 short_pool_short_conns==0 AND short_pool_long_conns<3）
       → 候选池中按最少活跃长请求数选实例，绑定会话

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
# 实际配置: max-model-len 192K, max-num-seqs 20（高并发，承载大量中小请求）
SHORT_POOL = [
    "http://10.195.176.130:1025",
]

# 长请求池（input >= SPLIT_THRESHOLD token）
# 建议配置: max-model-len 163840, max-num-seqs 8（低并发，承载大上下文请求）
LONG_POOL = [
    "http://10.195.176.109:1025",
    "http://10.195.176.192:1025",
]

# 分流阈值（估算 token 数）：长/短请求的负载分配分界，非容量上限。
# 短池 ctx 已达 192K 足以承载，阈值仅用于把大请求导向低并发的长池、避免拖慢短池高并发吞吐。
SPLIT_THRESHOLD = 32768 + 16384  # ≈49K，经验分流点

# 短池允许同时承载的最大长请求溢出数
MAX_LONG_OVERFLOW = 3

# 转发给 vLLM 时统一使用的模型名（必须与 --served-model-name 一致）
VLLM_MODEL_NAME = "MiniMax-M2.7"

# 路由服务监听
LISTEN_HOST = "0.0.0.0"
LISTEN_PORT = 3035

# 超时（秒）
REQUEST_TIMEOUT = 600

# 上游连接级错误（keep-alive 复用到已被服务端关闭的连接 / 建连失败）的重试次数。
# 仅在尚未收到任何响应字节时重试，对 LLM completions 而言重复一次是可接受的。
UPSTREAM_CONNECT_RETRIES = 2

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

# 各后端当前活跃总请求数
active_conns: dict[str, int] = {}

# 各后端当前活跃长请求数（用于长请求合并候选池的最少连接排序）
long_active_conns: dict[str, int] = {}

# 短池粒度计数（用于溢出门控）
short_pool_short_conns: int = 0   # 短池中活跃的短请求数
short_pool_long_conns:  int = 0   # 短池中活跃的长请求（溢出）数


def short_req_acquire(backend: str) -> None:
    global short_pool_short_conns
    active_conns[backend] = active_conns.get(backend, 0) + 1
    short_pool_short_conns += 1


def short_req_release(backend: str) -> None:
    global short_pool_short_conns
    active_conns[backend] = max(0, active_conns.get(backend, 0) - 1)
    short_pool_short_conns = max(0, short_pool_short_conns - 1)


def long_req_acquire(backend: str, is_overflow: bool) -> None:
    global short_pool_long_conns
    active_conns[backend] = active_conns.get(backend, 0) + 1
    long_active_conns[backend] = long_active_conns.get(backend, 0) + 1
    if is_overflow:
        short_pool_long_conns += 1


def long_req_release(backend: str, is_overflow: bool) -> None:
    global short_pool_long_conns
    active_conns[backend] = max(0, active_conns.get(backend, 0) - 1)
    long_active_conns[backend] = max(0, long_active_conns.get(backend, 0) - 1)
    if is_overflow:
        short_pool_long_conns = max(0, short_pool_long_conns - 1)


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

_short_pool_set = set(SHORT_POOL)


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

def chars_to_tokens(text: str) -> int:
    """按中文字符占比动态把字符数换算为 token：中文约 2 字符/token，英文/代码约 4 字符/token。"""
    if not text:
        return 0
    cjk = sum(1 for c in text if '一' <= c <= '鿿')
    ratio = cjk / len(text)
    divisor = 4 - 2 * ratio  # ratio=0 → 4，ratio=1 → 2
    return max(int(len(text) / divisor), 1)


def _collect_text(content) -> str:
    """提取 content 字段的文本，支持字符串和 text block 列表。"""
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "".join(
            block.get("text", "")
            for block in content
            if isinstance(block, dict) and block.get("type") == "text"
        )
    return ""


def estimate_tokens(body: dict) -> int:
    """估算请求 input token：先汇总所有文本，再用 CJK 感知的 chars_to_tokens 换算。"""
    parts: list[str] = []

    # system prompt（Anthropic 格式顶层字段；OpenAI 格式 system 在 messages 里）
    parts.append(_collect_text(body.get("system", "")))

    for msg in body.get("messages", []):
        # content 字段：字符串或 text block 列表
        parts.append(_collect_text(msg.get("content", "")))
        # OpenAI 格式工具调用（sproxy AtoO 转换后 tool_use → tool_calls）
        for tc in msg.get("tool_calls", []):
            fn = tc.get("function", {})
            parts.append(fn.get("name", ""))
            parts.append(fn.get("arguments", ""))

    # tools 定义（name + description + parameters schema）
    for tool in body.get("tools", []):
        parts.append(json.dumps(tool, ensure_ascii=False))

    return chars_to_tokens("".join(parts))


def pick_least_conn(pool: list[str], healthy: set[str]) -> str | None:
    """从健康实例中选活跃总连接数最少的后端（用于短请求路由）。"""
    candidates = [u for u in pool if u in healthy]
    if not candidates:
        return None
    return min(candidates, key=lambda u: active_conns.get(u, 0))


def pick_target(est_tokens: int, session_id: str | None) -> tuple[str | None, bool]:
    """
    路由决策，返回 (target, is_overflow)。
    is_overflow=True 表示长请求路由到了短池，需要更新短池溢出计数。

    短请求: 短池最少总连接数，不绑定会话。
    长请求:
      1. 会话亲和优先（短池/长池均可）
         - 亲和在长池且健康 → 复用
         - 亲和在短池且健康且未超溢出 cap → 复用（不受短请求阻塞）
      2. 无亲和/亲和失效 → 合并候选池（长池健康节点 + 短池溢出条件满足时）
         → 按最少活跃长请求数选实例，绑定会话
    """
    if est_tokens < SPLIT_THRESHOLD:
        return pick_least_conn(SHORT_POOL, healthy_short), False

    # 阶段一：会话亲和
    if session_id:
        cached = session_table.get(session_id)
        if cached:
            if cached in healthy_long:
                return cached, False
            if cached in healthy_short and cached in _short_pool_set:
                if short_pool_long_conns < MAX_LONG_OVERFLOW:
                    return cached, True
                # cap 已满，放弃亲和，重新选

    # 阶段二：合并候选池，按最少活跃长请求数选实例
    candidates = [u for u in LONG_POOL if u in healthy_long]
    if short_pool_short_conns == 0 and short_pool_long_conns < MAX_LONG_OVERFLOW:
        candidates += [u for u in SHORT_POOL if u in healthy_short]

    if not candidates:
        return None, False

    target = min(candidates, key=lambda u: long_active_conns.get(u, 0))
    is_overflow = target in _short_pool_set

    if session_id:
        session_table.set(session_id, target)
        logger.info(
            f"新绑定: session={session_id[:24]}, target={target}, "
            f"tokens≈{est_tokens}, overflow={is_overflow}"
        )
    return target, is_overflow


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
            session_table.remove_backend(url)
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
        f"短池[短:{short_pool_short_conns} 溢出:{short_pool_long_conns}/{MAX_LONG_OVERFLOW}] "
        f"长请求活跃:{dict(long_active_conns)}"
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
    logger.info(f"路由启动 | 短池:{SHORT_POOL} 长池:{LONG_POOL} 阈值:{SPLIT_THRESHOLD}tok "
                f"最大溢出:{MAX_LONG_OVERFLOW}")
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


# 连接级错误：请求已发出但在收到响应头前连接断开/建连失败，重试通常会换一条新连接成功。
# RemoteProtocolError 多见于复用了服务端已关闭的 keep-alive 连接。
_RETRYABLE_CONN_ERRORS = (
    httpx.RemoteProtocolError,
    httpx.ConnectError,
    httpx.ConnectTimeout,
    httpx.ReadError,
)


async def _send_upstream(
    target: str, body: dict, headers: dict[str, str], stream: bool
) -> httpx.Response:
    """向上游发请求，对连接级错误自动重试。返回 httpx.Response（stream=True 时为未读取的流）。"""
    last_exc: Exception | None = None
    for attempt in range(UPSTREAM_CONNECT_RETRIES + 1):
        try:
            req = http_client.build_request(
                "POST", f"{target}/v1/chat/completions", json=body, headers=headers
            )
            return await http_client.send(req, stream=stream)
        except _RETRYABLE_CONN_ERRORS as e:
            last_exc = e
            logger.warning(
                f"上游连接失败 (尝试 {attempt + 1}/{UPSTREAM_CONNECT_RETRIES + 1}): "
                f"target={target} err={e!r}"
            )
    assert last_exc is not None
    raise last_exc


@app.post("/v1/chat/completions")
async def chat_completions(request: Request):
    body = await request.json()
    est_tokens = estimate_tokens(body)

    is_long = est_tokens >= SPLIT_THRESHOLD
    session_id = extract_session_id(request) if is_long else None

    target, is_overflow = pick_target(est_tokens, session_id)

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

    # 改写模型名：客户端传来的 model 字段可能是 Claude/OpenAI 模型名，vLLM 只认自己的 served-model-name
    body["model"] = VLLM_MODEL_NAME

    stream = body.get("stream", False)

    # 透传客户端的 Authorization 头，vLLM 如果配置了 --api-key 或 VLLM_API_KEY 环境变量时需要
    upstream_headers: dict[str, str] = {"Content-Type": "application/json"}
    if auth := request.headers.get("Authorization"):
        upstream_headers["Authorization"] = auth

    # 按请求类型计数
    if is_long:
        long_req_acquire(target, is_overflow)
    else:
        short_req_acquire(target)

    def release() -> None:
        if is_long:
            long_req_release(target, is_overflow)
        else:
            short_req_release(target)

    # 建立上游连接（含连接级错误重试）。失败转成 502，避免裸 500 + traceback 冒泡给客户端。
    try:
        resp = await _send_upstream(target, body, upstream_headers, stream)
    except Exception as e:
        release()
        logger.error(f"上游请求失败: target={target} err={e!r}")
        return Response(
            content=json.dumps({"error": "upstream request failed", "detail": str(e)}),
            status_code=502,
            media_type="application/json",
            headers=extra_headers,
        )

    if stream:
        if resp.status_code != 200:
            body_bytes = await resp.aread()
            await resp.aclose()
            release()
            logger.warning(f"上游非200: target={target} status={resp.status_code} (stream)")
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
                release()

        return StreamingResponse(
            gen(),
            status_code=resp.status_code,
            media_type="text/event-stream",
            headers={"Cache-Control": "no-cache", **extra_headers},
        )
    else:
        try:
            if resp.status_code != 200:
                logger.warning(f"上游非200: target={target} status={resp.status_code}")
            return Response(
                content=resp.content,
                status_code=resp.status_code,
                media_type="application/json",
                headers=extra_headers,
            )
        finally:
            release()


@app.post("/v1/messages/count_tokens")
async def count_tokens(request: Request):
    """本地粗略预估 input_tokens，与路由分流共用同一套 CJK 感知估算逻辑。"""
    body = await request.json()
    tokens = estimate_tokens(body)
    logger.info(f"👉 count_tokens 本地预估 input_tokens={tokens}")
    return {"input_tokens": tokens}


@app.get("/v1/models")
async def list_models():
    # 优先长池节点（通常支持更大 context），依次 fallback 直到有一个成功
    candidates = [u for u in LONG_POOL if u in healthy_long] + \
                 [u for u in SHORT_POOL if u in healthy_short]
    if not candidates:
        return Response(content='{"error":"no backends"}', status_code=503)
    for url in candidates:
        try:
            resp = await http_client.get(f"{url}/v1/models", timeout=10)
            return Response(content=resp.content, status_code=resp.status_code,
                            media_type="application/json")
        except Exception:
            logger.warning(f"/v1/models 请求失败: {url}")
    return Response(content='{"error":"all backends unavailable"}', status_code=503)


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
            "short_reqs": short_pool_short_conns,
            "long_overflow": short_pool_long_conns,
            "max_overflow": MAX_LONG_OVERFLOW,
        },
        "long_pool": {
            "instances": LONG_POOL,
            "healthy": list(healthy_long),
            "active_conns": {u: active_conns.get(u, 0) for u in LONG_POOL},
            "long_active": {u: long_active_conns.get(u, 0) for u in LONG_POOL},
        },
    }


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host=LISTEN_HOST, port=LISTEN_PORT,
                log_level="info", timeout_keep_alive=600)
