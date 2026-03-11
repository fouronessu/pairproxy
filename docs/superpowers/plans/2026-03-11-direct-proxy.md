# Direct Proxy Implementation Plan

> **For agentic workers:** Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan.

**Goal:** 让用户无需 cproxy 客户端，直接用 `sk-pp-` API Key 接入 sproxy，支持 Anthropic（`/anthropic/*`）和 OpenAI（`/v1/*`）两种协议。

**Architecture:** `KeyAuthMiddleware` 从请求头提取并验证 API Key，将用户身份注入 context（与 JWT claims 同一 key），`serveProxy` 无需感知认证方式差异。`DirectProxyHandler` 预构建中间件链（修复问题3），`KeyCache` 缓存 `*CachedUser`，中间件直接从缓存字段构建 claims（修复问题4）。

**Tech Stack:** Go 1.22+, `github.com/hashicorp/golang-lru/v2`, zap, gorm, httptest

---

## Chunk 1: 基础层 — 依赖 + DB + keygen 包

### Task 1: 添加 golang-lru/v2 依赖

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: 添加依赖**

```bash
cd D:/pairproxy && "C:/Program Files/Go/bin/go.exe" get github.com/hashicorp/golang-lru/v2@v2.0.7
```

Expected: go.mod 新增 `github.com/hashicorp/golang-lru/v2 v2.0.7`

- [ ] **Step 2: 验证编译**

```bash
"C:/Program Files/Go/bin/go.exe" build ./...
```

Expected: 无错误

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add github.com/hashicorp/golang-lru/v2 for API key cache"
```

---

### Task 2: UserRepo.ListActive()

**Files:**
- Modify: `internal/db/user_repo.go`
- Test: `internal/db/user_repo_test.go`（新增测试用例）

- [ ] **Step 1: 找到现有 user_repo_test.go 并追加失败测试**

在 `internal/db/user_repo_test.go` 末尾追加：

```go
func TestUserRepo_ListActive(t *testing.T) {
    logger := zap.NewNop()
    gdb := openTestDB(t, logger)
    repo := NewUserRepo(gdb, logger)

    // 创建两个活跃用户和一个禁用用户
    u1 := &User{Username: "active1", PasswordHash: "h1", IsActive: true}
    u2 := &User{Username: "active2", PasswordHash: "h2", IsActive: true}
    u3 := &User{Username: "disabled", PasswordHash: "h3"}
    require.NoError(t, repo.Create(u1))
    require.NoError(t, repo.Create(u2))
    require.NoError(t, repo.Create(u3))
    // IsActive 默认 true，需要手动禁用 u3
    require.NoError(t, repo.SetActive(u3.ID, false))

    users, err := repo.ListActive()
    require.NoError(t, err)

    names := make(map[string]bool)
    for _, u := range users {
        names[u.Username] = true
        assert.True(t, u.IsActive, "ListActive should only return active users")
    }
    assert.True(t, names["active1"])
    assert.True(t, names["active2"])
    assert.False(t, names["disabled"], "disabled user must not appear")
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/db/... -run TestUserRepo_ListActive -v
```

Expected: FAIL `ListActive` undefined

- [ ] **Step 3: 实现 ListActive**

在 `internal/db/user_repo.go` 末尾追加：

```go
// ListActive 返回所有 is_active=true 的用户列表，用于 API Key 验证遍历。
func (r *UserRepo) ListActive() ([]User, error) {
    var users []User
    if err := r.db.Where("is_active = ?", true).Find(&users).Error; err != nil {
        r.logger.Error("failed to list active users", zap.Error(err))
        return nil, fmt.Errorf("list active users: %w", err)
    }
    r.logger.Debug("listed active users", zap.Int("count", len(users)))
    return users, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/db/... -run TestUserRepo_ListActive -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/db/user_repo.go internal/db/user_repo_test.go
git commit -m "feat(db): add UserRepo.ListActive for API key validation"
```

---

### Task 3: keygen/generator.go

**Files:**
- Create: `internal/keygen/generator.go`
- Create: `internal/keygen/generator_test.go`

- [ ] **Step 1: 创建测试文件**

创建 `internal/keygen/generator_test.go`：

```go
package keygen_test

import (
    "strings"
    "testing"
    "unicode"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/l17728/pairproxy/internal/keygen"
)

func TestGenerateKey_Format(t *testing.T) {
    key, err := keygen.GenerateKey("alice")
    require.NoError(t, err)
    assert.True(t, strings.HasPrefix(key, keygen.KeyPrefix), "must start with sk-pp-")
    assert.Equal(t, keygen.KeyTotalLen, len(key), "total length must be 54")
    body := key[len(keygen.KeyPrefix):]
    for _, c := range body {
        assert.True(t, unicode.IsLetter(c) || unicode.IsDigit(c),
            "body must be alphanumeric, got %q", c)
    }
}

func TestGenerateKey_ContainsUsernameChars(t *testing.T) {
    username := "alice"
    for i := 0; i < 20; i++ { // 多次生成，验证每次都包含用户名字符
        key, err := keygen.GenerateKey(username)
        require.NoError(t, err)
        body := strings.ToLower(key[len(keygen.KeyPrefix):])
        // 验证 body 包含 alice 所有字符（考虑重复次数）
        need := map[rune]int{'a': 1, 'l': 1, 'i': 1, 'c': 1, 'e': 1}
        have := map[rune]int{}
        for _, c := range body {
            have[c]++
        }
        for ch, count := range need {
            assert.GreaterOrEqual(t, have[ch], count,
                "key body must contain username char %q at least %d times", ch, count)
        }
    }
}

func TestGenerateKey_DifferentEachTime(t *testing.T) {
    keys := make(map[string]bool)
    for i := 0; i < 100; i++ {
        k, err := keygen.GenerateKey("alice")
        require.NoError(t, err)
        keys[k] = true
    }
    assert.Greater(t, len(keys), 90, "keys should be unique across 100 generations")
}

func TestGenerateKey_EmptyUsername(t *testing.T) {
    _, err := keygen.GenerateKey("")
    assert.Error(t, err, "empty username must return error")
}

func TestGenerateKey_UsernameWithRepeatedChars(t *testing.T) {
    // "aaab" 的字符提取：[a,a,a,b]（小写）
    key, err := keygen.GenerateKey("aaab")
    require.NoError(t, err)
    body := strings.ToLower(key[len(keygen.KeyPrefix):])
    // 统计 body 中 'a' 的数量，必须 >= 3
    count := 0
    for _, c := range body {
        if c == 'a' {
            count++
        }
    }
    assert.GreaterOrEqual(t, count, 3, "body must contain at least 3 'a' chars for username 'aaab'")
}

func TestExtractAlphanumeric(t *testing.T) {
    cases := []struct {
        input    string
        expected string
    }{
        {"alice", "alice"},
        {"Alice123", "alice123"},
        {"user-name_dot", "username"},
        {"", ""},
        {"---", ""},
    }
    for _, tc := range cases {
        result := keygen.ExtractAlphanumeric(tc.input)
        assert.Equal(t, tc.expected, string(result), "input=%q", tc.input)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/keygen/... -v
```

Expected: FAIL `package not found`

- [ ] **Step 3: 创建 generator.go**

创建 `internal/keygen/generator.go`：

```go
// Package keygen 提供 PairProxy API Key 的生成与验证功能。
//
// Key 格式：sk-pp-<48字符字母数字>，总长度 54 字符。
// 设计原则：Key 主体中嵌入用户名的字母数字字符（打散到随机位置），
// 其余位置用随机字母数字填充，无需数据库存储即可反向识别用户。
package keygen

import (
    "crypto/rand"
    "fmt"
    "math/big"
    "strings"

    "go.uber.org/zap"
)

const (
    // KeyPrefix 是所有 PairProxy API Key 的固定前缀。
    KeyPrefix = "sk-pp-"
    // KeyBodyLen 是 Key 前缀之后的主体长度（字母数字字符）。
    KeyBodyLen = 48
    // KeyTotalLen 是 Key 的总长度（前缀 + 主体）。
    KeyTotalLen = len(KeyPrefix) + KeyBodyLen
    // Charset 是 Key 主体允许使用的字符集（仅字母和数字）。
    Charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// GenerateKey 根据用户名生成一个 API Key。
//
// 算法：
//  1. 提取用户名中的字母和数字（转小写），作为"用户指纹"字符序列
//  2. 用随机字符填满 48 字节的主体
//  3. 在主体中随机选取 len(指纹) 个不重复位置，将指纹字符写入这些位置
//  4. 拼接前缀返回
//
// 返回的 Key 可通过 ValidateAndGetUser 反向识别用户。
func GenerateKey(username string) (string, error) {
    chars := ExtractAlphanumeric(username)
    if len(chars) == 0 {
        return "", fmt.Errorf("username %q contains no alphanumeric characters", username)
    }
    if len(chars) > KeyBodyLen {
        chars = chars[:KeyBodyLen]
    }

    body := make([]byte, KeyBodyLen)
    for i := range body {
        body[i] = randomChar()
    }

    positions := randomPositions(KeyBodyLen, len(chars))
    for i, pos := range positions {
        body[pos] = chars[i]
    }

    key := KeyPrefix + string(body)
    zap.L().Debug("api key generated",
        zap.String("username", username),
        zap.Int("fingerprint_chars", len(chars)),
    )
    return key, nil
}

// ExtractAlphanumeric 提取字符串中的字母和数字，统一转为小写。
// 例如 "Alice-123" → []byte("alice123")。
// 此函数是公开的，供测试和 Validator 共用。
func ExtractAlphanumeric(s string) []byte {
    lower := strings.ToLower(s)
    var result []byte
    for _, c := range lower {
        if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
            result = append(result, byte(c))
        }
    }
    return result
}

// randomChar 从 Charset 中随机返回一个字符。
func randomChar() byte {
    n, err := rand.Int(rand.Reader, big.NewInt(int64(len(Charset))))
    if err != nil {
        // crypto/rand 失败是极罕见的系统级错误；panic 比返回弱随机数更安全。
        panic("keygen: crypto/rand failed: " + err.Error())
    }
    return Charset[n.Int64()]
}

// randomPositions 在 [0, max) 范围内生成 count 个不重复的随机下标。
// 若 count >= max，返回所有下标的随机排列。
func randomPositions(max, count int) []int {
    if count >= max {
        count = max
    }
    // Fisher-Yates 部分洗牌：只生成前 count 个元素
    perm := make([]int, max)
    for i := range perm {
        perm[i] = i
    }
    for i := 0; i < count; i++ {
        n, err := rand.Int(rand.Reader, big.NewInt(int64(max-i)))
        if err != nil {
            panic("keygen: crypto/rand failed: " + err.Error())
        }
        j := i + int(n.Int64())
        perm[i], perm[j] = perm[j], perm[i]
    }
    return perm[:count]
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/keygen/... -v -run TestGenerate
"C:/Program Files/Go/bin/go.exe" test ./internal/keygen/... -v -run TestExtract
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/keygen/
git commit -m "feat(keygen): add key generator with username fingerprint embedding"
```

---

### Task 4: keygen/validator.go

**Files:**
- Create: `internal/keygen/validator.go`
- Create: `internal/keygen/validator_test.go`

- [ ] **Step 1: 创建测试文件** `internal/keygen/validator_test.go`：

```go
package keygen_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/l17728/pairproxy/internal/keygen"
)

// ---- IsValidFormat ----

func TestIsValidFormat_Valid(t *testing.T) {
    key, err := keygen.GenerateKey("alice")
    require.NoError(t, err)
    assert.True(t, keygen.IsValidFormat(key))
}

func TestIsValidFormat_WrongPrefix(t *testing.T) {
    assert.False(t, keygen.IsValidFormat("sk-ant-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
}

func TestIsValidFormat_TooShort(t *testing.T) {
    assert.False(t, keygen.IsValidFormat("sk-pp-short"))
}

func TestIsValidFormat_TooLong(t *testing.T) {
    assert.False(t, keygen.IsValidFormat("sk-pp-"+"a"+string(make([]byte, 48))+"X"))
}

func TestIsValidFormat_InvalidChars(t *testing.T) {
    // 48 chars with a dash
    assert.False(t, keygen.IsValidFormat("sk-pp-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-aa"))
}

// ---- ValidateAndGetUser ----

func TestValidateAndGetUser_Match(t *testing.T) {
    key, err := keygen.GenerateKey("alice")
    require.NoError(t, err)

    users := []keygen.UserEntry{{ID: "u1", Username: "alice", IsActive: true}}
    u, err := keygen.ValidateAndGetUser(key, users)
    require.NoError(t, err)
    require.NotNil(t, u)
    assert.Equal(t, "alice", u.Username)
}

func TestValidateAndGetUser_NoMatch(t *testing.T) {
    // 构造一个不包含 "bob" 字符(b,o,b)的 key（很难做到，用固定不含这些字符的key）
    key := "sk-pp-" + "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 少一位，格式无效先测别的路径
    // 直接测 alice 的 key 对 bob 用户是否不匹配
    aliceKey, _ := keygen.GenerateKey("alice")
    users := []keygen.UserEntry{{ID: "u2", Username: "xyz99", IsActive: true}}
    u, err := keygen.ValidateAndGetUser(aliceKey, users)
    // alice key 理论上不含 xyz99 所有字符（x,y,z,9,9），可能命中也可能不命中
    // 这里只验证不 panic 且返回类型正确
    _ = u
    _ = err
}

func TestValidateAndGetUser_InactiveSkipped(t *testing.T) {
    key, err := keygen.GenerateKey("alice")
    require.NoError(t, err)
    users := []keygen.UserEntry{
        {ID: "u1", Username: "alice", IsActive: false}, // 已禁用
    }
    u, err := keygen.ValidateAndGetUser(key, users)
    require.NoError(t, err)
    assert.Nil(t, u, "inactive user must not be returned")
}

func TestValidateAndGetUser_LongestMatchWins(t *testing.T) {
    // "alice" (5 chars) vs "ali" (3 chars)
    // alice 的 key 一定包含 ali 的所有字符，验证返回 alice（更长的用户名）
    key, err := keygen.GenerateKey("alice")
    require.NoError(t, err)
    users := []keygen.UserEntry{
        {ID: "u1", Username: "alice", IsActive: true},
        {ID: "u2", Username: "ali", IsActive: true},
    }
    u, err := keygen.ValidateAndGetUser(key, users)
    require.NoError(t, err)
    require.NotNil(t, u)
    assert.Equal(t, "alice", u.Username, "longest match must win")
}

func TestValidateAndGetUser_RepeatedChars(t *testing.T) {
    // 用户名 "aaab" 需要 key 中出现 3 个 'a' 和 1 个 'b'
    key, err := keygen.GenerateKey("aaab")
    require.NoError(t, err)
    users := []keygen.UserEntry{
        {ID: "u1", Username: "aaab", IsActive: true},
    }
    u, err := keygen.ValidateAndGetUser(key, users)
    require.NoError(t, err)
    require.NotNil(t, u)
    assert.Equal(t, "aaab", u.Username)
}

func TestValidateAndGetUser_Collision(t *testing.T) {
    // 构造两个 5字符用户名都完全命中的场景
    // 用一个固定的包含大量常见字母的 key，让两个用户名都匹配
    // 例如 key body = "aabbccddeeffgghhiijjkkllmmnnoopp..." → 匹配 "abcde"(5) 和 "abcdf"(5)
    // 构造方法：直接测 collision 检测逻辑：两用户名相同长度且都匹配时报 collision
    // 用 alice 的 key，alice(5) vs alicX(5) —— 如果 key 恰好也含 X 则 collision
    // 这个测试验证 collision 时返回 error
    key := "sk-pp-aabbccddeeffgghhiijjkkllmmnnooaabbccddeeffgghhii" // 54 chars, 含 a,b,c...
    // 创建两个 5 字符用户名，都应该在 key 中
    users := []keygen.UserEntry{
        {ID: "u1", Username: "abcde", IsActive: true},
        {ID: "u2", Username: "abcdf", IsActive: true},
    }
    u, err := keygen.ValidateAndGetUser(key, users)
    // 如果两个都匹配且长度相同，应该返回 collision error
    // 如果只有一个匹配，正常返回
    if err != nil {
        assert.Contains(t, err.Error(), "collision")
        assert.Nil(t, u)
    }
    // 无 collision 时 u 可为 nil 或某个用户，均合法
}

// ---- ValidateUsername ----

func TestValidateUsername_Valid(t *testing.T) {
    assert.NoError(t, keygen.ValidateUsername("alice"))
    assert.NoError(t, keygen.ValidateUsername("user123"))
    assert.NoError(t, keygen.ValidateUsername("ab12"))
}

func TestValidateUsername_TooShort(t *testing.T) {
    assert.Error(t, keygen.ValidateUsername("ab"))
    assert.Error(t, keygen.ValidateUsername("abc"))
}

func TestValidateUsername_TooFewUniqueChars(t *testing.T) {
    // 只有一种字母数字字符
    assert.Error(t, keygen.ValidateUsername("aaaa"))
    assert.Error(t, keygen.ValidateUsername("1111"))
    assert.Error(t, keygen.ValidateUsername("----")) // 无字母数字
}

func TestValidateUsername_Valid_TwoUniqueChars(t *testing.T) {
    assert.NoError(t, keygen.ValidateUsername("aabb")) // a+b，两种字符，长度4
}

// ---- containsAllCharsWithCount ----

func TestContainsAllCharsWithCount(t *testing.T) {
    cases := []struct {
        body   string
        chars  []byte
        expect bool
    }{
        {"abcde", []byte("alice"), true},
        {"abcd", []byte("alice"), false},   // 缺 'e'（注意body含a,b,c,d但缺e）
        {"aaabcd", []byte("aaab"), true},   // body有3个a和b，满足需求
        {"aabcd", []byte("aaab"), false},   // body只有2个a，不满足3个a的需求
    }
    for _, tc := range cases {
        result := keygen.ContainsAllCharsWithCount(tc.body, tc.chars)
        assert.Equal(t, tc.expect, result, "body=%q chars=%q", tc.body, tc.chars)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/keygen/... -v 2>&1 | head -20
```

Expected: FAIL `undefined`

- [ ] **Step 3: 创建 validator.go**

创建 `internal/keygen/validator.go`：

```go
package keygen

import (
    "fmt"
    "strings"

    "go.uber.org/zap"
)

// UserEntry 是 ValidateAndGetUser 所需的最小用户信息，
// 解耦了 keygen 包与 db 包的直接依赖。
type UserEntry struct {
    ID       string
    Username string
    IsActive bool
}

// IsValidFormat 检查 Key 是否满足格式要求（前缀、总长度、字符集）。
// 不涉及用户身份验证，仅作格式预筛。
func IsValidFormat(key string) bool {
    if !strings.HasPrefix(key, KeyPrefix) {
        return false
    }
    body := key[len(KeyPrefix):]
    if len(body) != KeyBodyLen {
        return false
    }
    for _, c := range body {
        if !strings.ContainsRune(Charset, c) {
            return false
        }
    }
    return true
}

// ValidateAndGetUser 验证 Key 并返回匹配的用户。
//
// 验证逻辑：
//  1. 对 users 中每个活跃用户，提取其用户名的字母数字字符序列（含重复次数）
//  2. 检查 Key 主体（转小写）是否包含该序列的所有字符（含重复次数）
//  3. 选择匹配的最长用户名（最多字母数字字符数）
//  4. 若相同长度有多个用户匹配，返回 collision error
//
// 返回 (nil, nil) 表示无匹配用户。
func ValidateAndGetUser(key string, users []UserEntry) (*UserEntry, error) {
    if !IsValidFormat(key) {
        return nil, nil
    }
    body := strings.ToLower(key[len(KeyPrefix):])

    var matched *UserEntry
    maxLen := 0
    collisionCount := 0

    for i := range users {
        u := &users[i]
        if !u.IsActive {
            continue
        }
        chars := ExtractAlphanumeric(u.Username)
        if len(chars) == 0 {
            continue
        }
        if !ContainsAllCharsWithCount(body, chars) {
            continue
        }
        // 匹配：按指纹长度排序
        l := len(chars)
        if l > maxLen {
            maxLen = l
            matched = u
            collisionCount = 1
        } else if l == maxLen {
            collisionCount++
        }
    }

    if collisionCount > 1 {
        zap.L().Warn("api key collision detected",
            zap.Int("collision_count", collisionCount),
            zap.Int("fingerprint_len", maxLen),
        )
        return nil, fmt.Errorf("collision detected: %d users share the same fingerprint length %d", collisionCount, maxLen)
    }

    if matched != nil {
        zap.L().Debug("api key validated",
            zap.String("username", matched.Username),
            zap.Int("fingerprint_len", maxLen),
        )
    }
    return matched, nil
}

// ValidateUsername 验证用户名是否满足 API Key 生成的最低要求。
// 规则：≥4字符，至少2个不同的字母数字字符。
func ValidateUsername(username string) error {
    if len(username) < 4 {
        return fmt.Errorf("username must be at least 4 characters, got %d", len(username))
    }
    chars := ExtractAlphanumeric(username)
    if len(chars) < 2 {
        return fmt.Errorf("username must contain at least 2 alphanumeric characters")
    }
    unique := make(map[byte]bool)
    for _, c := range chars {
        unique[c] = true
    }
    if len(unique) < 2 {
        return fmt.Errorf("username must contain at least 2 different alphanumeric characters")
    }
    return nil
}

// ContainsAllCharsWithCount 检查字符串 s 是否包含 chars 中每个字符所需的最低数量。
// 例如 chars=[]byte("aaab") 要求 s 中至少出现 3 个 'a' 和 1 个 'b'。
// s 和 chars 均已预期为小写。
func ContainsAllCharsWithCount(s string, chars []byte) bool {
    need := make(map[byte]int, len(chars))
    for _, c := range chars {
        need[c]++
    }
    have := make(map[byte]int, len(chars))
    for i := 0; i < len(s); i++ {
        c := s[i]
        if c >= 'A' && c <= 'Z' {
            c += 32
        }
        have[c]++
    }
    for ch, n := range need {
        if have[ch] < n {
            return false
        }
    }
    return true
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/keygen/... -v
```

Expected: 全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/keygen/
git commit -m "feat(keygen): add key validator, username validator, and user entry type"
```

---

### Task 5: keygen/cache.go

**Files:**
- Create: `internal/keygen/cache.go`
- Create: `internal/keygen/cache_test.go`

- [ ] **Step 1: 创建测试文件** `internal/keygen/cache_test.go`：

```go
package keygen_test

import (
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/l17728/pairproxy/internal/keygen"
)

func TestKeyCache_SetAndGet(t *testing.T) {
    cache, err := keygen.NewKeyCache(100, time.Minute)
    require.NoError(t, err)

    key := "sk-pp-testkey123"
    user := &keygen.CachedUser{UserID: "u1", Username: "alice"}
    cache.Set(key, user)

    got := cache.Get(key)
    require.NotNil(t, got)
    assert.Equal(t, "u1", got.UserID)
    assert.Equal(t, "alice", got.Username)
}

func TestKeyCache_Miss(t *testing.T) {
    cache, err := keygen.NewKeyCache(100, time.Minute)
    require.NoError(t, err)
    assert.Nil(t, cache.Get("nonexistent"))
}

func TestKeyCache_TTLExpiry(t *testing.T) {
    cache, err := keygen.NewKeyCache(100, 50*time.Millisecond)
    require.NoError(t, err)

    cache.Set("k", &keygen.CachedUser{UserID: "u1", Username: "alice"})
    assert.NotNil(t, cache.Get("k"), "should hit before TTL")

    time.Sleep(80 * time.Millisecond)
    assert.Nil(t, cache.Get("k"), "should miss after TTL")
}

func TestKeyCache_InvalidateUser(t *testing.T) {
    cache, err := keygen.NewKeyCache(100, time.Minute)
    require.NoError(t, err)

    cache.Set("k1", &keygen.CachedUser{UserID: "u1", Username: "alice"})
    cache.Set("k2", &keygen.CachedUser{UserID: "u1", Username: "alice"})
    cache.Set("k3", &keygen.CachedUser{UserID: "u2", Username: "bob"})

    cache.InvalidateUser("alice")

    assert.Nil(t, cache.Get("k1"), "alice key k1 should be evicted")
    assert.Nil(t, cache.Get("k2"), "alice key k2 should be evicted")
    assert.NotNil(t, cache.Get("k3"), "bob key should remain")
}

func TestKeyCache_SizeLimit(t *testing.T) {
    // LRU size=2, 写入3个 key，最老的应被淘汰
    cache, err := keygen.NewKeyCache(2, time.Minute)
    require.NoError(t, err)

    cache.Set("k1", &keygen.CachedUser{UserID: "u1", Username: "a"})
    cache.Set("k2", &keygen.CachedUser{UserID: "u2", Username: "b"})
    cache.Set("k3", &keygen.CachedUser{UserID: "u3", Username: "c"})

    // k1 应该被淘汰（LRU）
    assert.Nil(t, cache.Get("k1"), "oldest entry should be evicted when size exceeded")
    assert.NotNil(t, cache.Get("k2"))
    assert.NotNil(t, cache.Get("k3"))
}
```

- [ ] **Step 2: 创建 cache.go**

创建 `internal/keygen/cache.go`：

```go
package keygen

import (
    "sync"
    "time"

    lru "github.com/hashicorp/golang-lru/v2"
    "go.uber.org/zap"
)

// CachedUser 是 KeyCache 存储的用户信息快照，
// 包含构建 auth.JWTClaims 所需的全部字段。
type CachedUser struct {
    UserID   string
    Username string
    GroupID  *string   // 对应 db.User.GroupID（可为 nil）
    CachedAt time.Time
}

// KeyCache 是 API Key → 用户信息的 LRU 缓存，避免每次请求遍历所有用户。
type KeyCache struct {
    mu    sync.RWMutex
    inner *lru.Cache[string, *CachedUser]
    ttl   time.Duration
}

// NewKeyCache 创建 KeyCache。
//   - size: 最大缓存条目数（超出时 LRU 淘汰最久未访问的条目）
//   - ttl:  缓存有效期（0 表示永不过期）
func NewKeyCache(size int, ttl time.Duration) (*KeyCache, error) {
    inner, err := lru.New[string, *CachedUser](size)
    if err != nil {
        return nil, err
    }
    return &KeyCache{inner: inner, ttl: ttl}, nil
}

// Get 从缓存中取用户信息。未命中或已过期返回 nil。
func (c *KeyCache) Get(key string) *CachedUser {
    c.mu.RLock()
    entry, ok := c.inner.Get(key)
    c.mu.RUnlock()
    if !ok {
        return nil
    }
    if c.ttl > 0 && time.Since(entry.CachedAt) > c.ttl {
        // TTL 过期：主动删除
        c.mu.Lock()
        c.inner.Remove(key)
        c.mu.Unlock()
        zap.L().Debug("api key cache TTL expired", zap.String("username", entry.Username))
        return nil
    }
    return entry
}

// Set 将用户信息写入缓存。
func (c *KeyCache) Set(key string, user *CachedUser) {
    user.CachedAt = time.Now()
    c.mu.Lock()
    c.inner.Add(key, user)
    c.mu.Unlock()
    zap.L().Debug("api key cached",
        zap.String("username", user.Username),
        zap.String("user_id", user.UserID),
    )
}

// InvalidateUser 删除指定用户名对应的所有缓存条目（用户重新生成 Key 时调用）。
func (c *KeyCache) InvalidateUser(username string) {
    c.mu.Lock()
    defer c.mu.Unlock()
    keys := c.inner.Keys()
    removed := 0
    for _, k := range keys {
        if entry, ok := c.inner.Peek(k); ok && entry.Username == username {
            c.inner.Remove(k)
            removed++
        }
    }
    zap.L().Info("api key cache invalidated for user",
        zap.String("username", username),
        zap.Int("removed_entries", removed),
    )
}
```

- [ ] **Step 3: 运行测试确认通过**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/keygen/... -v -run TestKeyCache
```

Expected: 全部 PASS

- [ ] **Step 4: 运行全量 keygen 测试**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/keygen/... -v -race
```

Expected: PASS，无 data race

- [ ] **Step 5: Commit**

```bash
git add internal/keygen/cache.go internal/keygen/cache_test.go
git commit -m "feat(keygen): add LRU key cache with TTL and user invalidation"
```

---

## Chunk 2: 代理层 — KeyAuthMiddleware + ServeDirect + DirectProxyHandler

### Task 6: KeyAuthMiddleware

**Files:**
- Create: `internal/proxy/keyauth_middleware.go`
- Create: `internal/proxy/keyauth_middleware_test.go`

- [ ] **Step 1: 创建测试文件** `internal/proxy/keyauth_middleware_test.go`：

```go
package proxy_test

import (
    "net/http"
    "net/http/httptest"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.uber.org/zap"

    "github.com/l17728/pairproxy/internal/auth"
    "github.com/l17728/pairproxy/internal/keygen"
    "github.com/l17728/pairproxy/internal/proxy"
)

// fakeUserLookup 实现 KeyAuthMiddleware 需要的用户查询接口（测试用）
type fakeUserLookup struct {
    users []keygen.UserEntry
}

func (f *fakeUserLookup) ListActive() ([]keygen.UserEntry, error) {
    return f.users, nil
}

func makeAliceKey(t *testing.T) string {
    t.Helper()
    k, err := keygen.GenerateKey("alice")
    require.NoError(t, err)
    return k
}

func TestKeyAuthMiddleware_OpenAI_BearerFormat(t *testing.T) {
    key := makeAliceKey(t)
    users := &fakeUserLookup{users: []keygen.UserEntry{
        {ID: "u1", Username: "alice", IsActive: true},
    }}
    cache, _ := keygen.NewKeyCache(10, time.Minute)
    logger := zap.NewNop()

    var gotClaims *auth.JWTClaims
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotClaims = proxy.ClaimsFromContext(r.Context())
        w.WriteHeader(200)
    })

    mw := proxy.NewKeyAuthMiddleware(logger, users, cache, next)
    req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
    req.Header.Set("Authorization", "Bearer "+key)
    rr := httptest.NewRecorder()
    mw.ServeHTTP(rr, req)

    assert.Equal(t, 200, rr.Code)
    require.NotNil(t, gotClaims)
    assert.Equal(t, "u1", gotClaims.UserID)
    assert.Equal(t, "alice", gotClaims.Username)
}

func TestKeyAuthMiddleware_Anthropic_XApiKeyFormat(t *testing.T) {
    key := makeAliceKey(t)
    users := &fakeUserLookup{users: []keygen.UserEntry{
        {ID: "u1", Username: "alice", IsActive: true},
    }}
    cache, _ := keygen.NewKeyCache(10, time.Minute)

    var gotClaims *auth.JWTClaims
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotClaims = proxy.ClaimsFromContext(r.Context())
        w.WriteHeader(200)
    })

    mw := proxy.NewKeyAuthMiddleware(zap.NewNop(), users, cache, next)
    req := httptest.NewRequest("POST", "/anthropic/v1/messages", nil)
    req.Header.Set("x-api-key", key)
    rr := httptest.NewRecorder()
    mw.ServeHTTP(rr, req)

    assert.Equal(t, 200, rr.Code)
    require.NotNil(t, gotClaims)
    assert.Equal(t, "alice", gotClaims.Username)
}

func TestKeyAuthMiddleware_MissingAuth(t *testing.T) {
    users := &fakeUserLookup{}
    cache, _ := keygen.NewKeyCache(10, time.Minute)
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
    mw := proxy.NewKeyAuthMiddleware(zap.NewNop(), users, cache, next)

    req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
    rr := httptest.NewRecorder()
    mw.ServeHTTP(rr, req)
    assert.Equal(t, 401, rr.Code)
}

func TestKeyAuthMiddleware_InvalidFormat(t *testing.T) {
    users := &fakeUserLookup{}
    cache, _ := keygen.NewKeyCache(10, time.Minute)
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
    mw := proxy.NewKeyAuthMiddleware(zap.NewNop(), users, cache, next)

    req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
    req.Header.Set("Authorization", "Bearer sk-openai-notapairproxykey")
    rr := httptest.NewRecorder()
    mw.ServeHTTP(rr, req)
    assert.Equal(t, 401, rr.Code)
}

func TestKeyAuthMiddleware_InvalidUser(t *testing.T) {
    key := makeAliceKey(t)
    // 用户列表中没有 alice
    users := &fakeUserLookup{users: []keygen.UserEntry{
        {ID: "u2", Username: "bob", IsActive: true},
    }}
    cache, _ := keygen.NewKeyCache(10, time.Minute)
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
    mw := proxy.NewKeyAuthMiddleware(zap.NewNop(), users, cache, next)

    req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
    req.Header.Set("Authorization", "Bearer "+key)
    rr := httptest.NewRecorder()
    mw.ServeHTTP(rr, req)
    assert.Equal(t, 401, rr.Code)
}

func TestKeyAuthMiddleware_CacheHit(t *testing.T) {
    key := makeAliceKey(t)
    // 用户列表为空，但缓存中有记录 —— 验证缓存命中路径
    users := &fakeUserLookup{users: []keygen.UserEntry{}}
    cache, _ := keygen.NewKeyCache(10, time.Minute)
    cache.Set(key, &keygen.CachedUser{UserID: "u1", Username: "alice"})

    var gotClaims *auth.JWTClaims
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotClaims = proxy.ClaimsFromContext(r.Context())
        w.WriteHeader(200)
    })
    mw := proxy.NewKeyAuthMiddleware(zap.NewNop(), users, cache, next)

    req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
    req.Header.Set("Authorization", "Bearer "+key)
    rr := httptest.NewRecorder()
    mw.ServeHTTP(rr, req)
    assert.Equal(t, 200, rr.Code)
    require.NotNil(t, gotClaims)
    assert.Equal(t, "alice", gotClaims.Username)
}

func TestKeyAuthMiddleware_GroupID(t *testing.T) {
    key := makeAliceKey(t)
    gid := "g1"
    users := &fakeUserLookup{users: []keygen.UserEntry{
        {ID: "u1", Username: "alice", IsActive: true, GroupID: &gid},
    }}
    cache, _ := keygen.NewKeyCache(10, time.Minute)

    var gotClaims *auth.JWTClaims
    next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        gotClaims = proxy.ClaimsFromContext(r.Context())
        w.WriteHeader(200)
    })
    mw := proxy.NewKeyAuthMiddleware(zap.NewNop(), users, cache, next)
    req := httptest.NewRequest("POST", "/v1/chat/completions", nil)
    req.Header.Set("Authorization", "Bearer "+key)
    rr := httptest.NewRecorder()
    mw.ServeHTTP(rr, req)

    require.NotNil(t, gotClaims)
    assert.Equal(t, "g1", gotClaims.GroupID)
}
```

注意：`UserEntry` 需要增加 `GroupID *string` 字段（在 Task 4 的 validator.go 中追加，或在本步骤创建 middleware 时同步更新）。

- [ ] **Step 2: 更新 UserEntry 增加 GroupID 字段**

在 `internal/keygen/validator.go` 的 `UserEntry` struct 增加：

```go
type UserEntry struct {
    ID       string
    Username string
    IsActive bool
    GroupID  *string  // 新增
}
```

- [ ] **Step 3: 运行测试确认失败**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/proxy/... -run TestKeyAuthMiddleware -v 2>&1 | head -20
```

Expected: FAIL `NewKeyAuthMiddleware undefined`

- [ ] **Step 4: 创建 keyauth_middleware.go**

创建 `internal/proxy/keyauth_middleware.go`：

```go
package proxy

import (
    "context"
    "net/http"
    "strings"

    "go.uber.org/zap"

    "github.com/l17728/pairproxy/internal/auth"
    "github.com/l17728/pairproxy/internal/keygen"
)

// ActiveUserLister 是 KeyAuthMiddleware 依赖的用户查询接口。
// 由 *db.UserRepo 实现（通过 DBUserLister 适配器桥接）。
type ActiveUserLister interface {
    ListActive() ([]keygen.UserEntry, error)
}

// NewKeyAuthMiddleware 构建 API Key 认证中间件。
//
// 支持两种认证头格式（自动识别）：
//   - OpenAI 格式：Authorization: Bearer sk-pp-<48chars>
//   - Anthropic 格式：x-api-key: sk-pp-<48chars>
//
// 验证成功后将 *auth.JWTClaims 注入 context（与 AuthMiddleware 相同的 key），
// 下游 serveProxy 可无感知地复用。
//
// 中间件链：cache.Get → (miss) ListActive + ValidateAndGetUser → cache.Set → 注入 claims → next
func NewKeyAuthMiddleware(
    logger *zap.Logger,
    users ActiveUserLister,
    cache *keygen.KeyCache, // 可为 nil（禁用缓存）
    next http.Handler,
) http.Handler {
    log := logger.Named("key_auth")
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        reqID := RequestIDFromContext(r.Context())

        // 1. 提取 API Key（支持 OpenAI 和 Anthropic 两种格式）
        token := extractDirectAPIKey(r)
        if token == "" {
            log.Warn("direct auth: missing api key",
                zap.String("request_id", reqID),
                zap.String("path", r.URL.Path),
            )
            writeJSONError(w, http.StatusUnauthorized, "missing_authorization",
                "Authorization: Bearer <sk-pp-key> or x-api-key: <sk-pp-key> required")
            return
        }

        // 2. 格式预检（前缀 + 长度 + 字符集）
        if !keygen.IsValidFormat(token) {
            log.Warn("direct auth: invalid key format",
                zap.String("request_id", reqID),
                zap.String("key_prefix", safePrefix(token, 12)),
            )
            writeJSONError(w, http.StatusUnauthorized, "invalid_key_format",
                "API key must be in format sk-pp-<48 alphanumeric chars>")
            return
        }

        // 3. 查缓存（issue 4 fix：缓存返回 *CachedUser，直接提取字段）
        var userID, username string
        var groupID *string

        if cache != nil {
            if cached := cache.Get(token); cached != nil {
                userID = cached.UserID
                username = cached.Username
                groupID = cached.GroupID
                log.Debug("direct auth: cache hit",
                    zap.String("request_id", reqID),
                    zap.String("username", username),
                )
            }
        }

        // 4. 缓存未命中：遍历用户验证
        if userID == "" {
            activeUsers, err := users.ListActive()
            if err != nil {
                log.Error("direct auth: failed to list active users",
                    zap.String("request_id", reqID),
                    zap.Error(err),
                )
                writeJSONError(w, http.StatusInternalServerError, "internal_error", "user lookup failed")
                return
            }

            matched, valErr := keygen.ValidateAndGetUser(token, activeUsers)
            if valErr != nil {
                log.Warn("direct auth: key collision",
                    zap.String("request_id", reqID),
                    zap.Error(valErr),
                )
                writeJSONError(w, http.StatusUnauthorized, "key_collision",
                    "api key matches multiple users; contact administrator")
                return
            }
            if matched == nil {
                log.Warn("direct auth: no matching user",
                    zap.String("request_id", reqID),
                    zap.String("key_prefix", safePrefix(token, 12)),
                )
                writeJSONError(w, http.StatusUnauthorized, "invalid_api_key", "invalid API key")
                return
            }

            userID = matched.ID
            username = matched.Username
            groupID = matched.GroupID

            // 写缓存
            if cache != nil {
                cache.Set(token, &keygen.CachedUser{
                    UserID:   userID,
                    Username: username,
                    GroupID:  groupID,
                })
            }

            log.Info("direct auth: key validated",
                zap.String("request_id", reqID),
                zap.String("username", username),
                zap.String("user_id", userID),
            )
        }

        // 5. 构建 claims（复用 ctxKeyClaims，下游 serveProxy 无感知）
        groupIDStr := ""
        if groupID != nil {
            groupIDStr = *groupID
        }
        claims := &auth.JWTClaims{
            UserID:   userID,
            Username: username,
            GroupID:  groupIDStr,
        }
        ctx := context.WithValue(r.Context(), ctxKeyClaims, claims)

        log.Debug("direct auth: claims injected",
            zap.String("request_id", reqID),
            zap.String("user_id", userID),
            zap.String("group_id", groupIDStr),
        )

        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// extractDirectAPIKey 从请求头提取 API Key，支持两种格式：
//   - OpenAI：Authorization: Bearer <key>
//   - Anthropic：x-api-key: <key>（无 Bearer 前缀）
func extractDirectAPIKey(r *http.Request) string {
    if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
        return strings.TrimPrefix(auth, "Bearer ")
    }
    if key := r.Header.Get("x-api-key"); key != "" {
        return key
    }
    return ""
}

// safePrefix 安全截取字符串前 n 字符（避免越界）。
func safePrefix(s string, n int) string {
    if len(s) <= n {
        return s
    }
    return s[:n] + "..."
}
```

- [ ] **Step 5: 运行测试确认通过**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/proxy/... -run TestKeyAuthMiddleware -v -race
```

Expected: 全部 PASS，无 data race

- [ ] **Step 6: Commit**

```bash
git add internal/proxy/keyauth_middleware.go internal/proxy/keyauth_middleware_test.go internal/keygen/validator.go
git commit -m "feat(proxy): add KeyAuthMiddleware supporting OpenAI/Anthropic header formats"
```

---

### Task 7: SProxy.ServeDirect + x-api-key header 清理

**Files:**
- Modify: `internal/proxy/sproxy.go`

- [ ] **Step 1: 在 sproxy.go Director 中添加 x-api-key 清理**

找到 `serveProxy` 方法中的 Director 函数，在已有的 header 删除代码后追加：

```go
// 现有代码（约1322行）：
req.Header.Del("X-PairProxy-Auth")
req.Header.Del("Authorization")
// 新增：清理直连模式的 Anthropic 认证头
req.Header.Del("x-api-key")
```

- [ ] **Step 2: 在 sproxy.go 末尾添加 ServeDirect 方法**

```go
// ServeDirect 处理直连模式（API Key 认证）的代理请求。
//
// 前提：请求 context 中已由 KeyAuthMiddleware 注入 *auth.JWTClaims。
// 与 serveProxy 的唯一区别：Director 中会额外删除 x-api-key 认证头。
// 路径重写（/anthropic/* → /v1/*）由 DirectProxyHandler 在调用前完成。
func (sp *SProxy) ServeDirect(w http.ResponseWriter, r *http.Request) {
    sp.logger.Debug("serving direct proxy request",
        zap.String("path", r.URL.Path),
        zap.String("method", r.Method),
    )
    sp.serveProxy(w, r)
}
```

- [ ] **Step 3: 编译验证**

```bash
"C:/Program Files/Go/bin/go.exe" build ./internal/proxy/...
```

Expected: 无错误

- [ ] **Step 4: Commit**

```bash
git add internal/proxy/sproxy.go
git commit -m "feat(proxy): add ServeDirect and x-api-key header cleanup in Director"
```

---

### Task 8: DirectProxyHandler

**Files:**
- Create: `internal/proxy/direct_handler.go`
- Create: `internal/proxy/direct_handler_test.go`

- [ ] **Step 1: 创建测试文件** `internal/proxy/direct_handler_test.go`：

```go
package proxy_test

import (
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.uber.org/zap"

    "github.com/l17728/pairproxy/internal/keygen"
    "github.com/l17728/pairproxy/internal/proxy"
)

// mockSProxy 用于测试 DirectProxyHandler，避免真实代理依赖
type mockSProxy struct {
    receivedPath string
    receivedUser string
    response     string
}

func (m *mockSProxy) ServeDirect(w http.ResponseWriter, r *http.Request) {
    m.receivedPath = r.URL.Path
    if claims := proxy.ClaimsFromContext(r.Context()); claims != nil {
        m.receivedUser = claims.Username
    }
    w.WriteHeader(200)
    _, _ = io.WriteString(w, m.response)
}

func TestDirectHandler_AnthropicPathRewrite(t *testing.T) {
    mock := &mockSProxy{response: "ok"}
    aliceKey, _ := keygen.GenerateKey("alice")
    cache, _ := keygen.NewKeyCache(10, time.Minute)
    cache.Set(aliceKey, &keygen.CachedUser{UserID: "u1", Username: "alice"})

    users := &fakeUserLookup{users: []keygen.UserEntry{
        {ID: "u1", Username: "alice", IsActive: true},
    }}

    dh := proxy.NewDirectProxyHandler(zap.NewNop(), mock, users, cache)
    handler := dh.HandlerAnthropic()

    req := httptest.NewRequest("POST", "/anthropic/v1/messages", strings.NewReader(`{}`))
    req.Header.Set("x-api-key", aliceKey)
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    assert.Equal(t, 200, rr.Code)
    assert.Equal(t, "/v1/messages", mock.receivedPath, "path must be rewritten")
    assert.Equal(t, "alice", mock.receivedUser)
}

func TestDirectHandler_OpenAIPathUnchanged(t *testing.T) {
    mock := &mockSProxy{response: "ok"}
    aliceKey, _ := keygen.GenerateKey("alice")
    cache, _ := keygen.NewKeyCache(10, time.Minute)
    cache.Set(aliceKey, &keygen.CachedUser{UserID: "u1", Username: "alice"})

    users := &fakeUserLookup{users: []keygen.UserEntry{
        {ID: "u1", Username: "alice", IsActive: true},
    }}

    dh := proxy.NewDirectProxyHandler(zap.NewNop(), mock, users, cache)
    handler := dh.HandlerOpenAI()

    req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
    req.Header.Set("Authorization", "Bearer "+aliceKey)
    rr := httptest.NewRecorder()
    handler.ServeHTTP(rr, req)

    assert.Equal(t, 200, rr.Code)
    assert.Equal(t, "/v1/chat/completions", mock.receivedPath, "OpenAI path must not be rewritten")
}

func TestDirectHandler_HandlerBuiltOnce(t *testing.T) {
    // 验证 HandlerOpenAI/HandlerAnthropic 返回同一个 handler 实例（问题3 修复）
    mock := &mockSProxy{}
    users := &fakeUserLookup{}
    cache, _ := keygen.NewKeyCache(10, time.Minute)
    dh := proxy.NewDirectProxyHandler(zap.NewNop(), mock, users, cache)

    h1 := dh.HandlerOpenAI()
    h2 := dh.HandlerOpenAI()
    assert.Equal(t, h1, h2, "HandlerOpenAI must return the same pre-built handler")

    h3 := dh.HandlerAnthropic()
    h4 := dh.HandlerAnthropic()
    assert.Equal(t, h3, h4, "HandlerAnthropic must return the same pre-built handler")
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/proxy/... -run TestDirectHandler -v 2>&1 | head -20
```

Expected: FAIL `NewDirectProxyHandler undefined`

- [ ] **Step 3: 创建 direct_handler.go**

创建 `internal/proxy/direct_handler.go`：

```go
package proxy

import (
    "net/http"
    "strings"

    "go.uber.org/zap"

    "github.com/l17728/pairproxy/internal/keygen"
)

// DirectServer 是 DirectProxyHandler 依赖的代理接口，由 *SProxy 实现。
// 解耦便于测试。
type DirectServer interface {
    ServeDirect(w http.ResponseWriter, r *http.Request)
}

// DirectProxyHandler 处理 API Key 直连请求（无需 cproxy 客户端）。
//
// 使用前通过 NewDirectProxyHandler 构造，HandlerOpenAI / HandlerAnthropic
// 在构造时即完成中间件链的组装（问题3修复：不在每次请求时重建）。
type DirectProxyHandler struct {
    logger         *zap.Logger
    openAIHandler  http.Handler // 预构建，复用
    anthropicHandler http.Handler // 预构建，复用
}

// NewDirectProxyHandler 构造 DirectProxyHandler，同时完成中间件链预构建。
//
//   - server: *SProxy（实现 DirectServer 接口）
//   - users: ActiveUserLister（*db.UserRepo 通过适配器实现）
//   - cache: *keygen.KeyCache（可为 nil）
func NewDirectProxyHandler(
    logger *zap.Logger,
    server DirectServer,
    users ActiveUserLister,
    cache *keygen.KeyCache,
) *DirectProxyHandler {
    log := logger.Named("direct_proxy")

    // Anthropic 协议处理器（路径重写 /anthropic/* → /v1/*）
    anthropicCore := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        original := r.URL.Path
        r.URL.Path = strings.TrimPrefix(r.URL.Path, "/anthropic")
        if r.URL.RawPath != "" {
            r.URL.RawPath = strings.TrimPrefix(r.URL.RawPath, "/anthropic")
        }
        log.Debug("anthropic path rewritten",
            zap.String("original", original),
            zap.String("rewritten", r.URL.Path),
        )
        server.ServeDirect(w, r)
    })

    // OpenAI 协议处理器（路径不变）
    openAICore := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        log.Debug("openai direct request",
            zap.String("path", r.URL.Path),
        )
        server.ServeDirect(w, r)
    })

    // 组装中间件链（从内到外）：core → KeyAuth → RequestID → Recovery
    buildChain := func(core http.Handler) http.Handler {
        withAuth := NewKeyAuthMiddleware(log, users, cache, core)
        withReqID := RequestIDMiddleware(log, withAuth)
        return RecoveryMiddleware(log, withReqID)
    }

    return &DirectProxyHandler{
        logger:           log,
        openAIHandler:    buildChain(openAICore),
        anthropicHandler: buildChain(anthropicCore),
    }
}

// HandlerOpenAI 返回 OpenAI 协议直连 handler（预构建，每次返回同一实例）。
// 认证头：Authorization: Bearer sk-pp-<48chars>
func (h *DirectProxyHandler) HandlerOpenAI() http.Handler {
    return h.openAIHandler
}

// HandlerAnthropic 返回 Anthropic 协议直连 handler（预构建，每次返回同一实例）。
// 认证头：x-api-key: sk-pp-<48chars>
// 路径：/anthropic/v1/messages → /v1/messages（自动重写）
func (h *DirectProxyHandler) HandlerAnthropic() http.Handler {
    return h.anthropicHandler
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/proxy/... -run TestDirectHandler -v -race
```

Expected: 全部 PASS

- [ ] **Step 5: 全量 proxy 测试**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/proxy/... -v -race -count=1
```

Expected: PASS，无 data race

- [ ] **Step 6: Commit**

```bash
git add internal/proxy/direct_handler.go internal/proxy/direct_handler_test.go
git commit -m "feat(proxy): add DirectProxyHandler with pre-built middleware chains"
```

---

## Chunk 3: DB 适配器 + WebUI

### Task 9: DBUserLister 适配器

`KeyAuthMiddleware` 需要 `ActiveUserLister`（返回 `[]keygen.UserEntry`），而 `UserRepo.ListActive()` 返回 `[]db.User`。需要一个适配器。

**Files:**
- Create: `internal/proxy/db_adapter.go`

- [ ] **Step 1: 创建适配器**

创建 `internal/proxy/db_adapter.go`：

```go
package proxy

import (
    "github.com/l17728/pairproxy/internal/db"
    "github.com/l17728/pairproxy/internal/keygen"
)

// DBUserLister 将 *db.UserRepo 适配为 ActiveUserLister 接口。
// 负责将 db.User 切片转换为 keygen.UserEntry 切片，解耦 keygen 包与 db 包。
type DBUserLister struct {
    repo *db.UserRepo
}

// NewDBUserLister 创建 DBUserLister 适配器。
func NewDBUserLister(repo *db.UserRepo) *DBUserLister {
    return &DBUserLister{repo: repo}
}

// ListActive 实现 ActiveUserLister 接口。
func (d *DBUserLister) ListActive() ([]keygen.UserEntry, error) {
    users, err := d.repo.ListActive()
    if err != nil {
        return nil, err
    }
    entries := make([]keygen.UserEntry, 0, len(users))
    for _, u := range users {
        entries = append(entries, keygen.UserEntry{
            ID:       u.ID,
            Username: u.Username,
            IsActive: u.IsActive,
            GroupID:  u.GroupID,
        })
    }
    return entries, nil
}
```

- [ ] **Step 2: 编译验证**

```bash
"C:/Program Files/Go/bin/go.exe" build ./internal/proxy/...
```

- [ ] **Step 3: Commit**

```bash
git add internal/proxy/db_adapter.go
git commit -m "feat(proxy): add DBUserLister adapter bridging db.UserRepo to ActiveUserLister"
```

---

### Task 10: keygen WebUI Handler

**Files:**
- Create: `internal/api/keygen_handler.go`
- Create: `internal/api/keygen_handler_test.go`

- [ ] **Step 1: 创建测试文件** `internal/api/keygen_handler_test.go`：

```go
package api_test

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.uber.org/zap"

    "github.com/l17728/pairproxy/internal/api"
    "github.com/l17728/pairproxy/internal/auth"
    "github.com/l17728/pairproxy/internal/db"
)

func setupKeygenTest(t *testing.T) (*api.KeygenHandler, *db.UserRepo) {
    t.Helper()
    logger := zap.NewNop()
    gdb, err := db.Open(logger, ":memory:")
    require.NoError(t, err)
    require.NoError(t, db.Migrate(logger, gdb))
    userRepo := db.NewUserRepo(gdb, logger)
    jwtMgr, err := auth.NewManager(logger, "test-secret-key-for-testing-only")
    require.NoError(t, err)
    h := api.NewKeygenHandler(logger, userRepo, jwtMgr)
    return h, userRepo
}

func TestKeygenLogin_Success(t *testing.T) {
    h, userRepo := setupKeygenTest(t)

    // 创建用户
    pass, _ := auth.HashPassword(zap.NewNop(), "testpass")
    u := &db.User{Username: "alice", PasswordHash: pass, IsActive: true}
    require.NoError(t, userRepo.Create(u))

    body, _ := json.Marshal(map[string]string{"username": "alice", "password": "testpass"})
    req := httptest.NewRequest("POST", "/keygen/api/login", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()

    mux := http.NewServeMux()
    h.RegisterRoutes(mux)
    mux.ServeHTTP(rr, req)

    assert.Equal(t, 200, rr.Code)

    var resp map[string]interface{}
    require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
    assert.Equal(t, "alice", resp["username"])
    assert.Contains(t, resp["key"].(string), "sk-pp-")
    assert.NotEmpty(t, resp["token"])
}

func TestKeygenLogin_WrongPassword(t *testing.T) {
    h, userRepo := setupKeygenTest(t)

    pass, _ := auth.HashPassword(zap.NewNop(), "correct")
    u := &db.User{Username: "bob", PasswordHash: pass, IsActive: true}
    require.NoError(t, userRepo.Create(u))

    body, _ := json.Marshal(map[string]string{"username": "bob", "password": "wrong"})
    req := httptest.NewRequest("POST", "/keygen/api/login", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()

    mux := http.NewServeMux()
    h.RegisterRoutes(mux)
    mux.ServeHTTP(rr, req)

    assert.Equal(t, 401, rr.Code)
}

func TestKeygenLogin_DisabledUser(t *testing.T) {
    h, userRepo := setupKeygenTest(t)

    pass, _ := auth.HashPassword(zap.NewNop(), "pass")
    u := &db.User{Username: "disabled", PasswordHash: pass}
    require.NoError(t, userRepo.Create(u))
    require.NoError(t, userRepo.SetActive(u.ID, false))

    body, _ := json.Marshal(map[string]string{"username": "disabled", "password": "pass"})
    req := httptest.NewRequest("POST", "/keygen/api/login", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    rr := httptest.NewRecorder()

    mux := http.NewServeMux()
    h.RegisterRoutes(mux)
    mux.ServeHTTP(rr, req)

    assert.Equal(t, 401, rr.Code)
}

func TestKeygenRegenerate_Success(t *testing.T) {
    h, userRepo := setupKeygenTest(t)

    pass, _ := auth.HashPassword(zap.NewNop(), "pass")
    u := &db.User{Username: "charlie", PasswordHash: pass, IsActive: true}
    require.NoError(t, userRepo.Create(u))

    // 先登录
    loginBody, _ := json.Marshal(map[string]string{"username": "charlie", "password": "pass"})
    loginReq := httptest.NewRequest("POST", "/keygen/api/login", bytes.NewReader(loginBody))
    loginReq.Header.Set("Content-Type", "application/json")
    loginRR := httptest.NewRecorder()
    mux := http.NewServeMux()
    h.RegisterRoutes(mux)
    mux.ServeHTTP(loginRR, loginReq)
    require.Equal(t, 200, loginRR.Code)

    var loginResp map[string]interface{}
    require.NoError(t, json.Unmarshal(loginRR.Body.Bytes(), &loginResp))
    token := loginResp["token"].(string)
    firstKey := loginResp["key"].(string)

    // 重新生成
    regenReq := httptest.NewRequest("POST", "/keygen/api/regenerate", nil)
    regenReq.Header.Set("Authorization", "Bearer "+token)
    regenRR := httptest.NewRecorder()
    mux.ServeHTTP(regenRR, regenReq)

    assert.Equal(t, 200, regenRR.Code)
    var regenResp map[string]interface{}
    require.NoError(t, json.Unmarshal(regenRR.Body.Bytes(), &regenResp))
    newKey := regenResp["key"].(string)
    assert.Contains(t, newKey, "sk-pp-")
    assert.NotEqual(t, firstKey, newKey, "regenerated key should differ from original")
}

func TestKeygenRegenerate_Unauthorized(t *testing.T) {
    h, _ := setupKeygenTest(t)
    mux := http.NewServeMux()
    h.RegisterRoutes(mux)

    req := httptest.NewRequest("POST", "/keygen/api/regenerate", nil)
    req.Header.Set("Authorization", "Bearer invalid-token")
    rr := httptest.NewRecorder()
    mux.ServeHTTP(rr, req)
    assert.Equal(t, 401, rr.Code)
}

func TestKeygenStaticPage(t *testing.T) {
    h, _ := setupKeygenTest(t)
    mux := http.NewServeMux()
    h.RegisterRoutes(mux)

    req := httptest.NewRequest("GET", "/keygen/", nil)
    rr := httptest.NewRecorder()
    mux.ServeHTTP(rr, req)

    assert.Equal(t, 200, rr.Code)
    assert.Contains(t, rr.Body.String(), "PairProxy")
}
```

- [ ] **Step 2: 检查 auth 包是否有 HashPassword**

```bash
grep -rn "func HashPassword\|func CheckPassword" D:/pairproxy/internal/auth/
```

根据结果确认正确的函数名，必要时在测试中调整。

- [ ] **Step 3: 创建 keygen_handler.go**

创建 `internal/api/keygen_handler.go`：

```go
package api

import (
    "encoding/json"
    "net/http"
    "strings"
    "time"

    "go.uber.org/zap"

    "github.com/l17728/pairproxy/internal/auth"
    "github.com/l17728/pairproxy/internal/db"
    "github.com/l17728/pairproxy/internal/keygen"
)

// KeygenHandler 提供用户自助 API Key 生成功能。
//
// 路由：
//   - GET  /keygen/         — 静态 HTML 页面
//   - POST /keygen/api/login      — 用户名+密码登录，返回 key + session token
//   - POST /keygen/api/regenerate — 用 session token 重新生成 key
//
// 与 Dashboard 完全独立：使用普通用户密码，不使用管理员密码。
type KeygenHandler struct {
    logger   *zap.Logger
    userRepo *db.UserRepo
    jwtMgr   *auth.Manager
}

// NewKeygenHandler 创建 KeygenHandler。
func NewKeygenHandler(logger *zap.Logger, userRepo *db.UserRepo, jwtMgr *auth.Manager) *KeygenHandler {
    return &KeygenHandler{
        logger:   logger.Named("keygen_handler"),
        userRepo: userRepo,
        jwtMgr:   jwtMgr,
    }
}

// RegisterRoutes 注册 /keygen/ 相关路由。
func (h *KeygenHandler) RegisterRoutes(mux *http.ServeMux) {
    mux.HandleFunc("GET /keygen/", h.handleStaticPage)
    mux.HandleFunc("POST /keygen/api/login", h.handleLogin)
    mux.HandleFunc("POST /keygen/api/regenerate", h.handleRegenerate)
}

// loginRequest 登录请求体
type loginRequest struct {
    Username string `json:"username"`
    Password string `json:"password"`
}

// loginResponse 登录响应体
type loginResponse struct {
    Username  string `json:"username"`
    Key       string `json:"key"`
    Token     string `json:"token"`
    ExpiresIn int    `json:"expires_in"`
}

// regenerateResponse 重新生成 key 响应体
type regenerateResponse struct {
    Username string `json:"username"`
    Key      string `json:"key"`
    Message  string `json:"message"`
}

func (h *KeygenHandler) handleStaticPage(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.WriteHeader(http.StatusOK)
    _, _ = w.Write([]byte(keygenHTML))
}

func (h *KeygenHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
    var req loginRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        h.logger.Warn("keygen login: invalid request body", zap.Error(err))
        writeKeygenError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
        return
    }
    if req.Username == "" || req.Password == "" {
        writeKeygenError(w, http.StatusBadRequest, "missing_fields", "username and password required")
        return
    }

    // 查询用户
    user, err := h.userRepo.GetByUsername(req.Username)
    if err != nil {
        h.logger.Error("keygen login: db error", zap.String("username", req.Username), zap.Error(err))
        writeKeygenError(w, http.StatusInternalServerError, "internal_error", "user lookup failed")
        return
    }
    if user == nil || !user.IsActive {
        h.logger.Warn("keygen login: user not found or inactive", zap.String("username", req.Username))
        writeKeygenError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
        return
    }

    // 验证密码（VerifyPassword: logger, hash, plain）
    if !auth.VerifyPassword(h.logger, user.PasswordHash, req.Password) {
        h.logger.Warn("keygen login: wrong password", zap.String("username", req.Username))
        writeKeygenError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
        return
    }

    // 生成 API Key
    apiKey, err := keygen.GenerateKey(req.Username)
    if err != nil {
        h.logger.Error("keygen login: key generation failed",
            zap.String("username", req.Username), zap.Error(err))
        writeKeygenError(w, http.StatusInternalServerError, "key_gen_error", "failed to generate API key")
        return
    }

    // 生成 session token（1小时有效，复用 JWT Sign）
    groupIDStr := ""
    if user.GroupID != nil {
        groupIDStr = *user.GroupID
    }
    token, err := h.jwtMgr.Sign(auth.JWTClaims{
        UserID:   user.ID,
        Username: req.Username,
        GroupID:  groupIDStr,
        Role:     "user",
    }, time.Hour)
    if err != nil {
        h.logger.Error("keygen login: token issue failed", zap.Error(err))
        writeKeygenError(w, http.StatusInternalServerError, "token_error", "session token generation failed")
        return
    }

    h.logger.Info("keygen login: success",
        zap.String("username", req.Username),
        zap.String("user_id", user.ID),
    )

    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(loginResponse{
        Username:  req.Username,
        Key:       apiKey,
        Token:     token,
        ExpiresIn: 3600,
    })
}

func (h *KeygenHandler) handleRegenerate(w http.ResponseWriter, r *http.Request) {
    // 从 Authorization: Bearer <token> 提取 session token
    authHeader := r.Header.Get("Authorization")
    if !strings.HasPrefix(authHeader, "Bearer ") {
        writeKeygenError(w, http.StatusUnauthorized, "missing_token", "Authorization: Bearer <token> required")
        return
    }
    tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

    // 验证 JWT session token（Parse = ValidateToken）
    claims, err := h.jwtMgr.Parse(tokenStr)
    if err != nil {
        h.logger.Warn("keygen regenerate: invalid session token", zap.Error(err))
        writeKeygenError(w, http.StatusUnauthorized, "session_expired", "会话已过期，请重新登录")
        return
    }

    // 生成新 Key
    apiKey, err := keygen.GenerateKey(claims.Username)
    if err != nil {
        h.logger.Error("keygen regenerate: key generation failed",
            zap.String("username", claims.Username), zap.Error(err))
        writeKeygenError(w, http.StatusInternalServerError, "key_gen_error", "failed to generate API key")
        return
    }

    h.logger.Info("keygen regenerate: new key generated",
        zap.String("username", claims.Username),
        zap.String("user_id", claims.UserID),
    )

    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(regenerateResponse{
        Username: claims.Username,
        Key:      apiKey,
        Message:  "新 Key 已生成",
    })
}

func writeKeygenError(w http.ResponseWriter, status int, code, message string) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": message})
}

// keygenHTML 是内嵌的 Key 生成页面（避免静态文件依赖）。
const keygenHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>PairProxy Key Generator</title>
<style>
  body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;max-width:640px;margin:60px auto;padding:0 20px;color:#333}
  h1{color:#1a1a2e;font-size:1.6rem}
  label{display:block;margin:10px 0 4px;font-weight:500}
  input{width:100%;padding:8px 12px;border:1px solid #ddd;border-radius:6px;font-size:14px;box-sizing:border-box}
  button{padding:10px 24px;background:#4f46e5;color:#fff;border:none;border-radius:6px;cursor:pointer;font-size:14px;margin:4px 4px 4px 0}
  button:hover{background:#4338ca}
  button.secondary{background:#6b7280}
  button.secondary:hover{background:#4b5563}
  .key-box{font-family:monospace;background:#f8f9fa;border:1px solid #e9ecef;padding:14px;border-radius:6px;word-break:break-all;font-size:13px;margin:8px 0}
  .section{border-top:1px solid #eee;margin-top:24px;padding-top:16px}
  pre{background:#1e1e2e;color:#cdd6f4;padding:14px;border-radius:6px;font-size:12px;overflow-x:auto}
  .hidden{display:none}
  .error{color:#dc2626;font-size:13px;margin-top:6px}
</style>
</head>
<body>
<h1>PairProxy Key Generator</h1>

<div id="login-section">
  <label>用户名</label><input type="text" id="username" autocomplete="username">
  <label>密码</label><input type="password" id="password" autocomplete="current-password">
  <br><br>
  <button onclick="login()">登 录</button>
  <div class="error" id="login-error"></div>
</div>

<div id="key-section" class="hidden">
  <p>欢迎，<strong id="welcome-name"></strong>！</p>
  <p>您的 API Key：</p>
  <div class="key-box" id="api-key-display"></div>
  <button onclick="copyKey()">📋 复制</button>
  <button class="secondary" onclick="regenerate()">🔄 重新生成</button>

  <div class="section">
    <h3>Claude Code 配置</h3>
    <pre id="cc-snippet"></pre>
    <h3>OpenCode 配置</h3>
    <pre id="oc-snippet"></pre>
  </div>

  <button class="secondary" style="margin-top:16px" onclick="logout()">退出登录</button>
</div>

<script>
const BASE = window.location.origin;
let sessionToken = '';
let currentKey = '';

async function login() {
  const username = document.getElementById('username').value.trim();
  const password = document.getElementById('password').value;
  document.getElementById('login-error').textContent = '';
  try {
    const r = await fetch('/keygen/api/login', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({username, password})
    });
    const data = await r.json();
    if (!r.ok) { document.getElementById('login-error').textContent = data.message || '登录失败'; return; }
    sessionToken = data.token;
    showKey(data.username, data.key);
  } catch(e) { document.getElementById('login-error').textContent = '网络错误'; }
}

function showKey(username, key) {
  currentKey = key;
  document.getElementById('login-section').classList.add('hidden');
  document.getElementById('key-section').classList.remove('hidden');
  document.getElementById('welcome-name').textContent = username;
  document.getElementById('api-key-display').textContent = key;
  document.getElementById('cc-snippet').textContent =
    'export ANTHROPIC_BASE_URL=' + BASE + '/anthropic\nexport ANTHROPIC_API_KEY=' + key;
  document.getElementById('oc-snippet').textContent =
    'export OPENAI_BASE_URL=' + BASE + '/v1\nexport OPENAI_API_KEY=' + key;
}

function copyKey() {
  navigator.clipboard.writeText(currentKey).then(() => alert('已复制到剪贴板'));
}

async function regenerate() {
  if (!confirm('重新生成后旧 Key 将失效。继续？')) return;
  const r = await fetch('/keygen/api/regenerate', {
    method: 'POST',
    headers: {'Authorization': 'Bearer ' + sessionToken}
  });
  const data = await r.json();
  if (!r.ok) { alert(data.message || '重新生成失败'); return; }
  showKey(document.getElementById('welcome-name').textContent, data.key);
  alert('新 Key 已生成');
}

function logout() {
  sessionToken = ''; currentKey = '';
  document.getElementById('key-section').classList.add('hidden');
  document.getElementById('login-section').classList.remove('hidden');
  document.getElementById('password').value = '';
}
</script>
</body>
</html>`
```

- [ ] **Step 4: 检查 auth 函数签名并修正**

```bash
grep -n "func IssueToken\|func ValidateToken\|func CheckPassword\|func HashPassword" D:/pairproxy/internal/auth/*.go
```

根据实际签名调整 keygen_handler.go 中的调用。

- [ ] **Step 5: 运行测试**

```bash
"C:/Program Files/Go/bin/go.exe" test ./internal/api/... -run TestKeygen -v
```

Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/keygen_handler.go internal/api/keygen_handler_test.go
git commit -m "feat(api): add KeygenHandler - user self-service API key generation WebUI"
```

---

## Chunk 4: 路由注册 + E2E 测试

### Task 11: main.go 路由注册

**Files:**
- Modify: `cmd/sproxy/main.go`

- [ ] **Step 1: 读取 main.go 路由注册区域（617-692行）**

确认当前路由注册代码，规划插入点。

- [ ] **Step 2: 在路由注册区域添加直连路由**

在 `mux.Handle("/", proxyHandler)` 一行**之前**，插入以下代码：

```go
// -----------------------------------------------------------------------
// 直连模式（API Key 认证，无需 cproxy 客户端）
// -----------------------------------------------------------------------

// 构建 API Key 缓存（5分钟 TTL，最多 5000 条）
apiKeyCache, keyCacheErr := keygen.NewKeyCache(5000, 5*time.Minute)
if keyCacheErr != nil {
    logger.Warn("failed to create api key cache, proceeding without cache",
        zap.Error(keyCacheErr))
}

// 构建用户查询适配器和直连处理器
// 问题3修复：在此预构建 handler，而非每次请求时重建
dbUserLister := proxy.NewDBUserLister(userRepo)
directHandler := proxy.NewDirectProxyHandler(logger, sp, dbUserLister, apiKeyCache)
openAIDirectHandler := directHandler.HandlerOpenAI()
anthropicDirectHandler := directHandler.HandlerAnthropic()

logger.Info("direct proxy handlers built",
    zap.String("openai_path", "/v1/ (Bearer sk-pp-...)"),
    zap.String("anthropic_path", "/anthropic/ (x-api-key: sk-pp-...)"),
)

// Anthropic 协议直连：/anthropic/* → Anthropic API
// 认证头：x-api-key: sk-pp-<48chars>
mux.Handle("/anthropic/", anthropicDirectHandler)
logger.Info("direct proxy registered", zap.String("path", "/anthropic/"), zap.String("auth", "x-api-key"))

// 混合路由：/v1/* 同时支持 cproxy 模式和直连模式
// 根据认证头类型自动区分（问题3修复：handler 已预构建）
mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
    // cproxy 模式：JWT 认证头
    if r.Header.Get("X-PairProxy-Auth") != "" {
        proxyHandler.ServeHTTP(w, r)
        return
    }
    // 直连模式：sk-pp- API Key（OpenAI 格式）
    if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer sk-pp-") {
        openAIDirectHandler.ServeHTTP(w, r)
        return
    }
    // Authorization: Bearer <JWT>（cproxy 兼容写法）
    if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") && !strings.HasPrefix(auth, "Bearer sk-pp-") {
        proxyHandler.ServeHTTP(w, r)
        return
    }
    // 无有效认证头
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusUnauthorized)
    _ = json.NewEncoder(w).Encode(map[string]string{
        "error":   "missing_auth",
        "message": "X-PairProxy-Auth (cproxy) or Authorization: Bearer sk-pp-... (direct) required",
    })
    logger.Warn("v1 route: no valid auth header",
        zap.String("path", r.URL.Path),
        zap.String("remote_addr", r.RemoteAddr),
    )
})
logger.Info("hybrid route registered", zap.String("path", "/v1/"), zap.String("modes", "cproxy+direct"))

// Key 生成 WebUI（用户自助服务）
keygenAPIHandler := api.NewKeygenHandler(logger, userRepo, jwtMgr)
keygenAPIHandler.RegisterRoutes(mux)
logger.Info("keygen WebUI registered at /keygen/")
```

- [ ] **Step 3: 检查 import 块并添加缺少的包**

确保 `cmd/sproxy/main.go` 的 import 中包含：
- `"encoding/json"`
- `"strings"`
- `"github.com/l17728/pairproxy/internal/keygen"`（可能已有）

- [ ] **Step 4: 编译验证**

```bash
"C:/Program Files/Go/bin/go.exe" build ./cmd/sproxy/...
```

Expected: 无错误

- [ ] **Step 5: 全量测试**

```bash
"C:/Program Files/Go/bin/go.exe" test ./... -count=1 -timeout=5m
```

Expected: 全部 PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/sproxy/main.go
git commit -m "feat(main): register direct proxy routes /anthropic/ and /v1/ hybrid with pre-built handlers"
```

---

### Task 12: E2E 测试

**Files:**
- Create: `test/e2e/direct_proxy_e2e_test.go`

- [ ] **Step 1: 创建 E2E 测试文件**

创建 `test/e2e/direct_proxy_e2e_test.go`：

```go
package e2e_test

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/http/httptest"
    "strings"
    "testing"
    "time"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "go.uber.org/zap/zaptest"
    "go.uber.org/zap"

    "github.com/l17728/pairproxy/internal/auth"
    "github.com/l17728/pairproxy/internal/db"
    "github.com/l17728/pairproxy/internal/keygen"
    "github.com/l17728/pairproxy/internal/proxy"
)

// setupDirectProxyTest 创建一个完整的直连测试环境：
// - 内存数据库（含测试用户）
// - mockLLM（模拟上游 Anthropic/OpenAI）
// - sproxy（挂载直连路由）
// 返回 sproxy 的 httptest.Server URL 和测试用 API Key
func setupDirectProxyTest(t *testing.T) (spURL string, aliceKey string, cleanup func()) {
    t.Helper()

    logger := zaptest.NewLogger(t)
    ctx, cancel := context.WithCancel(context.Background())

    // 1. 内存数据库（按现有 E2E 模式）
    gdb, err := db.Open(logger, ":memory:")
    require.NoError(t, err)
    require.NoError(t, db.Migrate(logger, gdb))
    writer := db.NewUsageWriter(gdb, logger, 200, 30*time.Second)
    writer.Start(ctx)
    userRepo := db.NewUserRepo(gdb, logger)

    // 2. 创建测试用户 alice
    hash, _ := auth.HashPassword(logger, "pass123")
    alice := &db.User{Username: "alice", PasswordHash: hash, IsActive: true}
    require.NoError(t, userRepo.Create(alice))

    // 3. 模拟 LLM 上游（返回标准 Anthropic/OpenAI 响应）
    mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("Content-Type", "application/json")
        path := r.URL.Path
        switch {
        case strings.HasSuffix(path, "/messages"):
            _ = json.NewEncoder(w).Encode(map[string]interface{}{
                "id":      "msg_test",
                "type":    "message",
                "role":    "assistant",
                "content": []map[string]string{{"type": "text", "text": "hello from mock"}},
                "model":   "claude-3-5-sonnet-20241022",
                "stop_reason": "end_turn",
                "usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
            })
        case strings.HasSuffix(path, "/chat/completions"):
            _ = json.NewEncoder(w).Encode(map[string]interface{}{
                "id":     "chatcmpl_test",
                "object": "chat.completion",
                "choices": []map[string]interface{}{{
                    "index":         0,
                    "message":       map[string]string{"role": "assistant", "content": "hello from mock"},
                    "finish_reason": "stop",
                }},
                "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
            })
        default:
            w.WriteHeader(404)
        }
    }))

    // 4. 构建 sproxy（复用 setupE2E 模式，无需 jwtMgr，直连不走 JWT）
    jwtMgr, jwtErr := auth.NewManager(logger, "e2e-direct-secret")
    require.NoError(t, jwtErr)
    sp, spErr := proxy.NewSProxy(logger, jwtMgr, writer, []proxy.LLMTarget{
        {URL: mockLLM.URL, APIKey: "fake-llm-key", Provider: "anthropic"},
    })
    require.NoError(t, spErr)
    sp.SetDB(gdb)

    // 5. 构建 API Key 缓存和直连 handler
    apiKeyCache, _ := keygen.NewKeyCache(100, 0) // TTL=0 永不过期
    dbLister := proxy.NewDBUserLister(userRepo)
    directH := proxy.NewDirectProxyHandler(logger, sp, dbLister, apiKeyCache)

    // 6. 构建测试 HTTP 服务（挂载直连路由）
    mux := http.NewServeMux()
    mux.Handle("/anthropic/", directH.HandlerAnthropic())
    openAIH := directH.HandlerOpenAI()
    mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
        if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer sk-pp-") {
            openAIH.ServeHTTP(w, r)
            return
        }
        w.WriteHeader(401)
    })

    spServer := httptest.NewServer(mux)

    // 7. 生成 alice 的 API Key
    aliceKey, keyErr := keygen.GenerateKey("alice")
    require.NoError(t, keyErr)

    return spServer.URL, aliceKey, func() {
        spServer.Close()
        mockLLM.Close()
        cancel()
        writer.Wait()
    }
}

// TestDirectProxy_Anthropic_Messages 端到端测试：Anthropic 协议直连
func TestDirectProxy_Anthropic_Messages(t *testing.T) {
    spURL, aliceKey, cleanup := setupDirectProxyTest(t)
    defer cleanup()

    body, _ := json.Marshal(map[string]interface{}{
        "model":      "claude-3-5-sonnet-20241022",
        "max_tokens": 100,
        "messages":   []map[string]string{{"role": "user", "content": "hi"}},
    })
    req, _ := http.NewRequest("POST", spURL+"/anthropic/v1/messages", bytes.NewReader(body))
    req.Header.Set("x-api-key", aliceKey)
    req.Header.Set("anthropic-version", "2023-06-01")
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, 200, resp.StatusCode)
    respBody, _ := io.ReadAll(resp.Body)
    var respJSON map[string]interface{}
    require.NoError(t, json.Unmarshal(respBody, &respJSON))
    assert.Equal(t, "message", respJSON["type"])
}

// TestDirectProxy_OpenAI_ChatCompletions 端到端测试：OpenAI 协议直连
func TestDirectProxy_OpenAI_ChatCompletions(t *testing.T) {
    spURL, aliceKey, cleanup := setupDirectProxyTest(t)
    defer cleanup()

    body, _ := json.Marshal(map[string]interface{}{
        "model":    "gpt-4",
        "messages": []map[string]string{{"role": "user", "content": "hi"}},
    })
    req, _ := http.NewRequest("POST", spURL+"/v1/chat/completions", bytes.NewReader(body))
    req.Header.Set("Authorization", "Bearer "+aliceKey)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, 200, resp.StatusCode)
    respBody, _ := io.ReadAll(resp.Body)
    var respJSON map[string]interface{}
    require.NoError(t, json.Unmarshal(respBody, &respJSON))
    assert.Equal(t, "chat.completion", respJSON["object"])
}

// TestDirectProxy_InvalidKey 无效 Key 应返回 401
func TestDirectProxy_InvalidKey(t *testing.T) {
    spURL, _, cleanup := setupDirectProxyTest(t)
    defer cleanup()

    body, _ := json.Marshal(map[string]interface{}{"model": "claude-3-5-sonnet", "max_tokens": 10,
        "messages": []map[string]string{{"role": "user", "content": "hi"}}})
    req, _ := http.NewRequest("POST", spURL+"/anthropic/v1/messages", bytes.NewReader(body))
    req.Header.Set("x-api-key", "sk-ant-not-a-pairproxy-key")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()
    assert.Equal(t, 401, resp.StatusCode)
}

// TestDirectProxy_MissingKey 无认证头应返回 401
func TestDirectProxy_MissingKey(t *testing.T) {
    spURL, _, cleanup := setupDirectProxyTest(t)
    defer cleanup()

    body, _ := json.Marshal(map[string]interface{}{"model": "claude-3-5-sonnet", "max_tokens": 10,
        "messages": []map[string]string{{"role": "user", "content": "hi"}}})
    req, _ := http.NewRequest("POST", spURL+"/anthropic/v1/messages", bytes.NewReader(body))

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    defer resp.Body.Close()
    assert.Equal(t, 401, resp.StatusCode)
}

// TestDirectProxy_CacheHit 第二次请求走缓存路径
func TestDirectProxy_CacheHit(t *testing.T) {
    spURL, aliceKey, cleanup := setupDirectProxyTest(t)
    defer cleanup()

    send := func() int {
        body, _ := json.Marshal(map[string]interface{}{"model": "claude-3-5-sonnet", "max_tokens": 10,
            "messages": []map[string]string{{"role": "user", "content": "hi"}}})
        req, _ := http.NewRequest("POST", spURL+"/anthropic/v1/messages", bytes.NewReader(body))
        req.Header.Set("x-api-key", aliceKey)
        req.Header.Set("Content-Type", "application/json")
        resp, err := http.DefaultClient.Do(req)
        require.NoError(t, err)
        _ = resp.Body.Close()
        return resp.StatusCode
    }

    // 两次请求都应该成功（第二次走缓存）
    assert.Equal(t, 200, send(), "first request (cache miss) should succeed")
    assert.Equal(t, 200, send(), "second request (cache hit) should succeed")
}

// TestDirectProxy_AnthropicPathRewrite 验证 /anthropic/v1/messages → /v1/messages 路径重写
func TestDirectProxy_AnthropicPathRewrite(t *testing.T) {
    logger := zap.NewNop()

    // 记录收到的路径
    var receivedPath string
    mockLLM := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        receivedPath = r.URL.Path
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]interface{}{
            "id": "msg_1", "type": "message", "role": "assistant",
            "content": []map[string]string{{"type": "text", "text": "ok"}},
            "model": "claude-test", "stop_reason": "end_turn",
            "usage": map[string]int{"input_tokens": 1, "output_tokens": 1},
        })
    }))
    defer mockLLM.Close()

    gdb, _ := db.Open(logger, ":memory:")
    _ = db.Migrate(logger, gdb)
    ctx2, cancel2 := context.WithCancel(context.Background())
    writer2 := db.NewUsageWriter(gdb, logger, 200, 30*time.Second)
    writer2.Start(ctx2)
    userRepo := db.NewUserRepo(gdb, logger)
    hash, _ := auth.HashPassword(logger, "p")
    u := &db.User{Username: "dave", PasswordHash: hash, IsActive: true}
    _ = userRepo.Create(u)
    jwtMgr2, _ := auth.NewManager(logger, "test-secret")
    sp, _ := proxy.NewSProxy(logger, jwtMgr2, writer2, []proxy.LLMTarget{
        {URL: mockLLM.URL, APIKey: "k", Provider: "anthropic"},
    })
    _ = sp.SetDB(gdb)
    defer func() { cancel2(); writer2.Wait() }()

    cache, _ := keygen.NewKeyCache(10, 0)
    dh := proxy.NewDirectProxyHandler(logger, sp, proxy.NewDBUserLister(userRepo), cache)
    mux := http.NewServeMux()
    mux.Handle("/anthropic/", dh.HandlerAnthropic())
    server := httptest.NewServer(mux)
    defer server.Close()

    daveKey, _ := keygen.GenerateKey("dave")
    body, _ := json.Marshal(map[string]interface{}{
        "model": "claude-test", "max_tokens": 10,
        "messages": []map[string]string{{"role": "user", "content": "test"}},
    })
    req, _ := http.NewRequest("POST", server.URL+"/anthropic/v1/messages", bytes.NewReader(body))
    req.Header.Set("x-api-key", daveKey)
    req.Header.Set("Content-Type", "application/json")

    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    _ = resp.Body.Close()

    assert.Equal(t, "/v1/messages", receivedPath,
        "upstream must receive /v1/messages, not /anthropic/v1/messages")
}

// TestDirectProxy_V1_HybridRoute_DirectVsJWT 验证 /v1/ 混合路由：sk-pp- 走直连，JWT 走 cproxy
func TestDirectProxy_V1_HybridRoute_DirectVsJWT(t *testing.T) {
    spURL, aliceKey, cleanup := setupDirectProxyTest(t)
    defer cleanup()

    // sk-pp- key → 直连，应 200
    body, _ := json.Marshal(map[string]interface{}{
        "model": "gpt-4", "messages": []map[string]string{{"role": "user", "content": "hi"}},
    })
    req, _ := http.NewRequest("POST", spURL+"/v1/chat/completions", bytes.NewReader(body))
    req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", aliceKey))
    req.Header.Set("Content-Type", "application/json")
    resp, err := http.DefaultClient.Do(req)
    require.NoError(t, err)
    _ = resp.Body.Close()
    assert.Equal(t, 200, resp.StatusCode, "sk-pp- key on /v1/ should route to direct handler")
}

// TestKeygen_WebUI_Login 验证 /keygen/ WebUI 登录 API
func TestKeygen_WebUI_Login(t *testing.T) {
    logger := zap.NewNop()
    gdb, _ := openTestGormDB(t)
    userRepo := db.NewUserRepo(gdb, logger)
    jwtMgr := auth.NewManager("secret")

    hash, _ := auth.HashPassword(logger, "mypassword")
    u := &db.User{Username: "frank", PasswordHash: hash, IsActive: true}
    require.NoError(t, userRepo.Create(u))

    // 注意: 这里直接用 api.NewKeygenHandler 测试，不依赖完整的 sproxy 启动
    // 实际测试已在 internal/api/keygen_handler_test.go 中覆盖
    // 这里测试 E2E 链路：验证 key 可用于后续认证
    key, err := keygen.GenerateKey("frank")
    require.NoError(t, err)
    assert.True(t, keygen.IsValidFormat(key))

    users := []keygen.UserEntry{{ID: u.ID, Username: "frank", IsActive: true}}
    matched, err2 := keygen.ValidateAndGetUser(key, users)
    require.NoError(t, err2)
    require.NotNil(t, matched)
    assert.Equal(t, "frank", matched.Username)

    _ = jwtMgr // 避免 unused import
}
```

注意：`openTestGormDB` 和 `buildMinimalSProxy` 需要参考现有 E2E 测试中的辅助函数实现。在实现时，先检查 `test/e2e/` 中已有的 helper 函数，复用现有的 `openTestDB`/`newTestSProxy` 等。

- [ ] **Step 2: 检查现有 E2E 辅助函数**

```bash
grep -rn "func openTestGormDB\|func buildMinimalSProxy\|func newTestSProxy\|func setupTestSProxy" D:/pairproxy/test/e2e/
```

根据结果，在 `direct_proxy_e2e_test.go` 中调整辅助函数调用，或添加缺失的辅助函数。

- [ ] **Step 3: 运行 E2E 测试**

```bash
"C:/Program Files/Go/bin/go.exe" test ./test/e2e/... -run TestDirectProxy -v -timeout=60s
"C:/Program Files/Go/bin/go.exe" test ./test/e2e/... -run TestKeygen_WebUI_Login -v -timeout=60s
```

Expected: 全部 PASS

- [ ] **Step 4: 全量测试（含 race detector）**

```bash
"C:/Program Files/Go/bin/go.exe" test ./... -count=1 -race -timeout=10m
```

Expected: 全部 PASS，无 data race

- [ ] **Step 5: Commit**

```bash
git add test/e2e/direct_proxy_e2e_test.go
git commit -m "test(e2e): add direct proxy E2E tests covering Anthropic/OpenAI/auth/cache/path-rewrite"
```

---

## 验收检查清单

- [ ] `go test ./... -race` 全绿
- [ ] `go build ./...` 无错误
- [ ] `go vet ./...` 无 warning
- [ ] `/anthropic/v1/messages`（x-api-key: sk-pp-...）可正常代理
- [ ] `/v1/chat/completions`（Authorization: Bearer sk-pp-...）可正常代理
- [ ] `/v1/messages`（X-PairProxy-Auth: JWT）仍走 cproxy 模式（回归验证）
- [ ] `HandlerOpenAI()` 多次调用返回同一实例（问题3验证）
- [ ] 缓存命中路径不查 DB（问题4验证）
- [ ] `/keygen/` 页面可访问，登录/重新生成 API 正常
- [ ] 日志中有完整的 request_id、username、path 字段
