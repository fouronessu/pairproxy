# AI Coding 的五个控制点：从"帮我写段代码"到"按规格自测自验交付"

> 基于 6 万行 Go 代码、20 个版本的实战方法论
> 不讲怎么写 Prompt，讲怎么系统性地跟 AI 协作交付软件

---

## 开场

两种 AI Coding 的状态：

**状态 A**：随手丢给 AI 一个需求，AI 给了一段代码，粘贴进去，莫名其妙 bug，再问 AI，AI 再改……补丁打补丁，不知道什么时候算完。

**状态 B**：六周之后，60,500 行 Go 代码，20 个版本迭代，验收评分 4.8/5，1,894 个测试 0 失败，每个版本都有设计文档和验收报告存档。

这两种结果的差距，不在于 AI 的能力，在于**你给 AI 的工作环境**。

### 核心命题

> AI 是一个执行能力极强、但记忆极短、判断需要被约束的工程师。
> 你的工作，是设计它的工作环境，而不是跟它聊天。

---

## Part 1：上下文管理——给 AI 的文档和给人的文档，是两件事

### 方法论

这个项目里存在三种文档，写给三种不同的读者：

- `README.md` → 给用户：这个项目是什么，怎么用
- `docs/manual.md` → 给运维：怎么部署，怎么管理
- `CLAUDE.md` / `AGENTS.md` → **给 AI**：怎么在这个代码库里工作

大多数人把 AI 当搜索引擎用，上来就问问题，没有背景。更好的方式，是把 AI 当新入职的工程师对待：**第一件事是让它读项目文档，第二件事才是分配任务**。

### CLAUDE.md 写什么

**❌ 不要写（AI 不需要被提醒的废话）：**
```
- 请认真编写单元测试
- 错误处理要友好
- 不要提交 API Key 到 Git
```

**✅ 应该写（AI 不读代码不会知道的隐性知识）：**

**一、架构决策（尤其是非直觉的部分）**

```markdown
## Fail-Open vs Fail-Closed 原则
这个项目有明确的分层原则，不要随意修改：

- 配额/DB 错误：Fail-Open（放行 + WARN 日志）
  理由：可用性优先，不因内部故障影响用户
- 用户禁用校验：Fail-Closed（返回 HTTP 500）
  理由：安全边界，DB 不可达时宁可拒绝
- 语义路由超时：Fail-Open（降级到完整候选池）
  理由：路由失败不能阻断用户请求
```

**二、已知的坑（已踩过，不要再踩）**

```markdown
## 已知问题和禁区

### TeeResponseWriter 包装顺序不能改
目前的包装顺序是刻意的：
  AnthropicToOpenAIConverter（最内层）
  → TeeResponseWriter（外层，tee 给 token parser）
  → proxy.ServeHTTP
顺序改变会导致 token 计数为 0。

### injectOpenAIStreamOptions 是无条件执行的
对所有 /v1/chat/completions 请求注入 stream_options（包括 OtoA 转换路径）。
在 OtoA 转换里必须显式删除这个字段，Anthropic 不认识它。
```

**三、环境约束（精确到命令）**

```markdown
## 开发环境
- Go binary: C:/Program Files/Go/bin/go.exe
- 单包测试: go test ./internal/quota/... -v -count=1
- 含 race detector: go test -race ./...
- 覆盖率报告: go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
```

**四、代码风格的隐性规则**

```markdown
## Logging 层级（Zap）
- DEBUG: 每个请求的 token 数（生产环境关闭）
- INFO: 生命周期事件（启动/关闭/token 刷新）
- WARN: 可恢复的错误（DB 写失败、health check 失败）
- ERROR: 需要人工介入的（不应该出现又出现了的）

原则：WARN 不阻断请求，ERROR 只在真正无法继续时使用。
```

### 实操教训

这个项目的 CLAUDE.md **第一版**是一份中文运维手册，写满了 `sproxy admin user add` 这类管理命令。

AI 读完之后知道怎么管理用户，但完全不知道如何按项目规范写代码。重写之后把架构决策、fail-open 原则、已知禁区写进去——**第一次就能给出符合项目风格的代码，review 时改动量减少约 70%**。

### 可操作建议

项目启动时，花 2 小时写 CLAUDE.md，把脑子里"不言而喻"的东西写出来。
这是 AI Coding 里杠杆最高的单次投资。

> 模板见：[templates/CLAUDE-md-template.md](./templates/CLAUDE-md-template.md)

---

## Part 2：任务定义——Spec 决定了 AI 能飞多高

### 方法论

大多数人给 AI 的任务是这样的：

> "帮我实现 OpenAI 到 Anthropic 的协议转换"

AI 会给你一个实现——可能跑起来，可能有各种边界问题，你不知道它遗漏了什么，也不知道它做了哪些你不知道的假设。

更好的方式：**给 AI 写代码之前，先让 AI 帮你写 Spec，你来审 Spec，确认之后再实现**。

Spec 把"做什么"和"怎么做"分开。AI 实现之前的对齐成本远低于实现之后的返工成本。

### Spec 文档的四个必备要素

#### 要素一：用具体反例描述问题，不用抽象描述

❌ 抽象（AI 容易误解）：
```
旧的 Key 生成算法存在碰撞漏洞
```

✅ 具体（AI 理解准确）：
```
碰撞场景：
- 用户 alice123 → 字符集 {a, l, i, c, e, 1, 2, 3}
- 用户 321ecila → 字符集 {3, 2, 1, e, c, i, l, a}（完全相同！）

两个用户的字符指纹相同，任何能通过 alice123 验证的 Key，
也能通过 321ecila 的验证。攻击者可以枚举出碰撞 Key。
```

**原因**：AI 处理具体例子的准确率远高于抽象描述。越具体，实现偏差越小。

---

#### 要素二：明确写出已拒绝的备选方案

这是 Spec 里最容易被省略的部分，也是最重要的。

```markdown
## 已拒绝的方案

### 方案 A：双格式并存（新旧 Key 同时支持）
**拒绝原因**：需要维护两套验证逻辑，上线后无法强制迁移，
安全漏洞会长期存在。

### 方案 B：复用 jwt_secret 作为 HMAC 签名密钥
**拒绝原因**：密钥用途必须隔离。jwt_secret 泄露影响认证，
keygen_secret 泄露影响 API Key，不应该是同一个密钥。

### 方案 C：带用户名前缀的混合格式（sk-pp-alice-xxxxx）
**拒绝原因**：暴露了用户名信息，增加了攻击面。
```

**为什么必须写这个**：如果不写，AI 在实现过程中可能"发现"一个看起来更优雅的方案，并悄悄采用。而那个方案很可能已经被你否定了——但 AI 不知道。

---

#### 要素三：Success Criteria 必须量化，必须有四个维度

```markdown
## 完成标准

### 功能性
✅ 同一用户名 + secret → 永远同一个 Key（确定性/幂等性）
✅ 旧算法 Key 升级后立即失效（不支持双格式并存）
✅ keygen_secret 缺失时拒绝启动（硬中止，非警告）
✅ keygen_secret 长度 < 32 字符时拒绝启动

### 性能
✅ Key 生成耗时 < 1ms
✅ Key 验证（无缓存，1000 用户遍历）< 10ms
✅ Key 验证（缓存命中）< 1μs

### 测试
✅ generator.go 和 validator.go 覆盖率 100%
✅ 10,000 个随机用户名测试，无碰撞
✅ 旧格式 Key 被正确拒绝的测试

### 文档和用户预期
✅ UPGRADE.md 明确标注 Breaking Change
✅ 用户文档说明："重新生成"会得到同一个 Key（HMAC 确定性）
✅ Key 轮换说明：修改 keygen_secret 会使所有 Key 失效
```

**注意最后一条**：用户行为预期的文档说明——这条是实际踩坑后补入的（见 Part 3 坑2）。

---

#### 要素四：Out of Scope 明确排除

```markdown
## 不在本次范围内

- 双 secret 支持（宽限期过渡）— 列入 Roadmap，本次不做
- 用户级别的独立 Key 过期时间
- Key 吊销 API（当前通过禁用用户实现）
- 前端 UI 重新设计
```

**为什么必须写**：AI 有"求完整"的倾向。看到"还有这个问题没解决"，它会自发扩展范围，在没有说明的情况下在 PR 里加入额外改动。

> 模板见：[templates/spec-template.md](./templates/spec-template.md)

---

## Part 3：执行控制——Plan 文档的精确度决定 AI 的可靠度

### 方法论

Spec 解决"做什么"，Plan 解决"怎么做"。

很多人跳过 Plan 直接让 AI 实现。对于小任务没问题。对于超过 500 行的改动，会出现：
- AI 改了不该改的文件
- 中途发现某个依赖没建好，只能推倒重来
- 实现完了说"好了"，但有几个子功能其实没做

Plan 文档的核心价值：**把复杂任务分解成 AI 能可靠执行的最小步骤**。

### Plan 文档的五个关键机制

#### 机制一：File Map（范围约束）—— 告诉 AI 只能动哪些文件

任务开始前，先列出"白名单"：

```markdown
## File Map

| 文件                                | 操作   | 职责                            |
|------------------------------------|--------|---------------------------------|
| internal/proxy/converter.go        | Modify | 添加 conversionDirection 枚举   |
| internal/proxy/converter_test.go   | Modify | 重命名 TestShouldConvert → TestDetect |
| cmd/sproxy/main.go                 | Modify | 替换 bool 变量为 enum           |
```

只列 3 个文件。AI 修改了第 4 个文件，就是超出范围——这是可审计的边界。

**效果**：AI 知道"之外的文件不用管"，不会在实现时去"顺手修一下"其他地方。

---

#### 机制二：Chunk 分层（由内到外的依赖序）—— 大任务安全分块

OtoA（OpenAI → Anthropic）协议转换，是这个项目最复杂的特性。整个任务被拆成 5 个 Chunk：

```
Chunk 1: 类型层     — 定义 conversionDirection enum 和检测函数
Chunk 2: 请求转换   — OpenAI 请求体 → Anthropic 请求体
Chunk 3a: 响应转换  — Anthropic 响应 → OpenAI 响应（非流式）
Chunk 3b: 错误转换  — Anthropic 错误格式 → OpenAI 错误格式
Chunk 4: 流式转换   — Anthropic SSE → OpenAI SSE（逐行转换）
Chunk 5: 接线       — 把所有转换逻辑接入 sproxy.go 主流程
```

**关键设计**：Chunk 1 到 4 之间用"兼容性 shim"维持代码可编译。

```go
// Chunk 1 结束后插入的临时兼容函数（Chunk 5 时删掉）
// 让 sproxy.go 在 Chunk 2-4 施工期间仍能编译
func shouldConvertProtocol(convDir conversionDirection) bool {
    return convDir != conversionNone
}
```

**效果**：
- 任何时候都能 `go build`，任何时候都能跑测试
- 可以在 Chunk 3 完成后暂停，第二天从 Chunk 4 继续
- 中途出问题可以只回退单个 Chunk，不影响其他进度

---

#### 机制三：Step 的标准结构（最核心）—— 每步都有预期输出

每个 Step 不能只写"做什么"，必须同时写"预期看到什么"：

```markdown
**Step 2.3**: 运行测试，确认它失败（验证 Red 状态）

```bash
go test ./internal/proxy/... -run TestOtoARequest -v -count=1
```

**Expected（必须看到）**:
```
--- FAIL: TestOtoARequest
    converter_test.go:45: undefined: convertOpenAIToAnthropicRequest
FAIL
```

注意：这里预期的是**编译错误**，不是测试逻辑失败。
如果看到的是 PASS，说明测试写错了，没有真正测到预期的函数。
```

三个要素缺一不可：
1. **精确命令**：含 `-run` 过滤（不用 `go test ./...` 这种大炮打蚊子）
2. **Expected 字段**：不只说"运行测试"，说"预期看到什么"
3. **失败原因解释**：预期的失败是什么类型，为什么应该失败

---

#### 机制四：验证失败是独立的 Step（最容易被省略，最不该省略）

标准的 TDD 循环：

```
Step N.1 → 写失败的测试
Step N.2 → 运行测试，验证它确实失败   ← 这一步最容易被跳过
Step N.3 → 实现功能代码
Step N.4 → 运行测试，验证通过
Step N.5 → 运行完整包测试，确认无回归
Step N.6 → git commit
```

**为什么 Step N.2 不能省**：这是防止 AI 自欺欺人的核心机制。

AI 有时会写完测试直接报告"测试通过"——可能是测试写得有问题，可能是测的不是预期路径。"验证它确实失败"这一步让任何作弊行为无处遁形：功能还没实现，测试就应该失败。如果通过了，一定是测试写错了。

---

#### 机制五：Commit 作为检查点

每个 Task 末尾必须有明确的 commit 指令：

```markdown
**Step 2.6**: 提交当前进度

```bash
git add internal/proxy/converter.go internal/proxy/converter_test.go
git commit -m "feat(proxy): add OtoA request body conversion

- convertOpenAIToAnthropicRequest: maps messages, tools, tool_choice
- Handles tool message merging (consecutive role=tool → tool_result blocks)
- Strips OpenAI-only fields: n, logprobs, presence_penalty, stream_options
- 15 new test cases, all passing"
```
```

**为什么**：
- Commit 是进度的物理锁定，下一个 Task 开始前，这个 Task 的成果已经固化
- AI 在后续 Task 里不会意外"优化掉"已完成的代码
- `git log` 成为客观的完成记录，每个 Chunk 是否真的完成，一目了然

> 模板见：[templates/plan-template.md](./templates/plan-template.md)

### 实操教训：Plan 省略了什么，就会在哪里出问题

**案例：token 计数顺序**

Plan 里如果没有明确写操作顺序，AI 自然的写法是先转换格式，再计 token：

```go
// AI 自然写出的顺序（错误）
body = convertAnthropicToOpenAI(rawBody)  // 先格式转换
tw.RecordNonStreaming(body)               // 再计 token → 看到 OpenAI 格式 → 计出 0
```

正确顺序应该是：

```go
// 正确顺序（必须在 Plan 里显式说明）
tw.RecordNonStreaming(rawBody)            // 先用 Anthropic 原始格式计 token
body = convertAnthropicToOpenAI(rawBody) // 再转给客户端
```

顺序搞反：token 计数全部为 0，数据全乱，而代码看起来"完全正确"。

**这种"执行顺序"的约束，无法从函数签名或类型系统推断，只能在 Plan 里显式写出。**

---

## Part 4：AI 特有的失败模式

### 方法论

AI 生成的代码，主路径通常是对的。坑集中在几类固定的"盲区"。

提前认识这些模式，可以定向 review，而不是全量 review——全量 review 的成本和不用 AI 差不多，定向 review 才能最大化收益。

### 六类失败模式（真实案例）

---

#### 失败模式一：算法属性的副作用改变了用户预期

**案例（v2.15.0）**：HMAC 是确定性函数，同一用户名 + 同一 secret → 永远同一个 Key。

用户点击"重新生成 API Key"按钮，期望得到一个新 Key，但实际得到了完全相同的 Key。

AI 实现了正确的算法，通过了所有测试，但没有考虑"幂等性会改变用户预期"这个 UX 问题。

**防护**：Spec 的 Success Criteria 里，功能正确性之外，必须单独列"用户行为预期"这一条。

---

#### 失败模式二：降级策略过于严格，宁可系统不可用也要"正确"

**案例（v2.9.1）**：协议转换时遇到不支持的 `thinking` 参数 → AI 选择返回 HTTP 400。

结果：Claude Code 开启 extended thinking 模式后，整个工具完全罢工，用户无法发出任何请求。

AI 的逻辑是正确的：遇到不支持的参数就报错。但在中间件/网关场景，**保持服务可用 > 返回精确错误**。

修复：静默剥离 `thinking` 参数，继续转发请求。

**防护**：在 Spec 里针对每种"遇到不支持的输入"的处理策略，明确写出：降级、透传、还是报错。

---

#### 失败模式三：缓存的安全假设——命中即信任

**案例（v2.9.3）**：JWT 验证命中缓存后直接放行，不再校验用户是否仍处于激活状态。

结果：管理员禁用某个用户，但该用户的 JWT 仍在缓存 TTL（24小时）内有效，禁用操作在 TTL 期间完全无效。

AI 遵循的是"缓存命中即有效"的通用缓存原则——技术上正确，但在安全场景下是漏洞。

修复：缓存命中后额外执行一次 `IsUserActive` 查询（主键索引，< 1ms）。

**防护**：在 Spec 里对每个缓存点，明确写出"哪些状态变化需要 bypass 缓存"。安全相关的缓存不能靠 AI 的"常识"。

---

#### 失败模式四：构建元数据静默失效

**案例（v2.9.4）**：

```dockerfile
# Dockerfile 里的 ldflags 路径写错了
RUN go build -ldflags "-X github.com/wrong/path.Version=${VERSION}" ...
```

Go 的 `-X` flag 在路径不存在时**静默跳过，不报错，不警告**。所有发布版本的 `./sproxy version` 始终显示 `dev`。查了很久才发现，已经有几个版本的二进制元数据是错的。

同批次还有一个错误：`golang:1.25-alpine`（1.25 不存在）→ build 失败，相对容易发现。

**防护**：CI 里加断言：

```bash
./sproxy version | grep -qv "^dev$" || { echo "version injection failed"; exit 1; }
```

可验证的构建产物属性，必须在 CI 里显式断言，不能依赖 AI 主动检查。

---

#### 失败模式五：数据库 upsert 冲突键选错——业务语义盲区

**案例（v2.14.0）**：ConfigSyncer 用 `ON CONFLICT(id)` 做 upsert。

问题：`id` 是每次启动时生成的 UUID。Primary 和 Worker 对同一个 LLM Target URL 生成了不同的 UUID。

`ON CONFLICT(id)` 永远不命中 → 走 INSERT → 触发 `url` 的唯一索引 → UNIQUE constraint failed。

Worker 节点日志持续报错，ConfigSyncer 完全失效，两个节点数据开始分叉。

修复（v2.14.1）：`ON CONFLICT(url)` —— 业务唯一标识是 url，不是 uuid。

**防护**：数据库 Schema 里，每张表的"业务唯一键"必须在 Spec 或 Schema 注释里标注。

```sql
-- llm_targets 表
-- 业务唯一键：url（同一 URL 只能有一条记录）
-- id 是内部主键，不是业务标识，不能作为 upsert 的冲突键
CREATE TABLE llm_targets (
    id   TEXT PRIMARY KEY,  -- 内部 UUID，不稳定
    url  TEXT UNIQUE NOT NULL,  -- 业务唯一键，upsert 用这个
    ...
);
```

---

#### 失败模式六：前端视觉交互的盲区

**案例（v2.9.2）**：Dashboard 的"我的用量"页面，图表高度不断增加，最终填满整个页面，无法滚动。

原因：Chart.js 的 `responsive: true` + `maintainAspectRatio: false` 组合，图表高度 = 父元素高度，图表渲染后撑大父元素，父元素变大触发图表重绘，形成正反馈循环。

AI 不会主动在浏览器里打开页面拖动窗口测试，它认为"代码看起来对"就是对了。

修复：用固定高度的 `<div>` 包裹 `<canvas>`：

```html
<div style="height: 300px; position: relative;">
    <canvas id="usageChart"></canvas>
</div>
```

**防护**：前端视觉交互类改动，必须人工在浏览器里跑一遍。
这类问题 AI 无法覆盖，不在"可自动验证"的范围内。

---

### 六类失败模式的共同规律

AI 的失误不在主路径，而在：

```
算法属性的副作用  ×  用户行为预期
降级策略的选取    ×  中间件场景特殊性
缓存有效性假设    ×  安全场景的状态变化
构建产物元数据    ×  静默失败的工具行为
数据库业务语义    ×  内部 ID vs 业务标识
前端视觉交互      ×  无法自动验证
```

**识别这些模式，可以把 review 工作量减少 60% 以上，同时提高发现率。**

> 完整检查表见：[templates/ai-failure-checklist.md](./templates/ai-failure-checklist.md)

---

## Part 5：验收——让 AI 自我审查，让数字说话

### 方法论

代码实现完≠功能交付完。大多数人让 AI 实现完，自己 review 一遍，觉得"差不多"就提交。

更好的做法：**让 AI 用结构化模板验收自己的工作，你审验收报告，而不是审代码**。

验收报告的信噪比远高于代码 review：一份标准的验收报告能在 10 分钟内让你判断"能不能合并"。

### 验收报告的必备结构

```markdown
## 验收报告：[功能名称] v[版本号]

### 测试结果

| 类型         | 总数  | 通过  | 失败 | 状态      |
|-------------|-------|-------|------|-----------|
| 单元测试      | 1,894 | 1,894 | 0    | ✅ PASS   |
| 集成测试      | 8     | 8     | 0    | ✅ PASS   |
| E2E (httptest) | 90+  | 90+   | 0    | ✅ PASS   |
| E2E (真实进程) | 4    | 4     | 0    | ✅ PASS   |

### 覆盖率

| 包                   | 覆盖率 |
|---------------------|--------|
| internal/keygen     | 97.7%  |
| internal/quota      | 95.8%  |
| internal/proxy      | 83.8%  |

### 测试过程中发现并修复的问题

1. **cluster_multinode_e2e_test.go 认证失败（401）**
   - 现象：集群多节点测试认证报 401
   - 根因：doRequest() 函数里多加了 Authorization header，与测试预期冲突
   - 修复：移除该 header
   - 验证：✅ 所有集群测试通过

### 安全性评估
（此功能涉及认证/加密时必填）

### 性能影响
（此功能影响请求路径时必填）

### 四维度评分

| 维度       | 评分   | 说明                          |
|-----------|--------|-------------------------------|
| 功能完整性  | 5/5   | 所有 Spec 要求均已实现         |
| 代码质量   | 5/5   | 符合项目规范，有必要注释        |
| 测试覆盖   | 4/5   | 核心路径 100%，CLI 入口偏低    |
| 文档完整性  | 5/5   | UPGRADE.md 和 manual.md 已更新 |

### 结论

**✅ 通过验收** / ❌ 未通过（原因：...）
```

### 最关键的字段：测试过程中发现并修复的问题

要求 AI 写这个字段，是对"只汇报好消息"动机的定向对抗。

这个字段迫使 AI 把施工过程中遇到的问题暴露出来——**哪怕它自己修了，你也需要知道**。

一个自称"一切顺利"的验收报告，和一个列出"我发现了 3 个问题，修复方式如下"的验收报告，后者可信度远高于前者。

### 数字是不能嘴炮的

```
"所有测试通过"      → 无法审计，可能是没跑测试
"1,894 PASS, 0 FAIL" → 可以审计，运行 go test 就能验证
```

**要求 AI 输出可审计的具体数字，不接受纯文字的"验收通过"结论。**

这不是不信任 AI，而是给 AI 施加"可验证的压力"——知道输出会被核验，AI 的自我审查更认真。

---

## Part 6：方法论总结

### 五个控制点（总览）

```
┌─────────────────────────────────────────────────────────────────┐
│                     AI Coding 五个控制点                        │
├──────────────┬──────────────────────────────────────────────────┤
│ 1. 上下文管理 │ CLAUDE.md：把"不言而喻"写出来                   │
│             │ → 架构决策、已知禁区、环境约束、代码风格           │
├──────────────┼──────────────────────────────────────────────────┤
│ 2. 任务定义  │ Spec：Why + What + Success Criteria              │
│             │ → 具体反例、已拒绝方案、量化标准、Out of Scope    │
├──────────────┼──────────────────────────────────────────────────┤
│ 3. 执行控制  │ Plan：File Map + Chunk + Step + Expected         │
│             │ → 范围约束、依赖序分块、验证失败、Commit 检查点   │
├──────────────┼──────────────────────────────────────────────────┤
│ 4. 验证机制  │ TDD 强制：先写测试 → 验证失败 → 实现 → 验证通过  │
│             │ → 不可跳过"验证失败"这一步                       │
├──────────────┼──────────────────────────────────────────────────┤
│ 5. 交付验收  │ 结构化验收报告：数字 + 过程问题透明              │
│             │ → 可审计的测试数字、主动暴露修复过的 bug          │
└──────────────┴──────────────────────────────────────────────────┘
```

### 三个反直觉洞察

**第一：减少 AI 的决策空间，比扩大 AI 的能力更有效**

把实现代码直接写进 Plan，AI 的工作从"设计 + 实现"变成"按图执行 + 验证"。

看起来"浪费了 AI 的创造力"，但交付的可预测性大幅提升，返工率接近零。

**第二：给 AI 的文档和给人的文档必须分开写**

给人的文档：解释背景，说清楚是什么
给 AI 的文档：约束行为，说清楚做什么和不做什么

混在一起，两头都失效：人看不完那么多约束，AI 提取不到需要的行为规范。

**第三：AI 的失误是有规律可循的，不是随机的**

六类失败模式你现在已经知道了——算法副作用、降级策略、缓存安全、构建元数据、数据库语义、前端交互。

**定向 review 这六类，不做全量 review**。全量 review 的成本趋近于不用 AI。

### 什么时候这套流程值得用

| 场景 | 建议 |
|------|------|
| 单文件 / < 100 行改动 | 直接让 AI 做，简单 review |
| 多文件协同 / 有数据库改动 | 写 Spec，不需要完整 Plan |
| 跨多个包 / 涉及协议或安全边界 | 完整 Spec + Plan + 验收报告 |
| Exploratory / 快速原型阶段 | 不要套这个流程，先跑起来再说 |

**判断标准**：如果这个任务的一个边界条件没考虑到，代价是否承受得起？代价大 → 写 Spec。

### 结尾

回到开场的核心命题：

> AI 改变的是执行的速度和成本。
> 没有改变的是：清晰的需求、合理的任务拆分、可验证的完成标准，
> 永远是高质量软件的前提。

这套方法论的本质，不是发明了什么新东西。而是把软件工程里早已验证过的实践——需求文档、任务拆解、TDD、验收报告——迁移到 AI 协作的语境里。

工具变了。工程的基本规律没变。

---

*基于 PairProxy 项目（v1.0 → v2.18.0）的真实开发过程整理*
*代码规模：60,500+ 行 | 测试：1,894 个 | 版本：20+ | 验收评分：4.8/5.0*
