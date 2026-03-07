# PRD: cproxy 模型名称透传功能

## 文档信息

| 项目 | 内容 |
------|------|
| 标题 | cproxy Model Header 注入功能 |
| 版本 | 1.0.0 |
| 作者 | PairProxy Team |
| 状态 | 待评审 |
| 日期 | 2026-03-07 |

---

## 1. 背景与问题描述

### 1.1 当前问题

在 PairProxy 的实际使用过程中，用户反馈 Web 管理界面的"概览"和"日志"页面中，**模型名称(Model)** 字段显示为空（显示为 "—"）。

### 1.2 问题根因

通过代码分析发现：

1. **sproxy 期望接收 `X-PairProxy-Model` header**
   - `sproxy.go:1042-1047` 中定义了 `extractModel()` 函数，优先从该 header 获取模型名称
   - 若 header 不存在，则尝试从请求/响应 body 解析（作为 fallback）

2. **cproxy 未注入该 header**
   - `cproxy.go:191-203` 的 Director 函数中，cproxy 只注入了：
     - `X-PairProxy-Auth` (JWT Token)
     - `X-Routing-Version` (路由版本)
   - **缺失 `X-PairProxy-Model` header 的注入逻辑**

3. **Fallback 机制不完善**
   - 虽然 sproxy 实现了从 body 解析 model 的 fallback，但由于请求 body 在转发过程中可能被消耗或修改，导致解析失败率较高

### 1.3 影响范围

| 影响项 | 说明 |
|--------|------|
| Dashboard 数据完整性 | 模型名称为空，无法统计按模型的用量分析 |
| 费用估算准确性 | 不同模型费率不同，缺失模型信息影响成本计算 |
| 用户体验 | 用户无法直观看到请求使用的模型 |

---

## 2. 目标

### 2.1 主要目标

在 cproxy 层实现模型名称的提取和透传，确保 sproxy 能够正确接收并记录模型信息。

### 2.2 成功指标

- 所有经过 cproxy 的请求，都能在 Dashboard 中正确显示模型名称
- 支持主流 LLM 格式：Anthropic、OpenAI、Ollama
- 不破坏现有功能，保持向后兼容

---

## 3. 需求详细说明

### 3.1 功能需求

#### FR-1: 请求体模型提取

**描述**: cproxy 需要能够从客户端请求体中提取模型名称

**优先级**: P0 (必须)

**详细说明**:
- 支持的请求格式：
  ```json
  // Anthropic 格式
  {
    "model": "claude-3-5-sonnet-20241022",
    "messages": [...],
    "max_tokens": 1024
  }

  // OpenAI 格式
  {
    "model": "gpt-4",
    "messages": [...]
  }

  // Ollama 格式
  {
    "model": "llama2",
    "prompt": "..."
  }
  ```

- 提取逻辑：
  - 解析请求 body 中的 `model` 字段
  - 支持 JSON 格式请求体（`Content-Type: application/json`）
  - 提取失败时不阻断请求，静默处理

#### FR-2: Header 注入

**描述**: cproxy 需要将提取的模型名称注入到转发给 sproxy 的请求头中

**优先级**: P0 (必须)

**详细说明**:
- Header 名称: `X-PairProxy-Model`
- Header 值: 提取的模型名称（如 `claude-3-5-sonnet-20241022`）
- 仅在成功提取模型时注入该 header
- 若提取失败，则不注入该 header（让 sproxy 使用 fallback 机制）

#### FR-3: 性能保证

**描述**: 模型提取和 header 注入不能显著影响请求延迟

**优先级**: P1 (重要)

**详细说明**:
- 单次请求的额外处理时间 < 1ms
- 避免多次读取/解析请求体
- 复用已有的 body buffer

### 3.2 非功能需求

| 需求 | 说明 | 优先级 |
|------|------|--------|
| 向后兼容 | 不影响现有请求处理逻辑 | P0 |
| 容错性 | 解析失败不阻断请求 | P0 |
| 可测试性 | 新增代码需有单元测试覆盖 | P1 |
| 可维护性 | 代码结构清晰，便于后续扩展 | P1 |

---

## 4. 技术方案

### 4.1 架构图

```
┌─────────────────────────────────────────────────────────────┐
│                        Client Request                        │
│  POST /v1/messages                                           │
│  Body: {"model":"claude-3-5-sonnet","messages":[]}          │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                      cproxy (Client Proxy)                   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ 1. 读取请求 body（复用现有逻辑）                        │   │
│  │ 2. 提取 model 字段                                     │   │
│  │    └─> "claude-3-5-sonnet"                           │   │
│  │ 3. 构建转发请求                                        │   │
│  │    ├─ X-PairProxy-Auth: <jwt>                        │   │
│  │    ├─ X-Routing-Version: <version>                   │   │
│  │    └─ X-PairProxy-Model: claude-3-5-sonnet  ◄── 新增 │   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                      sproxy (Server Proxy)                   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │ extractModel(r):                                     │   │
│  │ 1. r.Header.Get("X-PairProxy-Model")                 │   │
│  │    └─> "claude-3-5-sonnet" ✓                       │   │
│  │ 2. 创建 UsageRecord，Model 字段已填充                  │   │
│  │ 3. 记录到数据库                                        │   │
│  └──────────────────────────────────────────────────────┘   │
└──────────────────────┬──────────────────────────────────────┘
                       │
                       ▼
┌─────────────────────────────────────────────────────────────┐
│                         Dashboard                            │
│  时间      │ 用户  │ 模型                    │ 输入 │ 输出 │
│  15:04:32  │ alice │ claude-3-5-sonnet       │  120 │  450 │
│  15:03:15  │ bob   │ gpt-4                   │   80 │  200 │
└─────────────────────────────────────────────────────────────┘
```

### 4.2 代码修改方案

#### 修改文件: `internal/proxy/cproxy.go`

**新增函数**:
```go
// extractModelFromBody 从请求 body 中提取 model 字段
func extractModelFromBody(body []byte) string {
    var req struct {
        Model string `json:"model"`
    }
    if err := json.Unmarshal(body, &req); err != nil {
        return ""
    }
    return req.Model
}
```

**修改 serveProxy 函数**:

在读取请求 body 后（约第 156-166 行），添加模型提取逻辑：

```go
// debug 日志：← client request（Claude Code 发来的原始请求）
var debugReqBody []byte
if r.Body != nil {
    bodyBytes, _ := io.ReadAll(r.Body)
    r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
    debugReqBody = bodyBytes

    // 新增：提取模型名称
    if model := extractModelFromBody(bodyBytes); model != "" {
        // 暂存到 context 或变量，供 Director 使用
        ctx = context.WithValue(r.Context(), modelContextKey, model)
        r = r.WithContext(ctx)
    }
}

// ... debug 日志代码 ...
```

**修改 Director 函数**:

在设置 header 时（约第 192-202 行），添加：

```go
Director: func(req *http.Request) {
    // ... 现有代码 ...

    // 删除 Claude Code 设置的假 API Key，注入用户 JWT
    req.Header.Del("Authorization")
    req.Header.Set("X-PairProxy-Auth", tf.AccessToken)

    // 新增：注入模型名称（如果已提取）
    if model, ok := req.Context().Value(modelContextKey).(string); ok && model != "" {
        req.Header.Set("X-PairProxy-Model", model)
    }

    // 告知 s-proxy 本地路由版本
    req.Header.Set("X-Routing-Version", strconv.FormatInt(localVersion, 10))

    // ... 后续代码 ...
},
```

**新增 context key**:
```go
// 在文件顶部添加
type contextKey string
const modelContextKey contextKey = "model"
```

### 4.3 测试方案

#### 单元测试

在 `internal/proxy/cproxy_test.go` 中添加：

```go
// TestCProxy_ExtractModelFromBody 测试从请求体提取模型
func TestCProxy_ExtractModelFromBody(t *testing.T) {
    tests := []struct {
        name     string
        body     []byte
        expected string
    }{
        {
            name:     "Anthropic format",
            body:     []byte(`{"model":"claude-3-5-sonnet-20241022","messages":[]}`),
            expected: "claude-3-5-sonnet-20241022",
        },
        {
            name:     "OpenAI format",
            body:     []byte(`{"model":"gpt-4","messages":[]}`),
            expected: "gpt-4",
        },
        {
            name:     "No model field",
            body:     []byte(`{"messages":[]}`),
            expected: "",
        },
        {
            name:     "Invalid JSON",
            body:     []byte(`not-json`),
            expected: "",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := extractModelFromBody(tt.body)
            if got != tt.expected {
                t.Errorf("extractModelFromBody() = %q, want %q", got, tt.expected)
            }
        })
    }
}

// TestCProxy_InjectsModelHeader 测试是否正确注入模型 header
func TestCProxy_InjectsModelHeader(t *testing.T) {
    var capturedModel string

    mockSProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        capturedModel = r.Header.Get("X-PairProxy-Model")
        w.Header().Set("Content-Type", "application/json")
        _, _ = io.WriteString(w, `{"id":"msg_1","type":"message"}`)
    }))
    defer mockSProxy.Close()

    cp, _ := newTestCProxy(t, mockSProxy.URL, validToken())

    req := httptest.NewRequest(http.MethodPost, "/v1/messages", 
        strings.NewReader(`{"model":"claude-3-opus","messages":[]}`))
    req.Header.Set("Authorization", "Bearer dummy-api-key")
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()
    cp.Handler().ServeHTTP(rr, req)

    if rr.Code != http.StatusOK {
        t.Errorf("status = %d, want 200", rr.Code)
    }
    if capturedModel != "claude-3-opus" {
        t.Errorf("X-PairProxy-Model = %q, want 'claude-3-opus'", capturedModel)
    }
}
```

#### 集成测试

在 `test/e2e/cproxy_e2e_test.go` 中补充：

```go
// TestCProxy_ModelHeaderPropagation_E2E 测试端到端的模型 header 透传
func TestCProxy_ModelHeaderPropagation_E2E(t *testing.T) {
    // 验证 Dashboard 能正确显示模型名称
}
```

---

## 5. 验收标准

### 5.1 功能验收

| 验收项 | 验收标准 | 验证方法 |
|--------|----------|----------|
| Anthropic 格式 | 请求体含 `model` 字段时，Dashboard 正确显示 | E2E 测试 |
| OpenAI 格式 | 请求体含 `model` 字段时，Dashboard 正确显示 | E2E 测试 |
| 缺失 model 字段 | Dashboard 显示 "—" 或从响应体补充 | E2E 测试 |
| 无效 JSON | 不阻断请求，Dashboard 可能显示空 | 单元测试 |

### 5.2 性能验收

| 验收项 | 验收标准 | 验证方法 |
|--------|----------|----------|
| 延迟影响 | 单次请求处理时间增加 < 1ms | Benchmark 测试 |
| 内存占用 | 不引入显著的内存分配 | pprof 分析 |

### 5.3 兼容性验收

| 验收项 | 验收标准 | 验证方法 |
|--------|----------|----------|
| 向后兼容 | 现有请求处理逻辑不受影响 | 回归测试 |
| Header 可选 | sproxy 不强制要求该 header | 单元测试 |

---

## 6. 风险分析

| 风险 | 可能性 | 影响 | 缓解措施 |
|------|--------|------|----------|
| Body 读取冲突 | 低 | 高 | 复用现有 body buffer 逻辑，确保只读取一次 |
| JSON 解析性能 | 低 | 中 | 使用轻量级结构体，只解析 model 字段 |
| 大 Body 内存占用 | 中 | 中 | 限制解析的 body 大小（如只读取前 10KB）|
| 编码问题 | 低 | 低 | 使用标准 json 库，支持 UTF-8 |

---

## 7. 实施计划

### 7.1 任务分解

```
Phase 1: 开发 (2 天)
  ├─ [ ] 实现 extractModelFromBody 函数
  ├─ [ ] 修改 serveProxy 提取模型
  ├─ [ ] 修改 Director 注入 header
  └─ [ ] 添加 context key 定义

Phase 2: 测试 (1 天)
  ├─ [ ] 编写单元测试
  ├─ [ ] 编写集成测试
  ├─ [ ] 性能基准测试
  └─ [ ] 回归测试

Phase 3: Code Review (1 天)
  ├─ [ ] 提交 PR
  ├─ [ ] 代码评审
  └─ [ ] 修改意见处理

Phase 4: 发布 (1 天)
  ├─ [ ] 合并到 main
  ├─ [ ] 打 tag
  └─ [ ] 更新文档
```

### 7.2 相关 PR

- PR 标题: `feat(cproxy): 注入 X-PairProxy-Model header 以支持模型名称透传`
- 关联 Issue: (待创建)

---

## 8. 附录

### 8.1 相关代码文件

| 文件 | 说明 |
|------|------|
| `internal/proxy/cproxy.go` | cproxy 核心实现，需要修改 |
| `internal/proxy/cproxy_test.go` | cproxy 测试，需要补充 |
| `internal/proxy/sproxy.go:1042-1047` | sproxy 的 extractModel 函数 |
| `internal/db/models.go:45` | UsageLog.Model 字段定义 |
| `internal/dashboard/templates/overview.html:55` | Dashboard 模型显示模板 |

### 8.2 参考文档

- [Anthropic API 文档 - 请求格式](https://docs.anthropic.com/)
- [OpenAI API 文档 - 请求格式](https://platform.openai.com/docs/)

### 8.3 变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|----------|------|
| 1.0.0 | 2026-03-07 | 初始版本 | PairProxy Team |

---

**文档结束**
