# PairProxy — 测试场景规格（Test Scenarios）

> **格式**：Given / When / Then
> **用途**：AI coding 时据此生成 Go test 代码（`_test.go` 文件）
> **注意**：每个场景对应一个独立的 `TestXxx` 函数

---

## 1. Auth 包场景

### 1.1 密码 Hash

**S-Auth-01：正确密码验证通过**
- Given: 明文密码 `"MySecretPass123"`，已用 bcrypt hash 存储
- When: 调用 `Verify(hash, "MySecretPass123")`
- Then: 返回 `true`

**S-Auth-02：错误密码验证失败**
- Given: hash 对应密码 `"correct"`
- When: 调用 `Verify(hash, "wrong")`
- Then: 返回 `false`

**S-Auth-03：空密码不 panic**
- When: 调用 `Hash("")`
- Then: 返回 error，不 panic

---

### 1.2 JWT

**S-Auth-04：JWT 完整生命周期**
- Given: secret=`"test-secret"`，claims 含 UserID=`"user-1"`，TTL=`1h`
- When: Sign → Parse
- Then: Parse 返回的 UserID、Username、JTI 与签发时一致；ExpiresAt 在 1h 内

**S-Auth-05：JWT 签名错误**
- Given: 用 secret-A 签发的 token
- When: 用 secret-B 调用 Parse
- Then: 返回 error，errors.Is(err, ErrInvalidSignature)

**S-Auth-06：JWT 已过期**
- Given: TTL=`1ms` 签发的 token
- When: sleep 5ms 后调用 Parse
- Then: 返回 error，errors.Is(err, ErrTokenExpired)

**S-Auth-07：JWT JTI 唯一性**
- Given: 两次调用 Sign（相同 claims）
- When: 比较两个 token 中的 JTI
- Then: JTI 不同（UUID 随机）

---

### 1.3 黑名单

**S-Auth-08：黑名单阻断**
- Given: JTI=`"jti-abc"`，expiry=1h 后
- When: `Blacklist("jti-abc", expiry)` 后调用 `IsBlacklisted("jti-abc")`
- Then: 返回 `true`

**S-Auth-09：不在黑名单的 JTI**
- When: 调用 `IsBlacklisted("unknown-jti")`
- Then: 返回 `false`

**S-Auth-10：黑名单自动清理**
- Given: JTI 以 TTL=`50ms` 加入黑名单
- When: sleep 100ms，等待后台 cleaner 运行
- Then: `IsBlacklisted` 返回 `false`

---

### 1.4 Token 存储

**S-Auth-11：Token 文件读写**
- Given: `TokenFile{AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: 1h后}`
- When: `Save(tmpDir)` 后 `Load(tmpDir)`
- Then: 加载的字段值与保存的完全一致

**S-Auth-12：Token 有效性检查**
- Given: ExpiresAt = now + 2h
- When: `IsValid(tf)`
- Then: 返回 `true`

**S-Auth-13：Token 临近过期视为无效**
- Given: ExpiresAt = now + 20min（< 30min 阈值）
- When: `IsValid(tf)`
- Then: 返回 `false`（触发自动刷新）

**S-Auth-14：文件不存在时 Load 不报错**
- Given: 目录下无 `token.json`
- When: `Load(emptyDir)`
- Then: 返回 `nil, nil`（无 error）

---

## 2. 数据库包场景

### 2.1 连接与迁移

**S-DB-01：内存数据库可正常打开**
- When: `Open(":memory:")`
- Then: 返回有效 `*gorm.DB`，不报错

**S-DB-02：WAL 模式已启用**
- When: `Open("test.db")` 后查询 `PRAGMA journal_mode`
- Then: 返回 `"wal"`

**S-DB-03：迁移创建所有表**
- When: `AutoMigrate(db)` 后查询 sqlite_master
- Then: 存在表：`users`, `groups`, `refresh_tokens`, `usage_logs`, `peers`

---

### 2.2 用户仓库

**S-DB-04：创建并查询用户**
- Given: `User{Username: "john", PasswordHash: hash, GroupID: "g-1"}`
- When: `Create` 后 `GetByUsername("john")`
- Then: 返回的 Username、GroupID 一致；PasswordHash 非空

**S-DB-05：重复用户名报错**
- When: 两次 `Create` 相同 Username
- Then: 第二次返回 error

**S-DB-06：禁用用户**
- When: `SetActive(userID, false)` 后 `GetByID(userID)`
- Then: `IsActive == false`

---

### 2.3 用量仓库

**S-DB-07：批量写入完整性**
- Given: 1000 个不同 RequestID 的 UsageRecord
- When: 逐个 `Record`，然后 `Flush`
- Then: DB 中 count = 1000

**S-DB-08：重复 RequestID 幂等**
- Given: 相同 RequestID 的 UsageRecord
- When: `Record` 两次，`Flush`
- Then: DB 中该 RequestID 只有 1 条

**S-DB-09：按用户和日期范围查询**
- Given: 用户 A 有 5 条记录（3条今天，2条昨天），用户 B 有 2 条记录
- When: `Query({UserID: "A", From: today_start, To: today_end})`
- Then: 返回 3 条，全属于用户 A

**S-DB-10：Token 聚合统计**
- Given: 用户 A 有 3 条记录：input=[100,200,300]，output=[50,100,150]
- When: `SumTokens("A", startOfDay, endOfDay)`
- Then: inputSum=600，outputSum=300

**S-DB-11：ListUnsynced 只返回未同步记录**
- Given: 5 条 synced=false，3 条 synced=true
- When: `ListUnsynced(10)`
- Then: 返回 5 条

**S-DB-12：MarkSynced 更新标记**
- Given: 3 条 synced=false，requestIDs = [r1, r2, r3]
- When: `MarkSynced(["r1","r2"])`
- Then: r1、r2 的 synced=true，r3 仍为 false

---

## 3. 负载均衡器场景

### 3.1 加权随机

**S-LB-01：权重分布统计正确**
- Given: targets = [{id:"a", weight:1}, {id:"b", weight:3}]
- When: `Pick()` 执行 1000 次，统计各 id 被选中次数
- Then: b 的选中次数约为 a 的 3 倍（允许 ±15% 误差）

**S-LB-02：跳过不健康节点**
- Given: targets = [{id:"a", healthy:false}, {id:"b", healthy:true}]
- When: `Pick()` 执行 100 次
- Then: 每次都选中 "b"，从不选中 "a"

**S-LB-03：全部不健康时报错**
- Given: 所有 target 均 healthy=false
- When: `Pick()`
- Then: 返回 `ErrNoHealthyTarget`

**S-LB-04：UpdateTargets 生效**
- Given: 初始 targets = [{id:"a"}]，balancer 已选过 "a"
- When: `UpdateTargets([{id:"b", healthy:true}])`
- Then: 下次 `Pick()` 返回 "b"

---

### 3.2 健康检查

**S-LB-05：健康检查检测宕机**
- Given: mock HTTP 服务器（返回 200），HealthChecker interval=100ms
- When: mock 服务器停止响应，等待 150ms
- Then: Balancer 中对应节点标记为 unhealthy

**S-LB-06：健康检查检测恢复**
- Given: 节点已被标记 unhealthy
- When: mock 服务器恢复响应，等待 150ms
- Then: 节点重新标记为 healthy

**S-LB-07：被动熔断——连续失败**
- Given: 节点 "a" 当前 healthy=true
- When: `ReportFailure("a")` 调用 3 次
- Then: `Balancer.Targets()` 中 "a" 的 healthy=false

**S-LB-08：被动熔断——成功重置计数**
- Given: "a" 已失败 2 次
- When: `ReportSuccess("a")`，再 `ReportFailure("a")` 2 次
- Then: 计数从 0 开始，2 次不触发熔断（需到第 3 次）

---

## 4. SSE 解析器场景

**S-TAP-01：完整 SSE 序列一次性解析**
- Given: 标准 Anthropic SSE 序列（message_start + N×content_block_delta + message_delta + message_stop）
  ```
  event: message_start
  data: {"type":"message_start","message":{"usage":{"input_tokens":150,"output_tokens":0}}}

  event: content_block_delta
  data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}

  event: message_delta
  data: {"type":"message_delta","usage":{"output_tokens":75}}

  event: message_stop
  data: {"type":"message_stop"}
  ```
- When: 一次性 `Feed(allBytes)`
- Then: OnComplete 回调触发，inputTokens=150，outputTokens=75

**S-TAP-02：分块喂入（边界鲁棒性）**
- Given: 与 S-TAP-01 相同的 SSE 序列
- When: 逐字节 `Feed([]byte{b})` 喂入
- Then: 结果与 S-TAP-01 相同

**S-TAP-03：随机分块大小**
- Given: 与 S-TAP-01 相同的 SSE 序列
- When: 随机 1~50 字节分块喂入（模糊测试）
- Then: 结果与 S-TAP-01 相同

**S-TAP-04：无 message_stop 时不触发回调**
- Given: 只有 message_start 和 content_block_delta，无 message_stop
- When: `Feed` 全部数据
- Then: OnComplete 回调不触发，不 panic

**S-TAP-05：非 streaming 响应解析**
- Given: Anthropic 非 streaming JSON 响应体
  ```json
  {"id":"msg-1","type":"message","usage":{"input_tokens":200,"output_tokens":100},"content":[...]}
  ```
- When: `ParseNonStreaming(body)`
- Then: input=200，output=100，无 error

---

## 5. TeeResponseWriter 场景

**S-TAP-06：原始字节完整透传**
- Given: 输入字节序列 `"data: hello\n\ndata: world\n\n"`
- When: 通过 TeeResponseWriter 写入
- Then: 底层 ResponseWriter 收到完全相同的字节，无增删

**S-TAP-07：SSEParser 接收所有字节**
- Given: 分 3 次 Write（每次部分数据）
- When: 全部 Write 完成
- Then: SSEParser 的 Feed 累计收到所有字节

**S-TAP-08：Flush 透传**
- Given: 底层 ResponseWriter 实现了 http.Flusher
- When: 调用 TeeResponseWriter.Flush()
- Then: 底层 Flusher.Flush() 被调用

**S-TAP-09：流结束后 UsageSink 收到记录**
- Given: 完整 Anthropic SSE 序列，含 message_start 和 message_delta
- When: 全部 Write 后调用流结束（context cancel 或显式 Close）
- Then: UsageSink.Record 被调用，UsageRecord 中 InputTokens=150，OutputTokens=75

**S-TAP-10：LLM 返回错误时不记录用量**
- Given: LLM 返回 HTTP 400 响应（非 2xx）
- When: WriteHeader(400) 后写入错误 body
- Then: UsageSink.Record 不被调用（或 InputTokens=OutputTokens=0）

---

## 6. s-proxy Auth 中间件场景

**S-SP-01：无鉴权头返回 401**
- Given: 请求无 `X-PairProxy-Auth` header
- When: 请求到达 AuthMiddleware
- Then: HTTP 401，body=`{"error":"authentication_required"}`

**S-SP-02：有效 JWT 通过**
- Given: 有效 JWT（未过期，未黑名单）
- When: 请求到达 AuthMiddleware
- Then: 请求继续，context 中含 UserID、Username、JTI

**S-SP-03：过期 JWT 返回 401**
- Given: 已过期的 JWT（TTL=1ms，sleep 后）
- When: 请求到达 AuthMiddleware
- Then: HTTP 401，body=`{"error":"token_expired"}`

**S-SP-04：黑名单 JTI 返回 401**
- Given: 有效 JWT，但其 JTI 已加入黑名单
- When: 请求到达 AuthMiddleware
- Then: HTTP 401，body=`{"error":"token_revoked"}`

**S-SP-05：无效签名返回 401**
- Given: 用错误 secret 签发的 JWT
- When: 请求到达 AuthMiddleware
- Then: HTTP 401，body=`{"error":"invalid_token"}`

---

## 7. s-proxy Header 替换场景

**S-SP-06：X-PairProxy-Auth 被删除**
- Given: 请求含 `X-PairProxy-Auth: <JWT>` 头
- When: SProxy 转发到 mock LLM
- Then: mock LLM 收到的请求无 `X-PairProxy-Auth` 头

**S-SP-07：注入真实 Authorization**
- Given: LLMTarget 的 api_key=`"sk-ant-real-key"`
- When: SProxy 转发请求
- Then: mock LLM 收到 `Authorization: Bearer sk-ant-real-key`

**S-SP-08：其他头透传不修改**
- Given: 请求含 `anthropic-version: 2023-06-01`、`anthropic-beta: ...`
- When: SProxy 转发
- Then: mock LLM 收到相同的这些头，值不变

---

## 8. s-proxy 配额中间件场景（sp-1 only）

**S-SP-09：用量在日限额内通过**
- Given: 用户 A，group daily_limit=10000，今日用量=5000
- When: 请求到达 QuotaMiddleware
- Then: 请求继续，不返回 4xx

**S-SP-10：超出日限额返回 429**
- Given: 用户 A，group daily_limit=10000，今日用量=10001
- When: 请求到达 QuotaMiddleware
- Then: HTTP 429，body=`{"error":"quota_exceeded","type":"daily","limit":10000,"used":10001,"reset_at":"<tomorrow_start>"}`

**S-SP-11：无配额限制始终通过**
- Given: 用户 A，group daily_limit=NULL（无限制）
- When: 任意用量
- Then: 请求始终通过

**S-SP-12：配额缓存命中，不重复查 DB**
- Given: QuotaChecker 已缓存用户 A 的用量（60s TTL 未过期）
- When: 连续 10 次请求
- Then: DB 聚合查询只执行 1 次（mock DB 验证）

**S-SP-13：Worker 节点不检查配额**
- Given: s-proxy 以 `cluster.role=worker` 启动
- When: 请求通过
- Then: QuotaMiddleware 不在处理链中（或总是返回 nil）

---

## 9. c-proxy 场景

**S-CP-01：无 token 文件返回明确错误**
- Given: 本地无 `token.json`
- When: 请求到达 c-proxy
- Then: HTTP 401，body=`{"error":"not_authenticated","hint":"run 'cproxy login' first"}`

**S-CP-02：有效 token 注入 JWT 头**
- Given: 有效 `token.json`，Claude Code 发送带任意 `Authorization: Bearer dummy` 的请求
- When: c-proxy 转发
- Then: 转发到 mock s-proxy 的请求包含 `X-PairProxy-Auth: <access_token>`，且不含 `Authorization` 头

**S-CP-03：Streaming 响应完整透传**
- Given: mock s-proxy 返回 SSE streaming 响应（多块分发）
- When: c-proxy 转发
- Then: Claude Code 收到的字节与 mock s-proxy 发送的完全一致，无截断、无合并

**S-CP-04：路由更新头被消费不透传**
- Given: mock s-proxy 响应含 `X-Routing-Version: 2` 和 `X-Routing-Update: ...`
- When: c-proxy 处理响应
- Then: Claude Code 收到的响应中不含 `X-Routing-Version` 和 `X-Routing-Update` 头

**S-CP-05：路由更新版本号递增时更新**
- Given: c-proxy 本地路由 version=1，收到 version=2
- When: 处理响应头
- Then: Balancer 目标列表更新，本地缓存文件 version 变为 2

**S-CP-06：路由更新版本号相同时忽略**
- Given: c-proxy 本地路由 version=3，收到 version=3
- When: 处理响应头
- Then: Balancer 不更新（UpdateTargets 不被调用）

**S-CP-07：s-proxy 不可达时 fallback**
- Given: 路由表 = [{sp-1: weight50, healthy:true}, {sp-2: weight50, healthy:true}]，sp-1 实际不可达
- When: c-proxy 尝试转发，sp-1 连接失败
- Then: 自动选 sp-2 重试，请求最终成功；被动熔断记录 sp-1 失败

**S-CP-08：所有 s-proxy 不可达时返回 503**
- Given: 所有路由节点均不可达
- When: c-proxy 尝试转发
- Then: HTTP 503，body=`{"error":"all_upstreams_unavailable"}`

---

## 10. 集群——Peer 注册场景

**S-CL-01：sp-2 注册成功**
- Given: sp-1 运行，sp-2 发送 `POST /api/internal/register`，body=`{"addr":"http://sp-2:9000","weight":50,"node_id":"sp-2"}`
- When: sp-1 处理注册
- Then: sp-1 DB `peers` 表中出现 sp-2 记录；路由表 version++；响应 `{"accepted":true}`

**S-CL-02：sp-2 重复注册（心跳）**
- Given: sp-2 已注册
- When: sp-2 再次发送相同注册请求
- Then: `peers` 表不新增记录；`last_seen` 更新；路由表 version 不变（幂等，无实质变化）

**S-CL-03：路由端点返回正确路由表**
- Given: sp-1 路由表含 sp-1(weight=50) 和 sp-2(weight=50)
- When: `GET /cluster/routing`
- Then: 响应 JSON 中 routes 含两个条目，weight 正确

---

## 11. 集群——用量上报场景

**S-CL-04：sp-2 批量上报成功**
- Given: sp-2 本地 usage_logs 有 30 条 synced=false
- When: Reporter 触发上报，mock sp-1 接收成功（返回 200）
- Then: sp-2 本地 30 条记录 synced=true；mock sp-1 收到 30 条 UsageItem

**S-CL-05：sp-1 幂等处理重复上报**
- Given: 同一批 request_id 上报两次
- When: sp-1 处理第二次请求
- Then: DB 中每个 request_id 只有 1 条记录；响应 accepted=0（无新增）

**S-CL-06：sp-1 不可达时指数退避重试**
- Given: mock sp-1 不可达（连接拒绝）
- When: Reporter 尝试上报，sp-1 第 3 次才可用
- Then: sp-2 在 sp-1 恢复后成功上报，重试间隔约 10s、20s

---

## 12. 登录流程场景

**S-LOGIN-01：正确凭据获取 token**
- Given: DB 中用户 `john`，密码 hash 正确
- When: `POST /auth/login` body=`{"username":"john","password":"correct"}`
- Then: HTTP 200，响应含 `access_token`、`refresh_token`、`expires_in`；DB 更新 `last_login_at`

**S-LOGIN-02：错误密码**
- Given: DB 中用户 `john`
- When: `POST /auth/login` body 密码错误
- Then: HTTP 401，body=`{"error":"invalid_credentials"}`（不暴露密码对错原因）

**S-LOGIN-03：不存在的用户**
- When: `POST /auth/login` 用不存在的 username
- Then: HTTP 401，body=`{"error":"invalid_credentials"}`（与错误密码返回相同，防止用户枚举）

**S-LOGIN-04：禁用用户无法登录**
- Given: DB 中用户 `bob`，is_active=false
- When: `POST /auth/login` 用 bob 的正确凭据
- Then: HTTP 403，body=`{"error":"account_disabled"}`

**S-LOGIN-05：Token 刷新**
- Given: 有效 refresh_token
- When: `POST /auth/refresh` body=`{"refresh_token":"..."}`
- Then: HTTP 200，返回新 access_token；旧 access_token 在 24h 内仍有效（未加黑名单）

**S-LOGIN-06：已撤销 refresh_token 不可刷新**
- Given: refresh_token 已被 revoke（DB 中 revoked=true）
- When: `POST /auth/refresh`
- Then: HTTP 401，body=`{"error":"token_revoked"}`

---

## 13. Admin 操作场景

**S-ADMIN-01：创建用户**
- When: Admin 调用 `POST /api/admin/users` body=`{"username":"alice","group_id":"g-1","password":"pass"}`
- Then: DB 中出现用户 alice，PasswordHash 非空；响应返回 user_id

**S-ADMIN-02：禁用用户后无法登录**
- Given: 用户 `carol` 存在且活跃
- When: Admin 调用 `PATCH /api/admin/users/:id` body=`{"is_active":false}`，carol 再尝试登录
- Then: carol 登录返回 403

**S-ADMIN-03：撤销用户 token**
- Given: 用户 `dave` 有有效的 access_token（JTI=`"jti-dave"`）
- When: Admin 调用 `POST /api/admin/token/revoke` body=`{"user_id":"dave"}`
- Then: dave 用旧 access_token 发请求 → 401（黑名单命中）；旧 refresh_token 刷新 → 401（DB revoked）

**S-ADMIN-04：设置分组配额**
- When: Admin 调用 `PATCH /api/admin/groups/:id` body=`{"daily_token_limit":5000}`
- Then: DB group 的 `DailyTokenLimit=5000`；用量超 5000 的用户开始收到 429

---

## 14. Dashboard 场景

**S-DASH-01：未登录跳转**
- Given: Dashboard 功能已启用
- When: 浏览器访问 `GET /dashboard`，无 session cookie
- Then: HTTP 302，重定向到 `/dashboard/login`

**S-DASH-02：Admin 登录后可访问**
- Given: Admin 已登录，有有效 session cookie
- When: `GET /dashboard`
- Then: HTTP 200，响应包含 `<html>` 标签和今日统计数据

**S-DASH-03：统计 API 聚合正确**
- Given: DB 中今日有 3 个用户各 100 次请求，每次 input=100 output=50 tokens
- When: `GET /api/stats/summary?from=today_start&to=today_end`
- Then: `{"total_requests":300,"total_input_tokens":30000,"total_output_tokens":15000,"active_users":3}`

---

## 15. 跨平台场景

**S-PLAT-01：Windows 配置目录正确**
- Given: 运行于 Windows 环境（GOOS=windows）
- When: `DefaultConfigDir()` 调用
- Then: 路径包含 `AppData\Roaming\pairproxy`（或 `%APPDATA%\pairproxy`）

**S-PLAT-02：Linux 配置目录正确**
- Given: 运行于 Linux 环境（GOOS=linux）
- When: `DefaultConfigDir()` 调用
- Then: 路径为 `$HOME/.config/pairproxy`

**S-PLAT-03：文件路径使用 filepath.Join**
- Given: 代码搜索
- When: grep `path + "/" | path + "\\"` 在 internal/ 目录
- Then: 无硬编码路径分隔符（全部使用 filepath.Join）

**S-PLAT-04：纯 Go SQLite 驱动无 CGO 编译**
- When: `CGO_ENABLED=0 go build ./...`（Linux/macOS 上测试 Windows 交叉编译）
- Then: 编译成功，无 CGO 相关错误

**S-PLAT-05：Windows 下文件权限错误被忽略**
- Given: 运行于 Windows
- When: `TokenStore.Save()` 调用 `os.Chmod(path, 0600)`
- Then: 函数返回 nil error（Chmod 错误被 swallowed，文件正常写入）
