#!/bin/bash
# 手动测试自动化脚本
# 用法: ./run_manual_tests.sh

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# 测试结果统计
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

# 日志函数
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((PASSED_TESTS++))
    ((TOTAL_TESTS++))
}

log_error() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((FAILED_TESTS++))
    ((TOTAL_TESTS++))
}

log_warning() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# 检查命令是否存在
check_command() {
    if ! command -v $1 &> /dev/null; then
        log_error "命令 $1 不存在，请先安装"
        exit 1
    fi
}

# 等待端口就绪
wait_for_port() {
    local port=$1
    local timeout=10
    local count=0

    while ! nc -z localhost $port 2>/dev/null; do
        sleep 1
        ((count++))
        if [ $count -ge $timeout ]; then
            log_error "端口 $port 在 ${timeout}s 内未就绪"
            return 1
        fi
    done
    return 0
}

# 清理函数
cleanup() {
    log_info "清理测试环境..."
    pkill -f "mockllm|sproxy|cproxy" 2>/dev/null || true
    sleep 1
}

# 设置清理陷阱
trap cleanup EXIT

echo "=========================================="
echo "  PairProxy 手动测试套件"
echo "=========================================="
echo ""

# 检查必需的命令
log_info "检查必需的命令..."
check_command "curl"
check_command "jq"

# 清理旧进程
cleanup

# ==========================================
# 阶段 1: 启动服务
# ==========================================
echo ""
log_info "阶段 1: 启动服务"
echo "=========================================="

log_info "启动 mockllm..."
./mockllm.exe --addr :11434 > /tmp/mockllm.log 2>&1 &
MOCKLLM_PID=$!
if wait_for_port 11434; then
    log_success "mockllm 启动成功 (PID: $MOCKLLM_PID)"
else
    log_error "mockllm 启动失败"
    exit 1
fi

log_info "启动 sproxy..."
./sproxy.exe start --config test-sproxy.yaml > /tmp/sproxy.log 2>&1 &
SPROXY_PID=$!
if wait_for_port 9000; then
    log_success "sproxy 启动成功 (PID: $SPROXY_PID)"
else
    log_error "sproxy 启动失败"
    exit 1
fi

log_info "启动 cproxy..."
./cproxy.exe start --config test-cproxy.yaml > /tmp/cproxy.log 2>&1 &
CPROXY_PID=$!
if wait_for_port 8080; then
    log_success "cproxy 启动成功 (PID: $CPROXY_PID)"
else
    log_error "cproxy 启动失败"
    exit 1
fi

# ==========================================
# 阶段 2: 用户登录
# ==========================================
echo ""
log_info "阶段 2: 用户认证"
echo "=========================================="

log_info "登录 cproxy..."
if echo -e "testuser\ntestpass123" | ./cproxy.exe login --server http://localhost:9000 > /tmp/login.log 2>&1; then
    log_success "登录成功"
else
    log_error "登录失败"
    cat /tmp/login.log
    exit 1
fi

# 获取 token
if [ -f "$HOME/.config/pairproxy/token.json" ] || [ -f "$HOME/AppData/Roaming/pairproxy/token.json" ]; then
    log_success "Token 文件已保存"
else
    log_error "Token 文件未找到"
    exit 1
fi

# ==========================================
# 阶段 3: 基础功能测试
# ==========================================
echo ""
log_info "阶段 3: 基础功能测试"
echo "=========================================="

log_info "测试 3.1: 流式请求"
if ./mockagent.exe --url http://localhost:8080 --count 10 --stream > /tmp/test_stream.log 2>&1; then
    log_success "流式请求测试通过 (10/10)"
else
    log_error "流式请求测试失败"
fi

log_info "测试 3.2: 非流式请求"
if ./mockagent.exe --url http://localhost:8080 --count 10 --stream=false > /tmp/test_nonstream.log 2>&1; then
    log_success "非流式请求测试通过 (10/10)"
else
    log_error "非流式请求测试失败"
fi

log_info "测试 3.3: 并发请求"
if ./mockagent.exe --url http://localhost:8080 --count 20 --concurrency 5 > /tmp/test_concurrent.log 2>&1; then
    log_success "并发请求测试通过 (20/20, 5并发)"
else
    log_error "并发请求测试失败"
fi

# ==========================================
# 阶段 4: 健康检查测试
# ==========================================
echo ""
log_info "阶段 4: 健康检查测试"
echo "=========================================="

log_info "测试 4.1: mockllm 健康检查"
if curl -s http://localhost:11434/health | jq -e '.status == "ok"' > /dev/null; then
    log_success "mockllm 健康检查通过"
else
    log_error "mockllm 健康检查失败"
fi

log_info "测试 4.2: sproxy 健康检查"
if curl -s http://localhost:9000/health | jq -e '.status == "ok"' > /dev/null; then
    log_success "sproxy 健康检查通过"
else
    log_error "sproxy 健康检查失败"
fi

log_info "测试 4.3: cproxy 健康检查"
if curl -s http://localhost:8080/health | jq -e '.status == "ok"' > /dev/null; then
    log_success "cproxy 健康检查通过"
else
    log_error "cproxy 健康检查失败"
fi

# ==========================================
# 阶段 5: 认证测试
# ==========================================
echo ""
log_info "阶段 5: 认证测试"
echo "=========================================="

log_info "测试 5.1: 无效 Token 请求"
RESPONSE=$(curl -s -w "\n%{http_code}" -X POST http://localhost:8080/v1/messages \
  -H "Authorization: Bearer invalid-token-12345" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-5-sonnet-20241022","max_tokens":100,"messages":[{"role":"user","content":"test"}]}')
HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
if [ "$HTTP_CODE" = "401" ]; then
    log_success "无效 Token 正确返回 401"
else
    log_error "无效 Token 未返回 401 (实际: $HTTP_CODE)"
fi

# ==========================================
# 阶段 6: 压力测试
# ==========================================
echo ""
log_info "阶段 6: 压力测试"
echo "=========================================="

log_info "测试 6.1: 中等并发 (100请求, 10并发)"
START_TIME=$(date +%s)
if ./mockagent.exe --url http://localhost:8080 --count 100 --concurrency 10 > /tmp/test_stress1.log 2>&1; then
    END_TIME=$(date +%s)
    DURATION=$((END_TIME - START_TIME))
    log_success "中等并发测试通过 (100/100, 耗时: ${DURATION}s)"
else
    log_error "中等并发测试失败"
fi

log_info "测试 6.2: 高并发 (200请求, 20并发)"
START_TIME=$(date +%s)
if ./mockagent.exe --url http://localhost:8080 --count 200 --concurrency 20 > /tmp/test_stress2.log 2>&1; then
    END_TIME=$(date +%s)
    DURATION=$((END_TIME - START_TIME))
    log_success "高并发测试通过 (200/200, 耗时: ${DURATION}s)"
else
    log_error "高并发测试失败"
fi

# ==========================================
# 测试总结
# ==========================================
echo ""
echo "=========================================="
echo "  测试总结"
echo "=========================================="
echo -e "总测试数: ${BLUE}${TOTAL_TESTS}${NC}"
echo -e "通过: ${GREEN}${PASSED_TESTS}${NC}"
echo -e "失败: ${RED}${FAILED_TESTS}${NC}"

if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "\n${GREEN}✓ 所有测试通过！${NC}"
    exit 0
else
    echo -e "\n${RED}✗ 有 ${FAILED_TESTS} 个测试失败${NC}"
    exit 1
fi
