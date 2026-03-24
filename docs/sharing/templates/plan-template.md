# Plan 执行计划模板

> Plan 是把 Spec 转化为 AI 可靠执行的操作序列。
> 在有了 Approved 的 Spec 之后使用。
> 删除所有以「> 」开头的注释行后使用。
> 核心原则：每个 Step 都要有 Expected 输出，大任务必须分 Chunk，验证失败是独立 Step。

---

# Plan：[功能名称]

**对应 Spec**: [Spec 文件链接或标题]
**目标版本**: vX.Y.Z
**预计 Chunk 数**: N
**执行模式**: 逐步执行（每个 Task 完成后 commit，再进行下一个）

---

## 元指令（给 AI 的调度说明）

> 如果使用支持 Agent 模式的工具（如 Claude Code），可以在这里写工作模式指令。

```
实现本 Plan 时：
1. 严格按 Chunk 顺序执行，不要跨 Chunk 同时修改
2. 每个 Step 运行命令后，对照 Expected 输出确认结果
3. Step N.2（验证失败）不可跳过——这是防止测试写错的关键
4. 每个 Task 末尾执行 git commit，锁定进度
5. 遇到 Expected 不符时，停下来分析原因，而不是强行继续
```

---

## File Map（范围约束）

> 列出本次改动允许修改的所有文件。
> 不在此列表中的文件，未经说明不要修改。

| 文件路径 | 操作 | 职责说明 |
|---------|------|---------|
| `internal/[pkg]/[file].go` | Create / Modify / Delete | [这个文件负责什么] |
| `internal/[pkg]/[file]_test.go` | Create / Modify | [测试什么] |
| `cmd/[binary]/main.go` | Modify | [修改什么] |
| `config/[file].yaml.example` | Modify | [更新示例配置] |
| `docs/UPGRADE.md` | Modify | [记录 Breaking Change] |

> 如果实现过程中发现需要修改其他文件，先暂停，说明原因，经确认后再继续。

---

## 依赖关系图

> 用简单的文字图说明 Chunk 之间的依赖顺序。

```
Chunk 1（基础层）
    ↓
Chunk 2（核心逻辑）
    ↓
Chunk 3a（路径 A）  Chunk 3b（路径 B）
         ↘         ↙
          Chunk 4（集成层）
```

---

## Chunk 1：[名称，例如：基础类型和检测函数]

> Chunk 命名建议：按"由内到外"的依赖序命名。
> 先做被依赖的（类型定义、基础函数），后做依赖别人的（集成、接线）。

### Task 1.1：[任务名称，例如：定义 conversionDirection 枚举]

---

**Step 1.1.1**：[操作描述，例如：在 converter.go 里添加类型定义]

> [可选：如果代码复杂，在这里写出预期的实现。AI 按此实现，不要发挥。]

```go
// 示例：直接给出预期代码
type conversionDirection int

const (
    conversionNone  conversionDirection = iota
    conversionAtoO  // Anthropic → OpenAI
    conversionOtoA  // OpenAI → Anthropic
)
```

**Expected**：文件修改成功，`go build ./internal/proxy/...` 通过。

---

**Step 1.1.2**：编写测试（先写，此时运行会失败）

```bash
# 运行测试
go test ./internal/[pkg]/... -run Test[功能名] -v -count=1
```

**Expected（预期失败）**：
```
--- FAIL: Test[功能名]
    [file]_test.go:[line]: [具体错误，例如 undefined: detectConversionDirection]
FAIL
```

> 重要：这一步预期看到失败。如果通过了，说明测试没有真正测到预期的代码路径，需要检查测试是否写对。

---

**Step 1.1.3**：实现 `[函数名]` 函数

> [如果有特殊逻辑或容易出错的地方，在这里说明]

```go
// 如果实现逻辑复杂，直接给出代码
func detectConversionDirection(r *http.Request, target *LLMTarget) conversionDirection {
    // ...
}
```

**Expected**：代码保存成功，`go build` 通过。

---

**Step 1.1.4**：运行测试，确认通过

```bash
go test ./internal/[pkg]/... -run Test[功能名] -v -count=1
```

**Expected**：
```
--- PASS: Test[功能名] (0.00s)
    --- PASS: Test[功能名]/[子测试1]
    --- PASS: Test[功能名]/[子测试2]
    ...
PASS
ok  	github.com/[org]/[repo]/internal/[pkg]	0.XXXs
```

---

**Step 1.1.5**：运行完整包测试，确认无回归

```bash
go test ./internal/[pkg]/... -count=1 2>&1 | tail -5
```

**Expected**：最后几行显示 `ok` 或 `PASS`，无 `FAIL`。

---

**Step 1.1.6**：提交

```bash
git add [列出修改的文件]
git commit -m "feat([pkg]): [简洁描述]

- [变更点 1]
- [变更点 2]
- [N] new tests, all passing"
```

---

### Task 1.2：[下一个任务]

> （按需增加 Task）

---

## Chunk 2：[名称，例如：核心转换逻辑]

> 在 Chunk 1 全部完成并 commit 后开始。

### 兼容性 Shim（如果 Chunk 2 的依赖方还未更新）

> 如果 Chunk 2 改变了某个函数的签名，但 Chunk 5（接线）还没有更新调用方，
> 可以在这里加一个临时 shim 函数，让项目保持可编译状态。
> 在 Chunk 5 里删除这个 shim。

```go
// TODO: 临时 shim，Chunk 5 完成后删除
// 保持 main.go 在 Chunk 3、4 施工期间可以编译
func legacyShouldConvert(dir conversionDirection) bool {
    return dir != conversionNone
}
```

### Task 2.1：[任务名称]

> （参照 Task 1.1 的 Step 结构）

**Step 2.1.1**：
**Step 2.1.2**：
...

---

## Chunk 3：[名称，例如：响应转换]

> （按需增加 Chunk）

---

## Chunk N（最后）：[接线，例如：更新主流程，删除 shim]

> 最后一个 Chunk 通常是"把所有东西连起来"和"清理施工临时产物"。

### 前置条件

在开始 Chunk N 之前，确认：
- [ ] Chunk 1 所有 Task 已完成并 commit
- [ ] Chunk 2 所有 Task 已完成并 commit
- [ ] ...（列出所有前置 Chunk）
- [ ] `go test ./...` 全量通过

### Task N.1：删除兼容性 Shim

**Step N.1.1**：删除所有标注 `// TODO: 临时 shim` 的函数

**Step N.1.2**：确认删除后项目仍可编译

```bash
go build ./...
```

**Expected**：无编译错误。

---

### Task N.2：全量测试和最终验收

**Step N.2.1**：运行全量测试

```bash
go test ./... -count=1 2>&1 | tail -20
```

**Expected**：
```
ok  	github.com/.../internal/auth      (0.XXXs)
ok  	github.com/.../internal/db        (0.XXXs)
...
ok  	github.com/.../internal/[pkg]     (0.XXXs)
```
全部 `ok`，无 `FAIL`。

**Step N.2.2**（如果适用）：运行 race detector

```bash
go test -race ./... -count=1
```

**Expected**：全部通过，无 `DATA RACE` 报告。

**Step N.2.3**：运行 E2E 测试

```bash
go test ./test/e2e/... -v -count=1 -run Test[相关功能]
```

**Expected**：相关 E2E 测试全部通过。

---

**Step N.2.4**：撰写验收报告

> 按验收报告模板填写，包含：
> - 测试总数和通过数
> - 覆盖率
> - 测试过程中发现并修复的问题（必填，如无写"无"）
> - 四维度评分

---

**Step N.2.5**：最终提交

```bash
git add .
git commit -m "feat: [功能名] vX.Y.Z

[功能总结，2-3 句话]

- [关键实现点 1]
- [关键实现点 2]
- [N] new tests
- Closes #[issue 编号（如有）]"
```

---

## 附录：常见 Expected 模板

> 复制这些片段到各 Step 的 Expected 字段。

### 编译错误（Step X.2 验证失败时）
```
--- FAIL
    [file]_test.go:[line]: [FunctionName] undefined (type [Type])
FAIL	github.com/.../internal/[pkg] [build failed]
```

### 测试通过
```
--- PASS: Test[Name] (0.00s)
PASS
ok  	github.com/.../internal/[pkg]	0.XXXs
```

### 全量测试汇总
```
ok  	github.com/[org]/[repo]/internal/auth     0.XXXs
ok  	github.com/[org]/[repo]/internal/db       0.XXXs
ok  	github.com/[org]/[repo]/internal/proxy    0.XXXs
```

### 编译成功（无输出）
```
（无输出表示编译成功）
```

---

> ## Plan 编写检查清单（给 Plan 作者用）
>
> - [ ] File Map 覆盖了所有需要修改的文件
> - [ ] Chunk 按"由内到外"的依赖顺序排列
> - [ ] 有兼容性 Shim（如果中间 Chunk 会破坏编译）
> - [ ] 每个复杂 Task 都有预期代码（不靠 AI 发挥）
> - [ ] 每个 Step 都有 Expected 字段
> - [ ] "验证失败"是独立 Step，有明确的 Expected（失败状态）
> - [ ] 每个 Task 末尾有 git commit 指令
> - [ ] 最后一个 Chunk 有全量测试 Step
> - [ ] Out of Scope 的事情在 Plan 里也没有出现
