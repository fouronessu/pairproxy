# CLAUDE.md 模板

> 复制此文件到项目根目录，命名为 `CLAUDE.md`，按说明填写。
> 删除所有以「> 」开头的注释行后使用。
> 核心原则：写 AI 不读代码不会知道的东西，不写 AI 已经知道的废话。

---

# CLAUDE.md

This file provides guidance to Claude Code when working in this repository.

## 项目定位

> 用 1-3 句话描述这是什么项目，解决什么问题。不需要详细，AI 会读代码。
> 重点放在"这个项目的特殊之处"，普通的 web 服务就不用写"这是一个 web 服务"。

[示例]
本项目是企业内网的 LLM API 网关，部署在用户机器（cproxy）和服务端（sproxy）之间。
核心职责是透明代理、token 用量追踪、多租户配额管理。

---

## 快速命令

> 写精确命令，包含完整路径、常用 flag。不要写"运行测试"，要写"怎么运行"。

```bash
# 构建
make build              # 构建主要二进制到 bin/
make build-dev         # 构建所有二进制（含 mock 工具）到 release/

# 测试
make test              # 运行全量测试
make test-race         # 含 race detector（并发改动前必跑）
make test-pkg PKG=./internal/quota/...  # 单包测试
go test -run TestXxx ./internal/proxy/... -v -count=1  # 单个测试

# 代码质量
make fmt               # 格式化（提交前必跑）
make vet               # 静态检查
make lint              # golangci-lint（需先安装）
```

---

## 架构决策（背景知识，不要随意修改）

> 写非直觉的、需要读多个文件才能理解的架构决策。
> 格式：决策描述 + 理由 + 如果违反会发生什么。

### [决策名称，例如：Fail-Open 原则]

```markdown
[示例]
本项目的错误处理有明确的分层原则：

Fail-Open（错误时放行）：
- 配额数据库不可达 → 记录 WARN，放行请求
- 语义路由分类超时 → 降级到完整候选池，放行请求
理由：这些是内部系统，不能因内部故障影响用户的正常使用

Fail-Closed（错误时拒绝）：
- 用户激活状态校验失败 → HTTP 500，拒绝请求
- Cluster shared_secret 为空 → 拒绝所有内部 API 调用
理由：这些是安全边界，不可达时拒绝优于放行

如果不确定，应偏向 Fail-Open——可用性通常优先于严格性。
```

### [决策名称，例如：缓存的安全 bypass]

```markdown
[示例]
本项目有多处缓存，但安全相关的缓存命中后必须二次校验：
- JWT 验证缓存命中 → 仍需调用 IsUserActive（主键索引，< 1ms）
- API Key 缓存命中 → 仍需校验用户激活状态

原因：用户状态可能在 TTL 内变化（管理员禁用用户），
缓存命中不等于用户当前有效。
```

---

## 已知禁区（踩过的坑，不要再踩）

> 这是最重要的部分之一。把已经踩过的坑、做过的反直觉决策记录在这里。
> 格式：描述现象 → 解释原因 → 说明不要改它

```markdown
[示例：执行顺序约束]
### TeeResponseWriter 的操作顺序不能改

proxy/handler.go 里的操作顺序是：
  1. tw.RecordNonStreaming(rawBody)      ← 先用原始格式计 token
  2. convertAnthropicToOpenAI(rawBody)  ← 再做格式转换
  3. w.Write(convertedBody)             ← 最后写给客户端

如果改成"先转换再计 token"，token parser 会看到 OpenAI 格式，
无法解析 Anthropic 的 usage 字段，导致 token 计数全为 0。
```

```markdown
[示例：看似多余其实必要的代码]
### injectOpenAIStreamOptions 会对所有请求执行

proxy/middleware.go 的 injectOpenAIStreamOptions 对所有
/v1/chat/completions 请求无条件注入 stream_options 字段。

这导致 OtoA（OpenAI → Anthropic）转换路径也会收到这个字段。
在 convertOpenAIToAnthropicRequest() 里必须显式删除它。
不要"优化掉"这个删除逻辑——Anthropic 会因为不认识该字段而报错。
```

---

## 代码规范（项目特有的，框架默认的不用写）

> 只写这个项目和语言/框架通用规范不同的地方。

### Logging 层级（Zap）

```markdown
- DEBUG: 每个请求的 token 数、SSE chunk 解析（生产禁用）
- INFO:  生命周期事件（启动/关闭/配置加载）
- WARN:  可恢复的错误，不阻断请求（DB 写失败、health check 失败）
- ERROR: 不可恢复的，需要人工介入（不应该出现却出现了的）

原则：WARN 记录后要继续执行，不要在 WARN 后面跟 return err。
```

### 错误处理

```markdown
- 用 fmt.Errorf("context: %w", err) 包装，不要丢失原始错误
- 公开 API 的所有错误路径都要处理
- 有意忽略的错误加 //nolint:errcheck 注释并说明原因
```

### 数据库（GORM）

```markdown
- 每张表的业务唯一键写在注释里（区别于内部主键 UUID）
- upsert 的 ON CONFLICT 用业务唯一键，不用 uuid 主键
- 异步写用 UsageWriter，不要在请求路径上做同步 DB 写入
```

---

## API 约定

> 只写非标准的、项目特有的约定。

```markdown
- 认证：X-PairProxy-Auth: <jwt> 或 Authorization: Bearer <jwt>（两种都接受）
- Admin API：/api/admin/*（需要 admin JWT）
- 用户 API：/api/user/*（需要普通用户 JWT）
- 内部 API：/api/internal/*（需要 cluster shared_secret）
- 错误响应：{"error": "error_code", "message": "human readable"}
- 分页：page（默认1）、page_size（默认100）
```

---

## 测试约定

```markdown
- 不使用外部 assert 框架，用标准 testing 包
- 测试 helper 命名：setupXxxTest(t *testing.T) 返回 cleanup 函数
- 不依赖外部网络/文件：用 httptest.NewServer、os.TempDir()
- 表驱动测试用 t.Run(tc.name, ...)
- race-sensitive 改动必须跑 make test-race
```

---

## 版本特性标注

> 如果项目有版本迭代，记录重要特性的引入版本。
> 这样 AI 在实现时能判断某个特性是否可用。

```markdown
- v2.15.0+: HMAC-SHA256 API Key（需要 auth.keygen_secret 配置）
- v2.14.0+: PostgreSQL Peer Mode（driver: "postgres" 时自动启用）
- v2.13.0+: PostgreSQL 支持
- v2.9.0+:  Direct Proxy（sk-pp- API Key 直连）
```

---

## 环境信息

> 写开发环境的特殊之处。如果是标准环境（PATH 里有 go）就不用写。

```markdown
[示例：非标准环境]
- Go binary: C:/Program Files/Go/bin/go.exe（Windows 开发环境）
- 配置文件: ./sproxy.yaml（当前目录，或用 --config 指定）
- 数据库: SQLite，路径在 sproxy.yaml 的 database.path 字段
```

---

> ## 填写检查清单
>
> 完成 CLAUDE.md 后，确认以下内容都已覆盖：
>
> - [ ] 项目定位：1-3 句话说明特殊之处
> - [ ] 快速命令：build / test / fmt 的精确命令
> - [ ] 至少一条架构决策（非直觉的部分）
> - [ ] 至少一条已知禁区（踩过的坑）
> - [ ] 代码规范里项目特有的部分（logging 层级最重要）
> - [ ] API 约定里非标准的部分
> - [ ] 删除所有「> 」开头的注释说明行
