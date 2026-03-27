# Claude Load Tester 完整使用手册

## 目录

1. [安装与构建](#安装与构建)
2. [命令行参数](#命令行参数)
3. [配置文件](#配置文件)
4. [Prompts 格式](#prompts-格式)
5. [基础使用](#基础使用)
6. [WebSocket 实时报告](#websocket-实时报告)
7. [Prometheus 指标导出](#prometheus-指标导出)
8. [Grafana Dashboard](#grafana-dashboard)
9. [HTTP API 远程控制](#http-api-远程控制)
10. [监控可视化对接](#监控可视化对接)

---

## 安装与构建

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/l17728/pairproxy.git
cd pairproxy/tools/loadtest

# 下载依赖
go mod download

# 构建当前平台
go build -o claude-load-tester ./cmd

# 或使用 Makefile
make build
# 输出: build/claude-load-tester
```

### 跨平台构建

```bash
# 构建所有平台
make build-all
# 输出:
#   build/claude-load-tester-linux-amd64
#   build/claude-load-tester-linux-arm64
#   build/claude-load-tester-darwin-amd64
#   build/claude-load-tester-darwin-arm64
#   build/claude-load-tester-windows-amd64.exe

# 创建发布包
make release
# 输出: dist/claude-load-tester-*.tar.gz
```

### 安装到系统

```bash
# 安装到 $GOPATH/bin
go install ./cmd

# 或手动安装
sudo cp build/claude-load-tester /usr/local/bin/
```

### Docker 构建

```bash
# 构建镜像
docker build -t claude-load-tester .

# 或使用 Makefile
make docker-build
```

---

## 命令行参数

### 全局参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--claude-path` | Claude CLI 可执行文件路径 | `claude` |
| `--prompts` | Prompts YAML 文件路径 | 内置默认 |
| `--output` | 输出报告文件路径 (JSON) | 控制台输出 |

### run 子命令 - 测试模式参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--mode` | 测试模式: `ramp-up`, `fixed`, `spike` | `ramp-up` |

#### ramp-up 模式参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--initial` | 初始并发数 | `1` |
| `--max` | 最大并发数 | `50` |
| `--step-size` | 每步增加的并发数 | `5` |
| `--step-duration` | 每步持续时间 | `60s` |
| `--ramp-interval` | 阶梯递增间隔 | `30s` |

#### fixed 模式参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--workers` | 固定并发数 | `10` |
| `--duration` | 测试总时长 | `10m` |

#### spike 模式参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--max` | 瞬间启动的并发数 | `50` |
| `--duration` | 测试持续时间 | `10m` |

### run 子命令 - 其他参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--timeout` | 单个请求超时时间 | `120s` |
| `--think-min` | 最小思考时间（请求间隔） | `10s` |
| `--think-max` | 最大思考时间（请求间隔） | `120s` |
| `--report-interval` | 实时报告间隔 | `10s` |
| `--circuit-breaker` | 启用熔断保护 | `true` |
| `--circuit-threshold` | 熔断错误率阈值 | `0.05` (5%) |

### run 子命令 - API 参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `--api-enabled` | 启用 HTTP API 服务器 | `false` |
| `--api-addr` | API 监听地址 | `:8080` |
| `--api-prometheus` | 启用 Prometheus 指标端点 | `false` |

### aggregate 子命令

| 参数 | 说明 | 必需 |
|------|------|------|
| `--inputs` | 输入 JSON 报告文件（逗号分隔） | 是 |
| `--output` | 聚合报告输出路径 | 否 |

### 使用示例

```bash
# 阶梯递增测试
./claude-load-tester run \
  --mode ramp-up \
  --initial 1 \
  --max 100 \
  --step-size 10 \
  --step-duration 120s \
  --output ./results/ramp-up.json

# 固定并发测试
./claude-load-tester run \
  --mode fixed \
  --workers 30 \
  --duration 30m \
  --output ./results/fixed-30.json

# 脉冲测试
./claude-load-tester run \
  --mode spike \
  --max 200 \
  --duration 5m \
  --output ./results/spike-200.json

# 带 API 和监控
./claude-load-tester run \
  --mode fixed \
  --workers 50 \
  --duration 1h \
  --api-enabled \
  --api-addr ":8080" \
  --api-prometheus \
  --output ./results/production.json

# 聚合多节点报告
./claude-load-tester aggregate \
  --inputs ./node1.json,./node2.json,./node3.json \
  --output ./summary.json
```

---

## 配置文件

### 配置文件格式

创建 `config.yaml` 文件：

```yaml
# Claude CLI 路径
claude_path: "claude"

# Prompts 文件路径
prompts_path: "./prompts/prompts.yaml"

# 输出报告路径
output_path: "./results/test-report.json"

# 测试模式: ramp-up | fixed | spike
mode: "ramp-up"

# Worker 配置
workers:
  initial: 1      # 初始 worker 数 (ramp-up)
  max: 50         # 最大 worker 数
  fixed: 10       # 固定 worker 数 (fixed 模式)

# 阶梯递增配置 (ramp-up 模式)
ramp_up:
  step_size: 5           # 每步增加的 worker 数
  step_duration: "60s"   # 每步持续时间
  interval: "30s"        # 递增间隔

# 固定模式配置
fixed:
  duration: "10m"        # 测试总时长

# 超时配置
timeout: "120s"

# 思考时间配置 (模拟真实用户间隔)
think_time:
  min: "10s"
  max: "120s"

# 实时报告间隔
report_interval: "10s"

# 熔断配置
circuit_breaker:
  enabled: true
  threshold: 0.05        # 错误率阈值 (5%)
```

### 使用配置文件

```bash
# 方式1: 命令行参数覆盖配置文件
./claude-load-tester run --config config.yaml --workers 100

# 方式2: 使用环境变量
export CLAUDE_LOAD_TEST_CONFIG=./config.yaml
./claude-load-tester run
```

### 配置文件示例

完整的配置示例见 `config/example.yaml`。

---

## Prompts 格式

### YAML 文件格式

Prompts 文件使用 YAML 格式，定义多个类别和对应的提示词：

```yaml
categories:
  code_understanding:
    prompts:
      - "解释这段代码的作用：func main() { fmt.Println(\"Hello\") }"
      - "这段 Python 代码是什么意思：def foo(): return [x for x in range(10)]"
      - "分析这个正则表达式的匹配规则"
      
  code_refactoring:
    prompts:
      - "重构这段代码，使其更易读：if (x == 1) { return true; } else { return false; }"
      - "将这段代码改为使用泛型"
      
  debugging:
    prompts:
      - "这段代码报错 'nil pointer dereference'，如何修复？"
      - "为什么会出现 'concurrent map writes' 错误？"
```

### 格式说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `categories` | map | 分类名称到提示词列表的映射 |
| `categories.<name>` | object | 单个分类 |
| `categories.<name>.prompts` | array | 该分类下的提示词列表 |

### 默认 Prompts

如果不指定 prompts 文件，将使用内置默认 prompts，包含以下分类：

| 分类 | 说明 | 示例数量 |
|------|------|----------|
| `code_understanding` | 代码理解 | 8 |
| `code_refactoring` | 代码重构 | 8 |
| `debugging` | 调试问题 | 8 |
| `code_generation` | 代码生成 | 8 |
| `explanation` | 概念解释 | 10 |
| `system_design` | 系统设计 | 8 |
| `algorithm` | 算法实现 | 8 |

### 自定义 Prompts 文件

```bash
# 使用自定义 prompts
./claude-load-tester run \
  --prompts ./my-prompts.yaml \
  --mode fixed \
  --workers 20
```

---

## 基础使用

### 启动 API 模式

```bash
# 启动带有 HTTP API 和 WebSocket 支持的服务
./claude-load-tester run \
  --mode fixed \
  --workers 30 \
  --api-addr ":8080" \
  --api-enabled
```

**参数说明：**
- `--api-enabled`: 启用 HTTP API 服务器
- `--api-addr`: API 服务器监听地址（默认 `:8080`）
- `--api-prometheus`: 启用 Prometheus 指标端点

---

## WebSocket 实时报告

### 功能说明

WebSocket 实时报告允许你通过 WebSocket 连接实时接收测试指标更新，无需轮询 HTTP API。

### 连接 WebSocket

```javascript
// JavaScript 示例
const ws = new WebSocket('ws://localhost:8080/ws');

ws.onopen = function() {
  console.log('WebSocket 连接成功');
};

ws.onmessage = function(event) {
  const data = JSON.parse(event.data);
  console.log('实时指标:', data);
};

ws.onerror = function(error) {
  console.error('WebSocket 错误:', error);
};

ws.onclose = function() {
  console.log('WebSocket 连接关闭');
};
```

### WebSocket 消息格式

```json
{
  "type": "realtime",
  "timestamp": "2026-03-25T10:30:00Z",
  "active_workers": 30,
  "rps": 45.5,
  "success_rate": 98.5,
  "avg_latency_ms": 1234.56
}
```

### 实时 Dashboard 示例

```html
<!DOCTYPE html>
<html>
<head>
  <title>Load Test Realtime Dashboard</title>
  <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
  <style>
    body { font-family: Arial, sans-serif; margin: 20px; }
    .metric-card { 
      background: #f5f5f5; 
      padding: 20px; 
      margin: 10px; 
      border-radius: 8px;
      display: inline-block;
      min-width: 200px;
    }
    .metric-value { font-size: 24px; font-weight: bold; color: #333; }
    .metric-label { font-size: 14px; color: #666; }
  </style>
</head>
<body>
  <h1>Load Test Realtime Dashboard</h1>
  
  <div class="metric-card">
    <div class="metric-label">Active Workers</div>
    <div class="metric-value" id="workers">0</div>
  </div>
  
  <div class="metric-card">
    <div class="metric-label">Requests/sec</div>
    <div class="metric-value" id="rps">0</div>
  </div>
  
  <div class="metric-card">
    <div class="metric-label">Success Rate</div>
    <div class="metric-value" id="success-rate">0%</div>
  </div>
  
  <div class="metric-card">
    <div class="metric-label">Avg Latency</div>
    <div class="metric-value" id="latency">0 ms</div>
  </div>
  
  <canvas id="rpsChart" width="800" height="400"></canvas>
  
  <script>
    const ctx = document.getElementById('rpsChart').getContext('2d');
    const chart = new Chart(ctx, {
      type: 'line',
      data: {
        labels: [],
        datasets: [{
          label: 'RPS',
          data: [],
          borderColor: 'rgb(75, 192, 192)',
          tension: 0.1
        }]
      },
      options: {
        responsive: true,
        scales: {
          y: { beginAtZero: true }
        }
      }
    });
    
    const ws = new WebSocket('ws://localhost:8080/ws');
    
    ws.onmessage = function(event) {
      const data = JSON.parse(event.data);
      
      // 更新指标卡片
      document.getElementById('workers').textContent = data.active_workers;
      document.getElementById('rps').textContent = data.rps.toFixed(2);
      document.getElementById('success-rate').textContent = data.success_rate.toFixed(2) + '%';
      document.getElementById('latency').textContent = data.avg_latency_ms.toFixed(2) + ' ms';
      
      // 更新图表
      const time = new Date(data.timestamp).toLocaleTimeString();
      chart.data.labels.push(time);
      chart.data.datasets[0].data.push(data.rps);
      
      // 保持最近 60 个数据点
      if (chart.data.labels.length > 60) {
        chart.data.labels.shift();
        chart.data.datasets[0].data.shift();
      }
      
      chart.update();
    };
  </script>
</body>
</html>
```

---

## Prometheus 指标导出

### 功能说明

Prometheus 指标导出允许你将测试指标暴露给 Prometheus 进行抓取和存储。

### 启用 Prometheus 端点

```bash
./claude-load-tester run \
  --mode fixed \
  --workers 30 \
  --api-enabled \
  --api-prometheus \
  --api-addr ":8080"
```

### Prometheus 配置

编辑 `prometheus.yml`：

```yaml
# 添加 scrape job
scrape_configs:
  - job_name: 'claude-load-tester'
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: /api/metrics
    scrape_interval: 5s
    scrape_timeout: 3s
```

### 暴露的指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `loadtest_requests_total` | Counter | 总请求数 |
| `loadtest_requests_success` | Counter | 成功请求数 |
| `loadtest_requests_failed` | Counter | 失败请求数 |
| `loadtest_success_rate` | Gauge | 成功率 (0-100) |
| `loadtest_rps` | Gauge | 每秒请求数 |
| `loadtest_latency_ms` | Summary | 请求延迟 (P50, P90, P95, P99) |
| `loadtest_workers_active` | Gauge | 活跃 Worker 数 |
| `loadtest_test_duration_seconds` | Gauge | 测试运行时间 |

### 指标示例

```promql
# 查看当前 RPS
loadtest_rps

# 查看成功率
loadtest_success_rate

# 查看 P95 延迟
loadtest_latency_ms{quantile="0.95"}

# 查看活跃 Worker 数
loadtest_workers_active

# 计算过去 5 分钟的平均 RPS
avg_over_time(loadtest_rps[5m])

# 设置告警规则
# 当成功率低于 95% 时告警
ALERT LowSuccessRate
  IF loadtest_success_rate < 95
  FOR 1m
  LABELS { severity = "warning" }
  ANNOTATIONS {
    summary = "Load test success rate is low",
    description = "Success rate is {{ $value }}%",
  }
```

---

## Grafana Dashboard

### 导入 Dashboard

1. 登录 Grafana
2. 点击左侧 "+" → "Import"
3. 上传 `grafana/dashboard.json` 文件
4. 选择 Prometheus 数据源
5. 点击 "Import"

### Dashboard 面板说明

#### 1. Overview Panel

- **总请求数**: `sum(loadtest_requests_total)`
- **成功率**: `loadtest_success_rate`
- **当前 RPS**: `loadtest_rps`
- **活跃 Workers**: `loadtest_workers_active`

#### 2. Latency Panel

- **延迟分布**: 
  - P50: `loadtest_latency_ms{quantile="0.5"}`
  - P90: `loadtest_latency_ms{quantile="0.9"}`
  - P95: `loadtest_latency_ms{quantile="0.95"}`
  - P99: `loadtest_latency_ms{quantile="0.99"}`

#### 3. Throughput Panel

- **RPS 趋势**: `rate(loadtest_requests_total[1m])`
- **成功率趋势**: `loadtest_success_rate`

#### 4. Workers Panel

- **Worker 数量变化**: `loadtest_workers_active`
- **Worker 分布**: 按实例分组

### 自定义 Dashboard

创建新的 Dashboard，添加 Panel：

```json
{
  "dashboard": {
    "title": "Claude Load Test",
    "panels": [
      {
        "title": "RPS",
        "type": "graph",
        "targets": [
          {
            "expr": "loadtest_rps",
            "legendFormat": "Requests/sec"
          }
        ]
      },
      {
        "title": "Latency",
        "type": "graph",
        "targets": [
          {
            "expr": "loadtest_latency_ms{quantile=\"0.5\"}",
            "legendFormat": "P50"
          },
          {
            "expr": "loadtest_latency_ms{quantile=\"0.95\"}",
            "legendFormat": "P95"
          }
        ]
      },
      {
        "title": "Success Rate",
        "type": "singlestat",
        "targets": [
          {
            "expr": "loadtest_success_rate"
          }
        ],
        "thresholds": [95, 99]
      }
    ]
  }
}
```

### 告警规则

在 Grafana 中配置告警：

```yaml
# 告警规则
apiVersion: 1
groups:
  - name: loadtest
    rules:
      - alert: HighLatency
        expr: loadtest_latency_ms{quantile="0.95"} > 5000
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Load test P95 latency is high"
          
      - alert: LowSuccessRate
        expr: loadtest_success_rate < 95
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Load test success rate is low"
          
      - alert: ZeroRPS
        expr: loadtest_rps == 0
        for: 30s
        labels:
          severity: critical
        annotations:
          summary: "Load test has stopped sending requests"
```

---

## HTTP API 远程控制

### API 概览

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /api/status | 获取测试状态 |
| GET | /api/metrics | Prometheus 格式指标 |
| GET | /api/report | 获取测试报告 |
| GET | /api/config | 获取当前配置 |
| PUT | /api/config | 更新配置 |
| POST | /api/start | 启动测试 |
| POST | /api/stop | 停止测试 |
| WS | /ws | WebSocket 实时数据 |

### API 详细说明

#### 1. 获取测试状态

```bash
curl http://localhost:8080/api/status
```

**响应：**
```json
{
  "running": true,
  "current_workers": 30,
  "start_time": "2026-03-25T10:00:00Z",
  "duration": "5m30s"
}
```

#### 2. 获取 Prometheus 指标

```bash
curl http://localhost:8080/api/metrics
```

**响应：**
```
# HELP loadtest_requests_total Total requests
# TYPE loadtest_requests_total counter
loadtest_requests_total 12345

# HELP loadtest_rps Requests per second
# TYPE loadtest_rps gauge
loadtest_rps 45.5
...
```

#### 3. 获取测试报告

```bash
curl http://localhost:8080/api/report
```

**响应：**
```json
{
  "start_time": "2026-03-25T10:00:00Z",
  "end_time": "2026-03-25T10:10:00Z",
  "duration": "10m0s",
  "total_workers": 30,
  "total_requests": 2453,
  "success_count": 2401,
  "success_rate": 97.88,
  "latency_stats": {
    "min_ms": 234.56,
    "mean_ms": 1234.78,
    "max_ms": 5678.90,
    "p50_ms": 1156.34,
    "p90_ms": 1890.12,
    "p95_ms": 2345.67,
    "p99_ms": 4567.89
  }
}
```

#### 4. 远程启动测试

```bash
curl -X POST http://localhost:8080/api/start \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "fixed",
    "workers": 50,
    "duration": "10m"
  }'
```

**响应：**
```json
{
  "status": "started"
}
```

#### 5. 远程停止测试

```bash
curl -X POST http://localhost:8080/api/stop
```

**响应：**
```json
{
  "status": "stopped"
}
```

#### 6. 获取配置

```bash
curl http://localhost:8080/api/config
```

**响应：**
```json
{
  "claude_path": "claude",
  "mode": "fixed",
  "fixed_workers": 30,
  "duration": "10m",
  "timeout": "120s"
}
```

#### 7. 更新配置

```bash
curl -X PUT http://localhost:8080/api/config \
  -H "Content-Type: application/json" \
  -d '{
    "mode": "ramp-up",
    "initial_workers": 1,
    "max_workers": 100
  }'
```

**响应：**
```json
{
  "status": "updated"
}
```

### Python API 客户端示例

```python
import requests
import json
import time

class LoadTestClient:
    def __init__(self, base_url="http://localhost:8080"):
        self.base_url = base_url
    
    def start_test(self, mode="fixed", workers=30, duration="10m"):
        """启动测试"""
        response = requests.post(f"{self.base_url}/api/start", json={
            "mode": mode,
            "workers": workers,
            "duration": duration
        })
        return response.json()
    
    def stop_test(self):
        """停止测试"""
        response = requests.post(f"{self.base_url}/api/stop")
        return response.json()
    
    def get_status(self):
        """获取状态"""
        response = requests.get(f"{self.base_url}/api/status")
        return response.json()
    
    def get_metrics(self):
        """获取 Prometheus 格式指标"""
        response = requests.get(f"{self.base_url}/api/metrics")
        return response.text
    
    def get_report(self):
        """获取测试报告"""
        response = requests.get(f"{self.base_url}/api/report")
        return response.json()
    
    def wait_for_completion(self, check_interval=5):
        """等待测试完成"""
        while True:
            status = self.get_status()
            if not status["running"]:
                break
            print(f"Running: {status['duration']}, Workers: {status['current_workers']}")
            time.sleep(check_interval)
        
        return self.get_report()

# 使用示例
if __name__ == "__main__":
    client = LoadTestClient()
    
    # 启动测试
    print("Starting test...")
    client.start_test(mode="ramp-up", workers=50, duration="5m")
    
    # 等待完成
    report = client.wait_for_completion()
    
    # 打印结果
    print(f"\nTest completed!")
    print(f"Total requests: {report['total_requests']}")
    print(f"Success rate: {report['success_rate']}%")
    print(f"P95 latency: {report['latency_stats']['p95_ms']}ms")
```

---

## 监控可视化对接

### 1. 与 Prometheus + Grafana 集成

```yaml
# docker-compose.yml
version: '3.8'

services:
  loadtest:
    build: .
    command: ./claude-load-tester run --api-enabled --api-prometheus
    ports:
      - "8080:8080"
    
  prometheus:
    image: prom/prometheus:latest
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
    ports:
      - "9090:9090"
    
  grafana:
    image: grafana/grafana:latest
    volumes:
      - ./grafana/dashboard.json:/etc/grafana/provisioning/dashboards/dashboard.json
      - ./grafana/dashboards/dashboards.yml:/etc/grafana/provisioning/dashboards/dashboards.yml
      - ./grafana/datasources/datasources.yml:/etc/grafana/provisioning/datasources/datasources.yml
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
```

### 2. 与 Datadog 集成

```python
# 使用 Datadog Agent 抓取 Prometheus 端点
# datadog.yaml
prometheus_scrape:
  enabled: true
  configs:
    - name: 'claude-load-tester'
      url: 'http://localhost:8080/api/metrics'
      namespace: 'loadtest'
```

### 3. 与 InfluxDB + Grafana 集成

```bash
# 使用 Telegraf 抓取 Prometheus 格式
telegraf --config telegraf.conf
```

```toml
# telegraf.conf
[[inputs.prometheus]]
  urls = ["http://localhost:8080/api/metrics"]
  
[[outputs.influxdb]]
  urls = ["http://influxdb:8086"]
  database = "loadtest"
```

### 4. 与 Elastic Stack 集成

```yaml
# filebeat.yml
filebeat.inputs:
- type: httpjson
  config_version: 2
  interval: 5s
  request.url: http://localhost:8080/api/report
  
output.elasticsearch:
  hosts: ["http://elasticsearch:9200"]
  index: "loadtest-%{+yyyy.MM.dd}"
```

### 5. 与 VictoriaMetrics 集成

VictoriaMetrics 是 Prometheus 的高性能替代方案：

```bash
# 启动 VictoriaMetrics
./victoria-metrics \
  -promscrape.config=prometheus.yml \
  -retentionPeriod=30d

# 配置抓取
# prometheus.yml
scrape_configs:
  - job_name: 'claude-load-tester'
    static_configs:
      - targets: ['localhost:8080']
```

### 6. 与 Thanos 集成（分布式 Prometheus）

```yaml
# thanos-sidecar 配置
- --prometheus.url=http://localhost:9090
- --tsdb.path=/var/prometheus
- --objstore.config-file=/etc/thanos/bucket.yml
```

---

## 完整示例：生产环境监控

### 架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                      监控架构                                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐   │
│  │ Load Tester  │────▶│ Prometheus   │────▶│ Grafana      │   │
│  │ (API Mode)   │     │ (Scrape)     │     │ (Dashboard)  │   │
│  └──────────────┘     └──────────────┘     └──────────────┘   │
│         │                    │                    │             │
│         │                    ▼                    ▼             │
│         │             ┌──────────────┐     ┌──────────────┐   │
│         │             │ AlertManager │     │ Web UI       │   │
│         │             └──────────────┘     └──────────────┘   │
│         │                                                       │
│         ▼                                                       │
│  ┌──────────────┐                                              │
│  │ WebSocket    │                                              │
│  │ Realtime     │                                              │
│  └──────────────┘                                              │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

### 启动命令

```bash
#!/bin/bash
# start-with-monitoring.sh

# 1. 启动 Load Tester API 模式
./claude-load-tester run \
  --mode fixed \
  --workers 30 \
  --duration 1h \
  --api-enabled \
  --api-addr ":8080" \
  --api-prometheus \
  --output ./results/production.json &

# 2. 等待 API 启动
sleep 5

# 3. 启动 Prometheus
docker run -d \
  --name prometheus \
  -p 9090:9090 \
  -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml \
  prom/prometheus

# 4. 启动 Grafana
docker run -d \
  --name grafana \
  -p 3000:3000 \
  -v $(pwd)/grafana:/etc/grafana/provisioning \
  grafana/grafana

echo "监控已启动:"
echo "  - Load Tester API: http://localhost:8080"
echo "  - Prometheus: http://localhost:9090"
echo "  - Grafana: http://localhost:3000 (admin/admin)"
```

---

## 故障排查

### 常见问题

#### 1. WebSocket 连接失败

**症状**: WebSocket 连接被拒绝

**解决**:
```bash
# 检查 API 是否启用
curl http://localhost:8080/api/status

# 检查防火墙
curl -v http://localhost:8080/ws
```

#### 2. Prometheus 抓取失败

**症状**: Prometheus 显示 `connection refused`

**解决**:
```bash
# 确认端点可访问
curl http://localhost:8080/api/metrics

# 检查 Prometheus 配置
cat prometheus.yml | grep -A5 'claude-load-tester'
```

#### 3. Grafana 无数据

**症状**: Dashboard 显示 `No data`

**解决**:
```bash
# 检查 Prometheus 数据源
curl http://localhost:9090/api/v1/targets

# 验证指标存在
curl http://localhost:9090/api/v1/query?query=loadtest_rps
```

---

## 附录

### A. 完整配置示例

```yaml
# config.yaml - 完整配置示例
claude_path: "claude"
prompts_path: "./prompts/prompts.yaml"
output_path: "./results/test.json"

# 测试模式: ramp-up | fixed | spike
mode: "ramp-up"

# Worker 配置
workers:
  initial: 1
  max: 100
  fixed: 10

# 阶梯递增配置
ramp_up:
  step_size: 10
  step_duration: "120s"
  interval: "60s"

# 固定模式配置
fixed:
  duration: "10m"

# 超时配置
timeout: "120s"

# 思考时间
think_time:
  min: "10s"
  max: "120s"

# 报告间隔
report_interval: "10s"

# 熔断配置
circuit_breaker:
  enabled: true
  threshold: 0.05

# API 配置
api:
  enabled: true
  addr: ":8080"
  prometheus: true
  cors: true
```

### B. Prompts 示例文件

```yaml
# prompts.yaml - 完整示例
categories:
  code_understanding:
    prompts:
      - "解释这段代码的作用：func main() { fmt.Println(\"Hello\") }"
      - "分析这个正则表达式的匹配规则"
      - "这段代码的时间复杂度是多少？"
      
  code_refactoring:
    prompts:
      - "重构这段代码，使其更易读"
      - "将这段代码改为使用泛型"
      
  debugging:
    prompts:
      - "这段代码报错如何修复？"
      - "为什么会出现并发问题？"
```

### C. 目录结构

```
tools/loadtest/
├── cmd/                      # CLI 入口
│   └── main.go
├── internal/                 # 内部包
│   ├── api/                  # HTTP API 服务
│   ├── controller/           # 测试控制器
│   ├── metrics/              # 指标收集
│   ├── prompts/              # Prompts 加载
│   └── worker/               # Worker 实现
├── config/                   # 配置文件
│   └── example.yaml
├── prompts/                  # Prompts 文件
│   └── prompts.yaml
├── grafana/                  # Grafana 配置
│   ├── dashboard.json
│   ├── dashboards/
│   │   └── dashboards.yml
│   └── datasources/
│       └── datasources.yml
├── test/                     # 测试文件
│   ├── integration/
│   └── e2e/
├── results/                  # 输出目录
├── prometheus.yml            # Prometheus 配置
├── docker-compose.yml        # Docker Compose
├── Dockerfile
├── Makefile
├── README.md
├── MANUAL.md                 # 本手册
└── LICENSE
```

### D. 性能调优建议

| 参数 | 推荐值 | 说明 |
|------|--------|------|
| WebSocket 连接数 | ≤ 100 | 单实例最大并发连接 |
| Prometheus 抓取频率 | 5-10s | 过频会影响性能 |
| Worker 数量 | ≤ 200 | 取决于目标系统容量 |
| 思考时间 min | 5-10s | 模拟真实用户 |
| 思考时间 max | 60-120s | 模拟真实用户 |

### E. 常见错误码

| 错误 | 原因 | 解决方案 |
|------|------|----------|
| `claude: command not found` | Claude CLI 未安装 | 安装 Claude CLI 或使用 `--claude-path` |
| `connection refused` | API 未启动 | 添加 `--api-enabled` 参数 |
| `no such file` | 配置文件不存在 | 检查 `--prompts` 或 `--config` 路径 |
| `circuit breaker triggered` | 错误率过高 | 检查目标系统或调整 `--circuit-threshold` |

---

**文档版本**: 2.0
**最后更新**: 2026-03-26
