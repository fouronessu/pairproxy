# E2E 测试说明

## 自动化 E2E 测试（当前实现）

使用 `httptest` 在单进程内运行，适合 CI/CD 和快速验证。

```bash
# 运行所有 E2E 测试
go test -v ./test/e2e/...

# 运行特定测试
go test -v ./test/e2e/f10_features_e2e_test.go
go test -v ./test/e2e/openai_compat_e2e_test.go
```

**特点**:
- ✅ 快速（无进程启动开销）
- ✅ 稳定（无端口冲突）
- ✅ 易于调试（可以打断点）
- ✅ CI/CD 友好

---

## 手动 E2E 测试（使用 mockagent + mockllm）

使用独立进程模拟真实部署环境，适合手动测试和压力测试。

### 1. 启动 mockllm（模拟 LLM 服务）

```bash
# 终端 1：启动 mock LLM
./mockllm --addr :11434 --v

# 或使用延迟模拟真实 LLM
./mockllm --addr :11434 --delay 100ms --chunks 5
```

### 2. 启动 sproxy

```bash
# 终端 2：启动 sproxy
./sproxy --config sproxy.yaml
```

确保 `sproxy.yaml` 配置了 mockllm：
```yaml
llm:
  targets:
    - url: "http://localhost:11434"
      api_key: "mock-key"
      provider: "anthropic"
```

### 3. 运行 mockagent（模拟客户端）

```bash
# 终端 3：发送测试请求
./mockagent --url http://localhost:9000 --count 10 --v

# 压力测试
./mockagent --count 1000 --concurrency 50

# 流式测试
./mockagent --stream --count 20

# 非流式测试
./mockagent --stream=false --count 20
```

---

## 完整链路测试

```bash
# 测试完整链路：mockagent → cproxy → sproxy → mockllm

# 1. 启动 mockllm
./mockllm --addr :11434 &

# 2. 启动 sproxy
./sproxy --config sproxy.yaml &

# 3. 启动 cproxy（如果需要）
./cproxy --server http://localhost:9000 &

# 4. 运行测试
./mockagent --url http://localhost:8080 --count 100 --concurrency 10

# 5. 清理
killall mockllm sproxy cproxy
```

---

## 测试场景对比

| 场景 | 自动化测试 (httptest) | 手动测试 (mockagent/mockllm) |
|------|----------------------|------------------------------|
| CI/CD 集成 | ✅ 推荐 | ❌ 复杂 |
| 快速验证 | ✅ 推荐 | ❌ 需要启动进程 |
| 压力测试 | ❌ 不适合 | ✅ 推荐 |
| 真实环境模拟 | ❌ 单进程 | ✅ 推荐 |
| 调试内部状态 | ✅ 可以 | ❌ 难以访问 |
| 手动调试 | ❌ 需要修改代码 | ✅ 推荐 |

---

## 建议

1. **日常开发**: 使用自动化测试（`go test`）
2. **手动验证**: 使用 mockagent/mockllm
3. **压力测试**: 使用 mockagent/mockllm
4. **CI/CD**: 使用自动化测试
5. **生产前验证**: 两种都用

---

## 添加新的 E2E 测试

如果要添加基于 mockagent/mockllm 的自动化测试，可以创建：

```go
// test/e2e/fullchain_with_mock_processes_test.go
// +build integration

func TestFullChainWithMockProcesses(t *testing.T) {
    // 启动 mockllm 进程
    // 启动 sproxy 进程
    // 运行 mockagent
    // 验证结果
}
```

运行：
```bash
go test -v -tags=integration ./test/e2e/...
```
