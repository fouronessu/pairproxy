# PairProxy — Go 接口契约（Interface Spec）

> **说明**：本文件定义所有包的公共接口和核心数据类型。
> 各 AI coding session 实现对应包时，以此为准，确保跨包编译兼容。
> 接口定义为规范，实现细节见 TODO.md 各 Task。

---

## 包结构总览

```
internal/
├── config/     配置加载
├── auth/       JWT、密码、Token 存储
├── db/         数据库连接、模型、仓库
├── lb/         负载均衡器
├── tap/        SSE 解析、响应流拦截
├── quota/      配额检查
├── cluster/    集群管理（路由表、注册、上报）
├── proxy/      c-proxy 和 s-proxy 核心处理器
└── api/        HTTP 处理器（认证、Admin、Stats）
```

---

## 1. config 包

```go
package config

import "time"

// CProxyConfig c-proxy 完整配置
type CProxyConfig struct {
    Listen  ListenConfig  `yaml:"listen"`
    SProxy  SProxyConfig  `yaml:"sproxy"`
    Auth    CProxyAuth    `yaml:"auth"`
    Log     LogConfig     `yaml:"log"`
}

type ListenConfig struct {
    Host string `yaml:"host"` // 默认 "127.0.0.1"
    Port int    `yaml:"port"` // 默认 8080
}

type SProxyConfig struct {
    Primary            string        `yaml:"primary"`             // 初始 sp-1 地址
    LBStrategy         string        `yaml:"lb_strategy"`         // "weighted_random"（当前唯一策略）
    HealthCheckInterval time.Duration `yaml:"health_check_interval"` // 默认 30s
    RequestTimeout     time.Duration `yaml:"request_timeout"`     // 默认 300s
}

type CProxyAuth struct {
    TokenDir        string        `yaml:"token_dir"`        // 默认 os.UserConfigDir()+"/pairproxy"
    AutoRefresh     bool          `yaml:"auto_refresh"`     // 默认 true
    RefreshThreshold time.Duration `yaml:"refresh_threshold"` // 默认 30m
}

type LogConfig struct {
    Level string `yaml:"level"` // "debug"|"info"|"warn"|"error"
}

// SProxyFullConfig s-proxy 完整配置
type SProxyFullConfig struct {
    Listen   ListenConfig    `yaml:"listen"`
    LLM      LLMConfig       `yaml:"llm"`
    Database DatabaseConfig  `yaml:"database"`
    Auth     SProxyAuth      `yaml:"auth"`
    Admin    AdminConfig     `yaml:"admin"`
    Cluster  ClusterConfig   `yaml:"cluster"`
    Dashboard DashboardConfig `yaml:"dashboard"`
    Log      LogConfig       `yaml:"log"`
}

type LLMConfig struct {
    LBStrategy     string         `yaml:"lb_strategy"`     // "round_robin"
    RequestTimeout time.Duration  `yaml:"request_timeout"` // 默认 300s
    Targets        []LLMTarget    `yaml:"targets"`
}

type LLMTarget struct {
    URL    string `yaml:"url"`     // e.g. "https://api.anthropic.com"
    APIKey string `yaml:"api_key"` // 支持 ${ENV_VAR}
    Weight int    `yaml:"weight"`  // 默认 1
}

type DatabaseConfig struct {
    Path            string        `yaml:"path"`              // SQLite 文件路径
    WriteBufferSize int           `yaml:"write_buffer_size"` // 默认 200
    FlushInterval   time.Duration `yaml:"flush_interval"`    // 默认 5s
}

type SProxyAuth struct {
    JWTSecret       string        `yaml:"jwt_secret"`        // 支持 ${ENV_VAR}
    AccessTokenTTL  time.Duration `yaml:"access_token_ttl"`  // 默认 24h
    RefreshTokenTTL time.Duration `yaml:"refresh_token_ttl"` // 默认 168h (7d)
}

type AdminConfig struct {
    PasswordHash string `yaml:"password_hash"` // bcrypt hash，支持 ${ENV_VAR}
}

type ClusterConfig struct {
    Role            string        `yaml:"role"`             // "primary" | "worker"
    Primary         string        `yaml:"primary"`          // worker 用：sp-1 地址
    SelfAddr        string        `yaml:"self_addr"`        // 自己的对外地址
    SelfWeight      int           `yaml:"self_weight"`      // 建议权重，默认 50
    AlertThreshold  int           `yaml:"alert_threshold"`  // active_req 告警线，默认 80
    AlertWebhook    string        `yaml:"alert_webhook"`    // 可选，Webhook URL
    ReportInterval  time.Duration `yaml:"report_interval"`  // worker 上报间隔，默认 30s
    PeerMonitorInterval time.Duration `yaml:"peer_monitor_interval"` // primary 监控 peer，默认 30s
}

type DashboardConfig struct {
    Enabled bool `yaml:"enabled"` // 默认 true（primary 节点）
}
```

---

## 2. auth 包

```go
package auth

import (
    "time"
    "github.com/golang-jwt/jwt/v5"
)

// JWTClaims JWT payload
type JWTClaims struct {
    UserID   string `json:"sub"`
    Username string `json:"username"`
    GroupID  string `json:"group_id"`
    Role     string `json:"role"`    // "user" | "admin"
    JTI      string `json:"jti"`     // 唯一 ID（用于撤销）
    jwt.RegisteredClaims
}

// JWTManager JWT 签发与验证
type JWTManager interface {
    // Sign 签发 token，TTL 决定过期时间
    Sign(claims JWTClaims, ttl time.Duration) (string, error)

    // Parse 解析并验证 token，返回 claims；过期、签名错误等均返回 error
    Parse(tokenStr string) (*JWTClaims, error)

    // IsBlacklisted 检查 JTI 是否在黑名单中
    IsBlacklisted(jti string) bool

    // Blacklist 将 JTI 加入黑名单，TTL 后自动清理
    Blacklist(jti string, expiry time.Time)
}

// PasswordHasher 密码 hash 与验证
type PasswordHasher interface {
    Hash(plain string) (string, error)
    Verify(hash, plain string) bool
}

// TokenFile 本地存储的 token（c-proxy 用）
type TokenFile struct {
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    ExpiresAt    time.Time `json:"expires_at"`
    ServerAddr   string    `json:"server_addr"`
}

// TokenStore c-proxy 本地 token 文件读写
type TokenStore interface {
    // Load 从指定目录加载 token.json；文件不存在返回 nil, nil
    Load(dir string) (*TokenFile, error)

    // Save 保存 token.json，设置文件权限 0600（Windows 忽略权限错误）
    Save(dir string, tf *TokenFile) error

    // IsValid 检查 access_token 是否有效（未过期且提前 30min 视为将过期返回 false）
    IsValid(tf *TokenFile) bool

    // DefaultDir 返回跨平台默认目录（os.UserConfigDir()+"/pairproxy"）
    DefaultDir() string
}
```

---

## 3. db 包

```go
package db

import (
    "time"
    "gorm.io/gorm"
)

// ===== GORM 模型 =====

type Group struct {
    ID                 string    `gorm:"primarykey"`
    Name               string    `gorm:"uniqueIndex;not null"`
    DailyTokenLimit    *int64    // NULL = 无限制
    MonthlyTokenLimit  *int64
    CreatedAt          time.Time
}

type User struct {
    ID           string `gorm:"primarykey"`
    Username     string `gorm:"uniqueIndex;not null"`
    PasswordHash string `gorm:"not null"`
    GroupID      string
    Group        Group  `gorm:"foreignKey:GroupID"`
    IsActive     bool   `gorm:"default:true"`
    CreatedAt    time.Time
    LastLoginAt  *time.Time
}

type RefreshToken struct {
    JTI       string    `gorm:"primarykey"`
    UserID    string    `gorm:"not null;index"`
    ExpiresAt time.Time `gorm:"not null"`
    Revoked   bool      `gorm:"default:false"`
    CreatedAt time.Time
}

type UsageLog struct {
    ID           uint      `gorm:"primarykey;autoIncrement"`
    RequestID    string    `gorm:"uniqueIndex;not null"`
    UserID       string    `gorm:"not null;index"`
    Model        string
    InputTokens  int       `gorm:"default:0"`
    OutputTokens int       `gorm:"default:0"`
    TotalTokens  int       `gorm:"default:0"`
    IsStreaming  bool      `gorm:"default:false"`
    UpstreamURL  string
    StatusCode   int
    DurationMs   int64
    SourceNode   string    `gorm:"default:'local'"`
    Synced       bool      `gorm:"default:false;index"` // sp-2+ 用：是否已上报
    CreatedAt    time.Time `gorm:"index"`
}

type Peer struct {
    ID           string     `gorm:"primarykey"` // e.g. "sp-2"
    Addr         string     `gorm:"uniqueIndex;not null"`
    Weight       int        `gorm:"default:50"`
    IsActive     bool       `gorm:"default:true"`
    RegisteredAt time.Time
    LastSeen     *time.Time
}

// ===== 仓库接口 =====

// UserRepo 用户 CRUD
type UserRepo interface {
    Create(u *User) error
    GetByUsername(username string) (*User, error)
    GetByID(id string) (*User, error)
    SetActive(id string, active bool) error
    UpdateLastLogin(id string, at time.Time) error
    ListByGroup(groupID string) ([]User, error)
}

// GroupRepo 分组 CRUD
type GroupRepo interface {
    Create(g *Group) error
    GetByID(id string) (*Group, error)
    GetByName(name string) (*Group, error)
    SetQuota(id string, daily, monthly *int64) error
    List() ([]Group, error)
}

// RefreshTokenRepo 刷新 token 管理
type RefreshTokenRepo interface {
    Create(rt *RefreshToken) error
    GetByJTI(jti string) (*RefreshToken, error)
    RevokeByUserID(userID string) error  // 撤销用户所有 refresh token
    DeleteExpired() error                 // 清理过期记录（定期调用）
}

// UsageFilter 用量查询过滤条件
type UsageFilter struct {
    UserID    string
    GroupID   string
    From      *time.Time
    To        *time.Time
    Model     string
    Limit     int
    Offset    int
}

// UsageRecord 用量写入数据（非 GORM 模型，用于 channel 传递）
type UsageRecord struct {
    RequestID    string
    UserID       string
    Model        string
    InputTokens  int
    OutputTokens int
    IsStreaming  bool
    UpstreamURL  string
    StatusCode   int
    DurationMs   int64
    SourceNode   string
    CreatedAt    time.Time
}

// UsageRepo 用量日志仓库
type UsageRepo interface {
    // Record 非阻塞写入（内部 channel + 批量 goroutine）
    Record(r UsageRecord)

    // Flush 强制立即写入（graceful shutdown 调用）
    Flush()

    // Query 查询用量日志
    Query(filter UsageFilter) ([]UsageLog, error)

    // SumTokens 聚合某用户在时间范围内的 token 用量
    SumTokens(userID string, from, to time.Time) (inputSum, outputSum int64, err error)

    // ListUnsynced 查询未上报记录（sp-2 用）
    ListUnsynced(limit int) ([]UsageLog, error)

    // MarkSynced 标记记录为已上报（sp-2 用）
    MarkSynced(requestIDs []string) error
}

// PeerRepo 集群 peer 管理（sp-1 only）
type PeerRepo interface {
    Upsert(p *Peer) error                    // 注册或更新 peer
    GetByAddr(addr string) (*Peer, error)
    List() ([]Peer, error)
    UpdateLastSeen(addr string, at time.Time) error
    SetActive(addr string, active bool) error
}
```

---

## 4. lb 包

```go
package lb

import "errors"

// ErrNoHealthyTarget 所有节点均不可用
var ErrNoHealthyTarget = errors.New("no healthy target available")

// Target 负载均衡目标节点
type Target struct {
    ID      string // 唯一标识，e.g. "sp-1"
    Addr    string // HTTP 地址，e.g. "http://sp-1:9000"
    Weight  int    // 权重，≥1
    Healthy bool   // 当前是否健康
}

// Balancer 负载均衡器接口
type Balancer interface {
    // Pick 选择一个健康节点，无可用返回 ErrNoHealthyTarget
    Pick() (*Target, error)

    // MarkHealthy 标记节点为健康
    MarkHealthy(id string)

    // MarkUnhealthy 标记节点为不健康（跳过直到恢复）
    MarkUnhealthy(id string)

    // UpdateTargets 原子替换目标列表（扩容/缩容时调用）
    UpdateTargets(targets []Target)

    // Targets 返回当前目标列表（含健康状态）
    Targets() []Target
}

// HealthChecker 后台健康检查
type HealthChecker interface {
    // Start 启动后台检查 goroutine
    Start(ctx context.Context)

    // ReportFailure 被动熔断：请求失败时调用，连续 3 次 → MarkUnhealthy
    ReportFailure(id string)

    // ReportSuccess 请求成功时调用，重置失败计数
    ReportSuccess(id string)
}
```

---

## 5. tap 包

```go
package tap

import "net/http"

// UsageSink 用量写入接口（解耦 tap 和 db 包）
type UsageSink interface {
    Record(r UsageRecord)
}

// UsageRecord 拦截到的用量（由 tap 包发往 db 包）
type UsageRecord struct {
    RequestID    string
    UserID       string
    Model        string        // 从请求体提取
    InputTokens  int
    OutputTokens int
    IsStreaming  bool
    UpstreamURL  string
    StatusCode   int
    DurationMs   int64
    SourceNode   string
}

// SSEParser Anthropic SSE 流解析器接口
type SSEParser interface {
    // Feed 喂入任意大小的字节块（线程安全）
    Feed(data []byte)

    // OnComplete 注册流完成回调（message_stop 事件触发）
    OnComplete(fn func(inputTokens, outputTokens int))

    // Reset 重置状态（复用时调用）
    Reset()
}

// TeeResponseWriter 同时转发响应并旁路解析 SSE
// 实现 http.ResponseWriter 和 http.Flusher
type TeeResponseWriter interface {
    http.ResponseWriter
    http.Flusher

    // StatusCode 返回已写入的 HTTP 状态码
    StatusCode() int

    // BytesWritten 返回已写入的字节数
    BytesWritten() int64
}

// NewTeeResponseWriter 构造函数
// sink: 用量写入目标
// requestID, userID, upstreamURL: 用于构建 UsageRecord
func NewTeeResponseWriter(
    w http.ResponseWriter,
    sink UsageSink,
    requestID, userID, upstreamURL string,
    isStreaming bool,
) TeeResponseWriter
```

---

## 6. quota 包

```go
package quota

import (
    "context"
    "errors"
    "time"
)

// ErrQuotaExceeded 配额超限错误，含详情供 HTTP 响应使用
type ErrQuotaExceeded struct {
    Type    string    // "daily" | "monthly"
    Limit   int64
    Used    int64
    ResetAt time.Time // 下次重置时间
}

func (e *ErrQuotaExceeded) Error() string

// QuotaChecker 配额检查（sp-1 only）
type QuotaChecker interface {
    // Check 检查用户是否超出配额。
    // 超限返回 *ErrQuotaExceeded；无配额或未超限返回 nil。
    Check(ctx context.Context, userID string) error

    // Invalidate 主动清除某用户的缓存（用量写入后调用）
    Invalidate(userID string)
}
```

---

## 7. cluster 包

```go
package cluster

import (
    "context"
    "time"
)

// RoutingEntry 单个节点的路由条目
type RoutingEntry struct {
    ID      string `json:"id"`
    Addr    string `json:"addr"`
    Weight  int    `json:"weight"`
    Healthy bool   `json:"healthy"`
}

// RoutingTable 路由表（由 sp-1 维护，下发给 c-proxy）
type RoutingTable struct {
    Version int64          `json:"version"` // 单调递增
    Primary string         `json:"primary"` // sp-1 地址
    Routes  []RoutingEntry `json:"routes"`
}

// EncodeForHeader 将路由表编码为响应头值（Base64+JSON）
func (rt *RoutingTable) EncodeForHeader() string

// DecodeFromHeader 从响应头值解码路由表
func DecodeFromHeader(val string) (*RoutingTable, error)

// Manager 集群管理器（sp-1 only）
type Manager interface {
    // CurrentRouting 获取当前路由表
    CurrentRouting() *RoutingTable

    // RegisterPeer sp-2 注册时调用，更新路由表（version++）
    RegisterPeer(addr string, weight int) error

    // UpdatePeerSeen 更新 peer 最后心跳时间
    UpdatePeerSeen(addr string)

    // SetPeerWeight Admin 手动调整权重（version++）
    SetPeerWeight(addr string, weight int) error

    // DisablePeer 下线某 peer（version++）
    DisablePeer(addr string) error
}

// Reporter 用量上报器（sp-2 only）
type Reporter interface {
    // Start 启动后台上报 goroutine
    Start(ctx context.Context)

    // ForceReport 立即触发一次上报（graceful shutdown 用）
    ForceReport() error
}

// RegisterRequest sp-2 向 sp-1 注册的请求体
type RegisterRequest struct {
    Addr   string `json:"addr"`
    Weight int    `json:"weight"`
    NodeID string `json:"node_id"` // 唯一节点名，e.g. "sp-2"
}

// RegisterResponse 注册响应
type RegisterResponse struct {
    Accepted       bool  `json:"accepted"`
    RoutingVersion int64 `json:"routing_version"`
}

// UsageReportRequest 用量批量上报请求体
type UsageReportRequest struct {
    ReporterID string      `json:"reporter_id"` // 上报节点 ID
    Records    []UsageItem `json:"records"`
}

type UsageItem struct {
    RequestID    string    `json:"request_id"`
    UserID       string    `json:"user_id"`
    Model        string    `json:"model"`
    InputTokens  int       `json:"input_tokens"`
    OutputTokens int       `json:"output_tokens"`
    IsStreaming  bool      `json:"is_streaming"`
    UpstreamURL  string    `json:"upstream_url"`
    StatusCode   int       `json:"status_code"`
    DurationMs   int64     `json:"duration_ms"`
    CreatedAt    time.Time `json:"created_at"`
}

// UsageReportResponse 上报响应
type UsageReportResponse struct {
    Accepted int `json:"accepted"` // 实际写入条数（INSERT OR IGNORE）
}
```

---

## 8. proxy 包

```go
package proxy

import "net/http"

// CProxy c-proxy 核心处理器
type CProxy struct {
    // 内部字段省略，由构造函数注入
}

// NewCProxy 构造函数
// tokenStore: 本地 token 存取
// balancer: s-proxy 负载均衡器
// routingTable: 路由表（含磁盘持久化）
func NewCProxy(tokenStore auth.TokenStore, balancer lb.Balancer, ...) *CProxy

// ServeHTTP 实现 http.Handler
func (c *CProxy) ServeHTTP(w http.ResponseWriter, r *http.Request)

// SProxy s-proxy 核心处理器
type SProxy struct{}

// NewSProxy 构造函数
// jwtMgr: JWT 验证
// balancer: LLM 负载均衡器
// usageRepo: 用量写入
// quotaChecker: 配额检查（nil 表示不检查，worker 节点使用）
// clusterMgr: 集群管理（nil 表示 worker 节点）
func NewSProxy(
    jwtMgr auth.JWTManager,
    balancer lb.Balancer,
    usageRepo db.UsageRepo,
    quotaChecker quota.QuotaChecker, // worker 传 nil
    clusterMgr cluster.Manager,      // worker 传 nil
) *SProxy

// ServeHTTP 实现 http.Handler
func (s *SProxy) ServeHTTP(w http.ResponseWriter, r *http.Request)
```

---

## 9. HTTP API 接口清单

### 公共接口（无需鉴权）
```
GET  /health
Response: {"status":"ok","role":"primary|worker","version":"1.0.0","active_req":N}

GET  /cluster/routing
Response: RoutingTable JSON
```

### 认证接口
```
POST /auth/login
Body:     {"username":"john","password":"****"}
Response: {"access_token":"...","refresh_token":"...","expires_in":86400}
Errors:   401 {"error":"invalid_credentials"}

POST /auth/refresh
Body:     {"refresh_token":"..."}
Response: {"access_token":"...","expires_in":86400}
Errors:   401 {"error":"token_revoked|token_expired"}

POST /auth/logout
Header:   X-PairProxy-Auth: <access_token>
Response: 204 No Content
```

### 内部接口（节点间，无需用户鉴权，限内网）
```
POST /api/internal/register
Body:     RegisterRequest
Response: RegisterResponse

POST /api/internal/usage-report
Body:     UsageReportRequest
Response: UsageReportResponse
```

### Admin 接口（需 Admin Session）
```
POST /api/admin/login
Body:     {"password":"****"}
Response: {"session_token":"...","expires_in":3600}

POST /api/admin/users
GET  /api/admin/users
GET  /api/admin/users/:id
PATCH /api/admin/users/:id          Body: {"is_active":false}
POST /api/admin/users/:id/reset-password

POST /api/admin/groups
GET  /api/admin/groups
PATCH /api/admin/groups/:id         Body: {"daily_token_limit":N,"monthly_token_limit":N}

GET  /api/admin/peers
PATCH /api/admin/peers/:id          Body: {"weight":N,"is_active":false}

POST /api/admin/token/revoke        Body: {"user_id":"..."}
```

### 统计接口（需 Admin Session）
```
GET /api/stats/summary              ?from=&to=
Response: {"total_input_tokens":N,"total_output_tokens":N,"total_requests":N,"active_users":N}

GET /api/stats/users                ?from=&to=&limit=20&offset=0
Response: [{"user_id":"...","username":"...","total_tokens":N,...}]

GET /api/stats/usage                ?user_id=&from=&to=&group_by=day
Response: [{"date":"2024-01-01","input_tokens":N,"output_tokens":N}]

GET /api/stats/logs                 ?user_id=&limit=50
Response: [UsageLog...]
```

---

## 10. 关键响应头（路由传播）

```
# s-proxy (sp-1) → c-proxy 响应头（路由表有更新时注入）
X-Routing-Version: 5
X-Routing-Update: <Base64(JSON(RoutingTable))>

# c-proxy 处理：
#   本地 version < 收到 version → 解析更新 Balancer + 写磁盘缓存 + 从响应头删除
#   本地 version >= 收到 version → 忽略

# 标准 Anthropic API 头（透传，不修改）
anthropic-version: 2023-06-01
anthropic-beta: ...
content-type: text/event-stream (streaming) | application/json
```

---

## 11. 跨平台约定

```go
// 所有文件路径使用以下函数，禁止硬编码路径分隔符
import (
    "os"
    "path/filepath"
)

// 配置目录
// Windows: C:\Users\<user>\AppData\Roaming\pairproxy
// Linux:   /home/<user>/.config/pairproxy
// macOS:   /Users/<user>/Library/Application Support/pairproxy
func DefaultConfigDir() string {
    base, _ := os.UserConfigDir()
    return filepath.Join(base, "pairproxy")
}

// SQLite 驱动：使用 github.com/glebarez/sqlite（纯 Go，无 CGO）
// 禁止使用 github.com/mattn/go-sqlite3（需要 gcc/CGO）

// 文件权限：os.Chmod(path, 0600) 调用后忽略错误（Windows 不支持但不影响功能）

// 信号处理
signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
// syscall.SIGTERM 在 Windows 不存在，编译时通过 build tags 处理：
// signals_unix.go:   //go:build !windows
// signals_windows.go://go:build windows
```
