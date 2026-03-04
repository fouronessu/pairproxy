#!/usr/bin/env bash
# =============================================================================
# test-cluster-lb.sh — 双 s-proxy 集群负载均衡压测脚本
#
# 链路: mockagent → cproxy(:8080) → sp-1(:9000) ⟺ sp-2(:9001) → mockllm(:11434)
#
# 逐轮提升并发（1→2→4→8→16→32→64），每轮统计两个节点各承接的请求数量，
# 直观展示负载均衡效果。
#
# 用法:
#   bash test-cluster-lb.sh
#
# 依赖: curl, awk — Windows Git Bash 自带
# =============================================================================
set -euo pipefail

# ---------------------------------------------------------------------------
# 可调参数
# ---------------------------------------------------------------------------
CONCURRENCIES=(1 2 4 8 16 32 64)   # 逐轮并发数
REQUESTS_PER_WORKER=8               # 每个并发槽发送的请求数（总请求 = 并发 × 此值）
WARMUP_REQUESTS=5                   # 暖机请求数（触发路由表推送）
PAYLOAD_LEN=64                      # 随机 payload 字节数

PRIMARY_PORT=9000
WORKER_PORT=9001
CPROXY_PORT=8080
MOCKLLM_PORT=11434

SPROXY_PRIMARY_CFG=test-sproxy-primary.yaml
SPROXY_WORKER_CFG=test-sproxy-worker.yaml
CPROXY_CFG=test-cproxy-cluster.yaml
CLUSTER_DB_PRIMARY=test-cluster-primary.db
CLUSTER_DB_WORKER=test-cluster-worker.db

LOG_DIR=/tmp/pairproxy-cluster-test
mkdir -p "$LOG_DIR"

# ---------------------------------------------------------------------------
# 颜色输出
# ---------------------------------------------------------------------------
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

info()    { echo -e "${CYAN}[INFO]${RESET}  $*"; }
ok()      { echo -e "${GREEN}[ OK ]${RESET}  $*"; }
warn()    { echo -e "${YELLOW}[WARN]${RESET}  $*"; }
err()     { echo -e "${RED}[ERR ]${RESET}  $*" >&2; }
section() { echo -e "\n${BOLD}═══ $* ═══${RESET}"; }

# ---------------------------------------------------------------------------
# 进程 & 端口管理
# ---------------------------------------------------------------------------
kill_port() {
    local port=$1
    local pids
    pids=$(netstat -ano 2>/dev/null | awk "/:${port} /{print \$5}" | sort -u)
    for pid in $pids; do
        [[ "$pid" =~ ^[0-9]+$ ]] || continue
        powershell -Command "Stop-Process -Id $pid -Force -ErrorAction SilentlyContinue" 2>/dev/null || true
    done
}

wait_for_http() {
    local url=$1 label=$2 timeout=${3:-15}
    local i=0
    while ! curl -sf "$url" >/dev/null 2>&1; do
        sleep 1
        ((i++))
        if [[ $i -ge $timeout ]]; then
            err "$label 未在 ${timeout}s 内就绪: $url"
            return 1
        fi
    done
    ok "$label 就绪"
}

# ---------------------------------------------------------------------------
# 请求计数：统计 sproxy 日志文件中 "proxying request to LLM" 出现次数
# 该行每成功代理一次 LLM 请求就写入一次 (debug 级别)，不受缓存影响。
# ---------------------------------------------------------------------------
count_proxied() {
    local logfile=$1
    grep -c '"proxying request to LLM"' "$logfile" 2>/dev/null || echo 0
}

# ---------------------------------------------------------------------------
# 清理（脚本退出时）
# ---------------------------------------------------------------------------
cleanup() {
    section "清理进程"
    for port in $MOCKLLM_PORT $PRIMARY_PORT $WORKER_PORT $CPROXY_PORT; do
        kill_port "$port" && info "已关闭端口 $port 上的进程" || true
    done
    info "日志保存在 $LOG_DIR"
}
trap cleanup EXIT

# ===========================================================================
# 主流程开始
# ===========================================================================
section "环境准备"

# 1. 杀掉残留进程
info "清理旧进程..."
for port in $MOCKLLM_PORT $PRIMARY_PORT $WORKER_PORT $CPROXY_PORT; do
    kill_port "$port"
done
sleep 1

# 2. 清理旧数据库 & 路由缓存（保证干净起点）
info "删除旧测试数据库..."
for db in "$CLUSTER_DB_PRIMARY" "$CLUSTER_DB_WORKER"; do
    rm -f "$db" "${db}-wal" "${db}-shm"
done

info "清理 cproxy 路由缓存（如存在）..."
ROUTING_CACHE="$APPDATA/pairproxy/routing-cache.json"
if [[ -f "$ROUTING_CACHE" ]]; then
    rm -f "$ROUTING_CACHE"
    info "  已删除: $ROUTING_CACHE"
fi

# 3. 编译
info "编译所有二进制..."
"C:/Program Files/Go/bin/go.exe" build ./... 2>&1 | sed 's/^/  /'
ok "编译完成"

# ---------------------------------------------------------------------------
section "启动服务链路"
# ---------------------------------------------------------------------------

# mockllm
info "启动 mockllm (:$MOCKLLM_PORT)..."
./mockllm.exe --addr ":$MOCKLLM_PORT" \
    > "$LOG_DIR/mockllm.log" 2>&1 &
wait_for_http "http://127.0.0.1:$MOCKLLM_PORT/health" "mockllm"

# sproxy primary (sp-1)
info "启动 sproxy primary (:$PRIMARY_PORT)..."
./sproxy.exe start --config "$SPROXY_PRIMARY_CFG" \
    > "$LOG_DIR/sproxy-primary.log" 2>&1 &
wait_for_http "http://127.0.0.1:$PRIMARY_PORT/health" "sproxy primary" 10

# sproxy worker (sp-2)
info "启动 sproxy worker (:$WORKER_PORT)..."
./sproxy.exe start --config "$SPROXY_WORKER_CFG" \
    > "$LOG_DIR/sproxy-worker.log" 2>&1 &
wait_for_http "http://127.0.0.1:$WORKER_PORT/health" "sproxy worker" 10

# 等待 worker 向 primary 注册（report_interval = 3s，等 6s 确保至少一次心跳）
info "等待 worker 完成心跳注册 (≤6s)..."
sleep 6

# ---------------------------------------------------------------------------
section "创建测试用户 & 登录"
# ---------------------------------------------------------------------------

info "在 primary 数据库中创建测试用户 testuser..."
./sproxy.exe admin user add testuser \
    --password testpass123 \
    --config "$SPROXY_PRIMARY_CFG" 2>&1 | sed 's/^/  /' || warn "用户可能已存在，继续..."

info "cproxy 登录 primary..."
printf "testuser\ntestpass123\n" | ./cproxy.exe login \
    --server "http://127.0.0.1:$PRIMARY_PORT" 2>&1 | sed 's/^/  /'
ok "登录完成"

# 启动 cproxy
info "启动 cproxy (:$CPROXY_PORT)..."
./cproxy.exe start --config "$CPROXY_CFG" \
    > "$LOG_DIR/cproxy.log" 2>&1 &
wait_for_http "http://127.0.0.1:$CPROXY_PORT/health" "cproxy" 10

# ---------------------------------------------------------------------------
section "暖机（触发路由表推送）"
# ---------------------------------------------------------------------------

info "发送 $WARMUP_REQUESTS 条暖机请求，使 cproxy 接收 primary 路由表更新..."
./mockagent.exe \
    --url "http://127.0.0.1:$CPROXY_PORT" \
    --count $WARMUP_REQUESTS \
    --concurrency 1 \
    --len $PAYLOAD_LEN 2>&1 | sed 's/^/  /'

# 验证两个节点均已获得流量（cproxy 已知两个节点）
P0=$(count_proxied "$LOG_DIR/sproxy-primary.log")
W0=$(count_proxied "$LOG_DIR/sproxy-worker.log")
info "暖机后计数 — primary: $P0  worker: $W0"

# ---------------------------------------------------------------------------
section "负载均衡压测（逐轮升并发）"
# ---------------------------------------------------------------------------

echo ""
printf "${BOLD}%-12s %-10s %-10s %-10s %-8s %-8s %-8s${RESET}\n" \
    "并发数" "总请求" "PASS" "FAIL" "primary" "worker" "分布比"
printf '%s\n' "────────────────────────────────────────────────────────────────────"

TOTAL_P=0
TOTAL_W=0

for CONC in "${CONCURRENCIES[@]}"; do
    COUNT=$(( CONC * REQUESTS_PER_WORKER ))

    # 读取本轮前日志行数
    P_BEFORE=$(count_proxied "$LOG_DIR/sproxy-primary.log")
    W_BEFORE=$(count_proxied "$LOG_DIR/sproxy-worker.log")

    # 运行 mockagent
    RESULT=$(./mockagent.exe \
        --url "http://127.0.0.1:$CPROXY_PORT" \
        --count $COUNT \
        --concurrency $CONC \
        --len $PAYLOAD_LEN 2>&1)

    # 解析结果（Total: N  Pass: N  Fail: N  Error: N  Time: Xms）
    PASS=$(echo "$RESULT" | awk '/^Total:/{print $4}')
    FAIL=$(echo "$RESULT" | awk '/^Total:/{print $6}')
    PASS=${PASS:-0}; FAIL=${FAIL:-0}

    # 读取本轮后日志行数（日志是同步写入，无需额外等待）
    P_AFTER=$(count_proxied "$LOG_DIR/sproxy-primary.log")
    W_AFTER=$(count_proxied "$LOG_DIR/sproxy-worker.log")

    P_DELTA=$(( P_AFTER - P_BEFORE ))
    W_DELTA=$(( W_AFTER - W_BEFORE ))
    TOTAL_P=$(( TOTAL_P + P_DELTA ))
    TOTAL_W=$(( TOTAL_W + W_DELTA ))

    # 计算分布比（避免除零）
    HANDLED=$(( P_DELTA + W_DELTA ))
    if [[ $HANDLED -gt 0 ]]; then
        P_PCT=$(awk "BEGIN{printf \"%.0f\", $P_DELTA*100/$HANDLED}")
        W_PCT=$(awk "BEGIN{printf \"%.0f\", $W_DELTA*100/$HANDLED}")
    else
        P_PCT=0; W_PCT=0
    fi

    # 结果状态颜色
    if [[ "$FAIL" == "0" ]]; then
        STATUS="${GREEN}✓${RESET}"
    else
        STATUS="${RED}✗${RESET}"
    fi

    printf "%-12s %-10s %-10s %-10s %-8s %-8s %s\n" \
        "$CONC" \
        "$COUNT" \
        "${GREEN}${PASS}${RESET}" \
        "$( [[ "$FAIL" == "0" ]] && echo "${GREEN}${FAIL}${RESET}" || echo "${RED}${FAIL}${RESET}" )" \
        "${P_DELTA}(${P_PCT}%)" \
        "${W_DELTA}(${W_PCT}%)" \
        "$STATUS"
done

printf '%s\n' "────────────────────────────────────────────────────────────────────"

# 最终汇总
ALL=$(( TOTAL_P + TOTAL_W ))
if [[ $ALL -gt 0 ]]; then
    FP=$(awk "BEGIN{printf \"%.1f\", $TOTAL_P*100/$ALL}")
    FW=$(awk "BEGIN{printf \"%.1f\", $TOTAL_W*100/$ALL}")
else
    FP=0; FW=0
fi

echo ""
echo -e "${BOLD}全程汇总（不含暖机）:${RESET}"
echo -e "  primary 承接: ${TOTAL_P} 请求 (${FP}%)"
echo -e "  worker  承接: ${TOTAL_W} 请求 (${FW}%)"
echo -e "  合计:         ${ALL} 请求"

if [[ $ALL -gt 0 && $TOTAL_P -gt 0 && $TOTAL_W -gt 0 ]]; then
    echo ""
    ok "负载均衡工作正常 — 两个节点均已承接流量"
else
    warn "注意: 未检测到双节点流量分布，请检查 worker 注册状态"
    echo "  sproxy-primary 日志: $LOG_DIR/sproxy-primary.log"
    echo "  sproxy-worker  日志: $LOG_DIR/sproxy-worker.log"
fi
