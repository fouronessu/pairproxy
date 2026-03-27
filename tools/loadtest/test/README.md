# Claude Load Tester - 测试指南

## 测试结构

```
tools/loadtest/
├── internal/
│   ├── prompts/
│   │   ├── loader.go          # 实现
│   │   └── loader_test.go     # 单元测试
│   ├── worker/
│   │   ├── worker.go          # 实现
│   │   └── worker_test.go     # 单元测试
│   ├── metrics/
│   │   ├── collector.go       # 实现
│   │   └── collector_test.go  # 单元测试
│   └── controller/
│       ├── controller.go      # 实现
│       └── controller_test.go # 单元测试
├── cmd/
│   ├── main.go                # CLI 入口
│   └── main_test.go           # 主程序测试
└── test/
    ├── integration/           # 集成测试
    │   └── integration_test.go
    └── e2e/                   # E2E 测试
        └── e2e_test.go
```

## 运行测试

### 单元测试

```bash
# 运行所有单元测试
cd tools/loadtest
go test ./...

# 运行特定包的测试
go test ./internal/prompts
go test ./internal/worker
go test ./internal/metrics
go test ./internal/controller

# 带覆盖率
go test -cover ./...

# 生成覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### 集成测试

```bash
# 构建二进制文件
go build -o claude-load-tester ./cmd

# 运行集成测试
go test -tags=integration ./test/integration/

# 详细输出
go test -v -tags=integration ./test/integration/
```

### E2E 测试

```bash
# 运行 E2E 测试（需要完整环境）
go test -tags=e2e ./test/e2e/

# 详细输出
go test -v -tags=e2e ./test/e2e/
```

## 测试分类

### 单元测试

- **prompts/loader_test.go**: 80+ 行
  - TestNewLoader: 验证从文件加载 prompts
  - TestNewLoaderWithInvalidFile: 验证错误处理
  - TestLoaderGetRandom: 验证随机获取
  - TestLoaderGetRandomFromCategory: 验证分类获取
  - TestLoaderGetCategories: 验证分类列表
  - TestLoaderGetCategoryPrompts: 验证获取 prompts
  - TestDefaultPrompts: 验证默认 prompts

- **worker/worker_test.go**: 130+ 行
  - TestNewWorker: 验证创建 worker
  - TestWorkerStartStop: 验证启动和停止
  - TestWorkerGetStats: 验证统计
  - TestPool: 验证 worker pool
  - TestPoolScale: 验证动态扩容缩容
  - TestTruncate: 验证字符串截断
  - TestRandNorm: 验证随机数生成

- **metrics/collector_test.go**: 160+ 行
  - TestNewCollector: 验证创建收集器
  - TestCollectorRecord: 验证记录指标
  - TestCalculateLatencyStats: 验证延迟统计
  - TestPercentile: 验证百分位数计算
  - TestReportSaveToFile: 验证报告保存
  - TestAggregator: 验证聚合器
  - TestLoadFromFiles: 验证从文件加载
  - TestReporter: 验证实时报告

- **controller/controller_test.go**: 90+ 行
  - TestDefaultConfig: 验证默认配置
  - TestNewController: 验证创建控制器
  - TestControllerIsRunning: 验证运行状态
  - TestControllerSetters: 验证配置设置
  - TestControllerUpdateConfig: 验证配置更新
  - TestFileExists: 验证文件存在检查

- **cmd/main_test.go**: 70+ 行
  - TestFindInPath: 验证 PATH 查找
  - TestInitLogger: 验证日志初始化
  - TestAggregateCommand: 验证聚合命令
  - TestVersion: 验证版本信息

### 集成测试

- **test/integration/integration_test.go**: 160+ 行
  - TestBuild: 验证构建成功
  - TestVersion: 验证版本命令
  - TestHelp: 验证帮助信息
  - TestConfigFile: 验证配置文件
  - TestRunCommand: 验证运行命令
  - TestAggregateCommand: 验证聚合命令
  - TestDockerBuild: 验证 Docker 构建
  - BenchmarkWorkerCreation: Worker 创建性能
  - BenchmarkMetricsCollection: 指标收集性能

### E2E 测试

- **test/e2e/e2e_test.go**: 180+ 行
  - TestE2E_APIStatus: API 状态端点
  - TestE2E_APIMetrics: Prometheus 指标端点
  - TestE2E_WebSocket: WebSocket 连接
  - TestE2E_RemoteControl: 远程控制 API
  - TestE2E_FullWorkflow: 完整工作流

## 日志实现

项目使用 **zap** 日志库，日志输出包括：

### 日志级别

- **INFO**: 启动信息、状态变更、报告保存
- **WARN**: 配置问题、降级情况
- **ERROR**: 致命错误、启动失败
- **DEBUG**: 详细信息（可选启用）

### 日志示例

```bash
# 启动日志
2026-03-25T10:00:00.000Z	INFO	Starting load test	{"mode": "ramp-up", "max_workers": 50}

# Worker 日志
2026-03-25T10:00:01.000Z	INFO	Worker started	{"worker_id": 1}
2026-03-25T10:00:05.000Z	DEBUG	Request succeeded	{"worker_id": 1, "duration_ms": 1234}

# 统计日志
2026-03-25T10:00:10.000Z	INFO	Realtime stats	{"workers": 10, "rps": 45.5, "success_rate": 98.5}

# 完成日志
2026-03-25T10:10:00.000Z	INFO	Test completed	{"elapsed": "10m0s"}
2026-03-25T10:10:00.000Z	INFO	Report saved	{"path": "./results/test.json"}
```

### 启用 Debug 日志

```bash
# 设置环境变量
export CLAUDE_LOAD_TEST_DEBUG=1
./claude-load-tester run ...

# 或使用 zap 配置
./claude-load-tester run --log-level debug ...
```

## 测试覆盖率

目标覆盖率:

| 模块 | 目标覆盖率 | 当前状态 |
|------|-----------|---------|
| prompts | 80%+ | 已实现 |
| worker | 75%+ | 已实现 |
| metrics | 85%+ | 已实现 |
| controller | 70%+ | 已实现 |
| cmd | 60%+ | 已实现 |
| **总计** | **75%+** | 已实现 |

## CI/CD 集成

```yaml
# .github/workflows/test.yml
name: Tests

on: [push, pull_request]

jobs:
  unit-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.24'
      - run: go test -race -coverprofile=coverage.out ./...
      - uses: codecov/codecov-action@v3
        with:
          files: ./coverage.out

  integration-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.24'
      - run: go build -o claude-load-tester ./cmd
      - run: go test -tags=integration ./test/integration/
```

## 添加新测试

```go
func TestNewFeature(t *testing.T) {
    // 准备
    logger := zap.NewNop()
    
    // 执行
    result := newFeature(logger)
    
    // 验证
    if result != expected {
        t.Errorf("expected %v, got %v", expected, result)
    }
}
```

## 测试统计

- **单元测试**: 25+ 个测试用例
- **集成测试**: 8+ 个测试用例
- **E2E 测试**: 5+ 个测试用例
- **总计**: 38+ 个测试用例
- **代码行数**: 850+ 行测试代码
