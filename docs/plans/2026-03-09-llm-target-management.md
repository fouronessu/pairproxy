# LLM Target 动态管理功能实现计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 实现 LLM Target 动态管理功能，支持配置文件与数据库双来源管理，CLI 命令和 WebUI 界面操作

**Architecture:** 配置文件（只读）+ 数据库（可写）双来源架构。启动时将配置文件中的 targets 同步到数据库（标记为 source='config'），CLI/WebUI 只能管理数据库中 source='database' 的 targets。数据库包含所有 targets。

**Tech Stack:** Go, GORM, SQLite, Cobra CLI, Go templates, Tailwind CSS

---

## Phase 1: 数据库层实现

### Task 1.1: 添加 LLMTarget 模型

**Files:**
- Modify: `internal/db/models.go:116` (在 LLMBinding 后添加)

**Step 1: 添加 LLMTarget 模型定义**

在 `models.go` 的 `LLMBinding` 定义后添加:

```go
// LLMTarget LLM 目标端点（支持配置文件和数据库双来源）
type LLMTarget struct {
	ID              string     `gorm:"primarykey"`
	URL             string     `gorm:"uniqueIndex;not null"` // LLM 端点 URL
	APIKeyID        *string    `gorm:"index"`                // 外键 → api_keys.id（可选）
	Provider        string     `gorm:"default:'anthropic'"`  // "anthropic" | "openai" | "ollama"
	Name            string     // 显示名称
	Weight          int        `gorm:"default:1"`            // 负载均衡权重
	HealthCheckPath string     // 健康检查路径
	Source          string     `gorm:"default:'database'"`   // "config" | "database"
	IsEditable      bool       `gorm:"default:true"`         // false for config-sourced
	IsActive        bool       `gorm:"default:true"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// TableName 指定表名
func (LLMTarget) TableName() string { return "llm_targets" }
```

**Step 2: 更新 TableName 函数列表**

在文件末尾的 TableName 函数列表中添加:

```go
func (LLMTarget) TableName() string      { return "llm_targets" }
```

**Step 3: 提交**

```bash
git add internal/db/models.go
git commit -m "feat(db): add LLMTarget model for dynamic target management"
```

---

### Task 1.2: 数据库迁移 - 添加 llm_targets 表

**Files:**
- Modify: `internal/db/db.go:136` (在 AutoMigrate 列表中添加)

**Step 1: 添加 LLMTarget 到 AutoMigrate**

在 `db.go` 的 `AutoMigrate` 调用中添加 `&LLMTarget{}`:

```go
if err := gormDB.AutoMigrate(
	&Group{},
	&User{},
	&RefreshToken{},
	&UsageLog{},
	&Peer{},
	&AuditLog{},
	&APIKey{},
	&APIKeyAssignment{}, // F-5: API Key 分配
	&LLMBinding{},
	&LLMTarget{}, // 新增
); err != nil {
	return nil, fmt.Errorf("auto migrate: %w", err)
}
```

**Step 2: 提交**

```bash
git add internal/db/db.go
git commit -m "feat(db): add llm_targets table migration"
```

---

### Task 1.3: 实现 LLMTargetRepo - Create 方法

**Files:**
- Create: `internal/db/llmtarget_repo.go`

**Step 1: 编写 Create 方法的测试**

创建 `internal/db/llmtarget_repo_test.go`:

```go
package db

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestLLMTargetRepo_Create(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	repo := NewLLMTargetRepo(gormDB, logger)

	target := &LLMTarget{
		ID:       uuid.NewString(),
		URL:      "http://test.local:8080",
		Provider: "anthropic",
		Name:     "Test Target",
		Weight:   1,
		Source:   "database",
		IsEditable: true,
		IsActive: true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	err = repo.Create(target)
	assert.NoError(t, err)

	// 验证创建成功
	found, err := repo.GetByURL(target.URL)
	require.NoError(t, err)
	assert.Equal(t, target.URL, found.URL)
	assert.Equal(t, target.Provider, found.Provider)
}

func TestLLMTargetRepo_Create_DuplicateURL(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	repo := NewLLMTargetRepo(gormDB, logger)

	target1 := &LLMTarget{
		ID:  uuid.NewString(),
		URL: "http://test.local:8080",
		Source: "database",
	}
	err = repo.Create(target1)
	require.NoError(t, err)

	// 尝试创建相同 URL
	target2 := &LLMTarget{
		ID:  uuid.NewString(),
		URL: "http://test.local:8080",
		Source: "database",
	}
	err = repo.Create(target2)
	assert.Error(t, err) // 应该失败（唯一性约束）
}
```

**Step 2: 运行测试确认失败**

```bash
go test ./internal/db -run TestLLMTargetRepo_Create -v
```

Expected: FAIL (LLMTargetRepo not defined)

**Step 3: 实现 LLMTargetRepo**

创建 `internal/db/llmtarget_repo.go`:

```go
package db

import (
	"fmt"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// LLMTargetRepo LLM Target 数据库操作
type LLMTargetRepo struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewLLMTargetRepo 创建 LLMTargetRepo
func NewLLMTargetRepo(db *gorm.DB, logger *zap.Logger) *LLMTargetRepo {
	return &LLMTargetRepo{
		db:     db,
		logger: logger.Named("llmtarget_repo"),
	}
}

// Create 创建新的 LLM target
func (r *LLMTargetRepo) Create(target *LLMTarget) error {
	if err := r.db.Create(target).Error; err != nil {
		r.logger.Error("failed to create llm target",
			zap.String("url", target.URL),
			zap.Error(err))
		return fmt.Errorf("create llm target: %w", err)
	}

	r.logger.Info("llm target created",
		zap.String("id", target.ID),
		zap.String("url", target.URL),
		zap.String("source", target.Source))

	return nil
}
```

**Step 4: 运行测试确认通过**

```bash
go test ./internal/db -run TestLLMTargetRepo_Create -v
```

Expected: PASS

**Step 5: 提交**

```bash
git add internal/db/llmtarget_repo.go internal/db/llmtarget_repo_test.go
git commit -m "feat(db): implement LLMTargetRepo Create method with tests"
```

---

### Task 1.4: 实现 LLMTargetRepo - 查询方法

**Files:**
- Modify: `internal/db/llmtarget_repo.go`
- Modify: `internal/db/llmtarget_repo_test.go`

**Step 1: 编写查询方法的测试**

在 `llmtarget_repo_test.go` 中添加:

```go
func TestLLMTargetRepo_GetByURL(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	repo := NewLLMTargetRepo(gormDB, logger)

	target := &LLMTarget{
		ID:  uuid.NewString(),
		URL: "http://test.local:8080",
		Provider: "anthropic",
		Source: "database",
	}
	err = repo.Create(target)
	require.NoError(t, err)

	// 查询
	found, err := repo.GetByURL(target.URL)
	require.NoError(t, err)
	assert.Equal(t, target.ID, found.ID)
	assert.Equal(t, target.URL, found.URL)
}

func TestLLMTargetRepo_GetByURL_NotFound(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	repo := NewLLMTargetRepo(gormDB, logger)

	_, err = repo.GetByURL("http://nonexistent.local")
	assert.Error(t, err)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestLLMTargetRepo_ListAll(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	repo := NewLLMTargetRepo(gormDB, logger)

	// 创建多个 targets
	targets := []*LLMTarget{
		{ID: uuid.NewString(), URL: "http://test1.local", Source: "config"},
		{ID: uuid.NewString(), URL: "http://test2.local", Source: "database"},
		{ID: uuid.NewString(), URL: "http://test3.local", Source: "database", IsActive: false},
	}
	for _, t := range targets {
		err := repo.Create(t)
		require.NoError(t, err)
	}

	// 查询所有
	all, err := repo.ListAll()
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestLLMTargetRepo_URLExists(t *testing.T) {
	logger := zap.NewNop()
	gormDB, err := Open(logger, ":memory:")
	require.NoError(t, err)
	defer closeGormDB(logger, gormDB)

	repo := NewLLMTargetRepo(gormDB, logger)

	target := &LLMTarget{
		ID:  uuid.NewString(),
		URL: "http://test.local:8080",
		Source: "database",
	}
	err = repo.Create(target)
	require.NoError(t, err)

	// 检查存在
	exists, err := repo.URLExists(target.URL)
	require.NoError(t, err)
	assert.True(t, exists)

	// 检查不存在
	exists, err = repo.URLExists("http://nonexistent.local")
	require.NoError(t, err)
	assert.False(t, exists)
}
```

**Step 2: 运行测试确认失败**

```bash
go test ./internal/db -run "TestLLMTargetRepo_(GetByURL|ListAll|URLExists)" -v
```

Expected: FAIL (methods not defined)

**Step 3: 实现查询方法**

在 `llmtarget_repo.go` 中添加:

```go
// GetByURL 根据 URL 查询 LLM target
func (r *LLMTargetRepo) GetByURL(url string) (*LLMTarget, error) {
	var target LLMTarget
	if err := r.db.Where("url = ?", url).First(&target).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, err
		}
		r.logger.Error("failed to get llm target by url",
			zap.String("url", url),
			zap.Error(err))
		return nil, fmt.Errorf("get llm target by url: %w", err)
	}
	return &target, nil
}

// GetByID 根据 ID 查询 LLM target
func (r *LLMTargetRepo) GetByID(id string) (*LLMTarget, error) {
	var target LLMTarget
	if err := r.db.Where("id = ?", id).First(&target).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, err
		}
		r.logger.Error("failed to get llm target by id",
			zap.String("id", id),
			zap.Error(err))
		return nil, fmt.Errorf("get llm target by id: %w", err)
	}
	return &target, nil
}

// ListAll 列出所有 LLM targets
func (r *LLMTargetRepo) ListAll() ([]*LLMTarget, error) {
	var targets []*LLMTarget
	if err := r.db.Order("created_at DESC").Find(&targets).Error; err != nil {
		r.logger.Error("failed to list llm targets", zap.Error(err))
		return nil, fmt.Errorf("list llm targets: %w", err)
	}

	r.logger.Debug("listed llm targets", zap.Int("count", len(targets)))
	return targets, nil
}

// URLExists 检查 URL 是否已存在
func (r *LLMTargetRepo) URLExists(url string) (bool, error) {
	var count int64
	if err := r.db.Model(&LLMTarget{}).Where("url = ?", url).Count(&count).Error; err != nil {
		r.logger.Error("failed to check url exists",
			zap.String("url", url),
			zap.Error(err))
		return false, fmt.Errorf("check url exists: %w", err)
	}
	return count > 0, nil
}
```

**Step 4: 运行测试确认通过**

```bash
go test ./internal/db -run "TestLLMTargetRepo_(GetByURL|ListAll|URLExists)" -v
```

Expected: PASS

**Step 5: 提交**

```bash
git add internal/db/llmtarget_repo.go internal/db/llmtarget_repo_test.go
git commit -m "feat(db): implement LLMTargetRepo query methods with tests"
```

---

## Phase 3-6: 剩余实现任务概要

由于完整实现计划非常详细，以下是剩余阶段的关键任务概要。每个任务都应遵循 TDD 原则：先写测试，确认失败，实现代码，确认通过，然后提交。

### Phase 3: CLI 命令实现

**关键文件:**
- `cmd/sproxy/admin_llm_target.go` (新建)
- `cmd/sproxy/admin_llm_target_test.go` (新建)
- `cmd/sproxy/main.go` (修改，注册命令)
- `cmd/sproxy/help_ref.go` (修改，添加帮助文档)

**关键任务:**
1. 实现 `sproxy admin llm targets` 命令（列出所有 targets）
2. 实现 `sproxy admin llm target add` 命令
3. 实现 `sproxy admin llm target update` 命令
4. 实现 `sproxy admin llm target delete` 命令
5. 实现 `sproxy admin llm target enable/disable` 命令
6. 添加完整的单元测试
7. 更新 help_ref.go 添加命令文档

**测试要点:**
- URL 冲突检查
- 配置文件来源的 target 不可修改/删除
- API Key 存在性验证
- 错误消息友好性

### Phase 4: WebUI 实现

**关键文件:**
- `internal/api/admin_llm_target_handler.go` (新建)
- `internal/dashboard/templates/llm.html` (修改)
- `internal/dashboard/handler.go` (修改，注册路由)

**REST API 端点:**
```
GET    /api/admin/llm/targets          # 列出所有
POST   /api/admin/llm/targets          # 创建
GET    /api/admin/llm/targets/:id      # 获取详情
PUT    /api/admin/llm/targets/:id      # 更新
DELETE /api/admin/llm/targets/:id      # 删除
POST   /api/admin/llm/targets/:id/enable   # 启用
POST   /api/admin/llm/targets/:id/disable  # 禁用
```

**WebUI 修改:**
1. 修改 `/dashboard/llm` 页面，添加 LLM Target 管理区域
2. 添加"添加 Target"表单
3. 为数据库来源的 targets 添加"编辑"和"删除"按钮
4. 配置文件来源的 targets 显示为只读
5. 添加 URL 冲突检查的前端验证

### Phase 5: 文档更新

**需要更新的文档:**

1. **CLAUDE.md** - 添加以下内容:
```markdown
## LLM Target 动态管理

### 理念
- 配置文件：运维团队管理核心端点（基础设施即代码）
- 数据库：业务团队动态添加临时端点（热更新）
- 数据库包含所有 targets（配置文件同步的 + 动态添加的）

### CLI 命令
# 查看所有 targets
./sproxy admin llm targets

# 添加 target
./sproxy admin llm target add \
  --url <url> \
  --provider <anthropic|openai|ollama> \
  --api-key-id <key-id> \
  [--name <name>] \
  [--weight <n>]

# 更新 target（仅数据库来源的）
./sproxy admin llm target update <url> \
  [--provider <provider>] \
  [--name <name>] \
  [--weight <n>]

# 删除 target（仅数据库来源的）
./sproxy admin llm target delete <url>
```

2. **docs/manual.md** - 新增完整章节:
```markdown
## 8. LLM Target 动态管理

### 8.1 理念说明
[详细说明配置文件 vs 数据库的设计理念]

### 8.2 配置文件管理（运维团队）
[编辑 YAML、Git 版本控制、重启同步的完整流程]

### 8.3 数据库管理（业务团队）
[CLI 命令和 WebUI 操作的完整指南]

### 8.4 常见场景
[添加核心端点、添加临时端点、修改配置、删除端点、URL 冲突处理]

### 8.5 故障排查
[常见错误和解决方案]
```

3. **README.md** - 更新功能列表:
```markdown
- 动态 LLM Target 管理（配置文件 + 数据库双来源）
```

### Phase 6: E2E 测试

**测试文件:**
- `test/e2e/llm_target_management_e2e_test.go` (新建)

**测试场景:**
1. 启动时配置文件自动同步到数据库
2. CLI 添加数据库 target
3. CLI 修改数据库 target
4. CLI 删除数据库 target
5. CLI 尝试修改配置文件来源的 target（应失败）
6. URL 冲突检查
7. 配置文件修改后重启，数据库自动更新
8. WebUI 添加/修改/删除 target

---

## 实现顺序建议

1. **Phase 1 (数据库层)** - 完全实现并测试
2. **Phase 2 (同步机制)** - 完全实现并测试
3. **Phase 3 (CLI 命令)** - 完全实现并测试
4. **Phase 4 (WebUI)** - 完全实现并测试
5. **Phase 5 (文档)** - 完整更新所有文档
6. **Phase 6 (E2E 测试)** - 完整的端到端测试

每个 Phase 完成后提交一次，确保可以逐步回滚。

---

## 日志规范

所有实现都必须包含完备的日志：

```go
// 同步开始
logger.Info("syncing config targets to database", zap.Int("count", len(configTargets)))

// 单个操作
logger.Debug("config target synced", zap.String("url", ct.URL), zap.String("action", "upsert"))

// 操作完成
logger.Info("config targets sync completed", zap.Int("synced", len(configURLs)), zap.Int("deleted", deleted))

// 错误
logger.Error("failed to sync config target", zap.String("url", ct.URL), zap.Error(err))

// CLI 操作
logger.Info("llm target created", zap.String("url", target.URL), zap.String("source", "database"), zap.String("operator", "admin"))
```

---

## 测试覆盖率要求

- 单元测试覆盖率：≥ 85%
- E2E 测试覆盖率：≥ 70%
- 关键路径（同步、CRUD）覆盖率：100%

运行测试：
```bash
# 单元测试
go test ./internal/db -v -cover
go test ./cmd/sproxy -v -cover

# E2E 测试
go test ./test/e2e -v

# 覆盖率报告
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out
```

---

## 验收标准

- [ ] 配置文件中的 targets 启动时自动同步到数据库
- [ ] CLI 命令可以添加/修改/删除数据库中的 targets
- [ ] WebUI 可以添加/修改/删除数据库中的 targets
- [ ] 配置文件来源的 targets 不可通过 CLI/WebUI 修改
- [ ] URL 冲突检查正常工作
- [ ] 所有单元测试通过，覆盖率 ≥ 85%
- [ ] 所有 E2E 测试通过
- [ ] 文档完整更新（CLAUDE.md, manual.md, help_ref.go, README.md）
- [ ] 日志完备，便于定位问题
- [ ] 审计日志记录所有 target 管理操作

---

**Co-Authored-By**: Claude Sonnet 4.6 <noreply@anthropic.com>
