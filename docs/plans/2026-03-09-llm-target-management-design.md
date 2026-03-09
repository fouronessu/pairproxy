# LLM Target 动态管理功能设计文档

**日期**: 2026-03-09
**作者**: Claude Sonnet 4.6
**状态**: Approved

## 1. 概述

### 1.1 背景

当前 PairProxy 系统中，LLM targets 只能通过 `sproxy.yaml` 配置文件定义，无法动态添加或修改。这导致：
- 每次添加新端点都需要重启服务
- 无法热更新配置
- 临时测试端点管理困难
- 多级网关场景下，无法灵活调整端点配置

### 1.2 目标

实现动态 LLM target 管理功能，支持：
- CLI 命令动态添加/修改/删除 LLM targets
- WebUI 界面管理 LLM targets
- 配置文件与数据库双来源管理
- 热更新，无需重启服务

### 1.3 核心原则

**配置文件只读，数据库可写**：
- **配置文件**：运维团队管理核心端点（基础设施即代码）
- **数据库**：业务团队动态添加临时端点（热更新）
- **数据库包含所有 targets**：配置文件同步的 + 动态添加的
- **URL 唯一性约束**：不能冲突

## 2. 架构设计

### 2.1 数据流

```
配置文件 (sproxy.yaml)          数据库 (llm_targets 表)
    ↓                               ↓
启动时同步 →                    CLI/WebUI 管理
    ↓                               ↓
    └────────── 合并 ──────────────┘
                 ↓
         运行时 LLM Targets
         (SProxy.targets[])
```

### 2.2 来源标记

每个 LLM target 都有 `source` 字段标记来源：
- `source='config'`: 来自配置文件，只读，不可通过 CLI/WebUI 修改
- `source='database'`: 来自数据库，可通过 CLI/WebUI 管理

### 2.3 同步机制

**启动时同步**：
1. 从配置文件加载 targets
2. 将配置文件中的 targets 同步到数据库（UPSERT）
3. 删除数据库中 `source='config'` 但不在配置文件中的记录
4. 保留数据库中 `source='database'` 的记录

**运行时加载**：
- 从数据库加载所有 targets（包括 `source='config'` 和 `source='database'`）
- 按 URL 去重（理论上不会重复，因为有唯一性约束）

## 3. 数据库设计

### 3.1 新增表：llm_targets

```sql
CREATE TABLE llm_targets (
    id                TEXT PRIMARY KEY,
    url               TEXT NOT NULL UNIQUE,
    api_key_id        TEXT,  -- 外键 → api_keys.id
    provider          TEXT DEFAULT 'anthropic',
    name              TEXT,
    weight            INTEGER DEFAULT 1,
    health_check_path TEXT,
    source            TEXT DEFAULT 'database',  -- 'config' | 'database'
    is_editable       BOOLEAN DEFAULT TRUE,     -- false for config-sourced
    is_active         BOOLEAN DEFAULT TRUE,
    created_at        TIMESTAMP,
    updated_at        TIMESTAMP,
    FOREIGN KEY (api_key_id) REFERENCES api_keys(id)
);

CREATE INDEX idx_llm_targets_source ON llm_targets(source);
CREATE INDEX idx_llm_targets_is_active ON llm_targets(is_active);
```

### 3.2 字段说明

| 字段 | 类型 | 说明 |
|-----|------|------|
| `id` | TEXT | 主键，UUID |
| `url` | TEXT | LLM 端点 URL，唯一 |
| `api_key_id` | TEXT | 外键关联到 `api_keys` 表 |
| `provider` | TEXT | 协议类型：anthropic/openai/ollama |
| `name` | TEXT | 显示名称 |
| `weight` | INTEGER | 负载均衡权重 |
| `health_check_path` | TEXT | 健康检查路径 |
| `source` | TEXT | 来源：config/database |
| `is_editable` | BOOLEAN | 是否可编辑（config 来源为 false） |
| `is_active` | BOOLEAN | 是否启用 |
| `created_at` | TIMESTAMP | 创建时间 |
| `updated_at` | TIMESTAMP | 更新时间 |

### 3.3 API Key 管理

复用现有 `api_keys` 表：
- `llm_targets.api_key_id` 外键关联到 `api_keys.id`
- 统一的加密存储（AES-256-GCM）
- 支持密钥轮换

## 4. CLI 命令设计

### 4.1 命令列表

```bash
# 查看所有 targets
sproxy admin llm targets
sproxy admin llm targets --format json

# 添加 target（仅数据库）
sproxy admin llm target add \
  --url <url> \
  --provider <anthropic|openai|ollama> \
  --api-key-id <key-id> \
  [--name <name>] \
  [--weight <n>] \
  [--health-check-path <path>]

# 更新 target（仅数据库来源的）
sproxy admin llm target update <url> \
  [--provider <provider>] \
  [--api-key-id <key-id>] \
  [--name <name>] \
  [--weight <n>] \
  [--health-check-path <path>]

# 删除 target（仅数据库来源的）
sproxy admin llm target delete <url>

# 启用/禁用 target
sproxy admin llm target enable <url>
sproxy admin llm target disable <url>
```

### 4.2 错误处理

| 场景 | 错误信息 |
|-----|---------|
| URL 冲突 | "URL already exists in config file or database" |
| 修改配置文件来源的 target | "Cannot modify config-sourced target, edit sproxy.yaml instead" |
| 删除配置文件来源的 target | "Cannot delete config-sourced target, edit sproxy.yaml instead" |
| API Key 不存在 | "API Key not found: <key-id>" |

## 5. WebUI 设计

### 5.1 页面布局

`/dashboard/llm` 页面包含：
1. **LLM 目标列表**：显示所有 targets（配置文件 + 数据库）
2. **添加 Target 表单**：只能添加到数据库

### 5.2 列表显示

| 列 | 说明 |
|---|------|
| 名称/URL | 显示名称和 URL |
| Provider | 协议类型 |
| 权重 | 负载均衡权重 |
| 来源 | [配置文件] 或 [数据库] |
| 健康状态 | ✓ 健康 / ✗ 不健康 |
| 绑定数 | 用户/分组绑定数量 |
| 操作 | 配置文件来源：[查看详情]<br>数据库来源：[编辑] [删除] |

### 5.3 添加表单

字段：
- URL（必填）
- Provider（下拉选择：anthropic/openai/ollama）
- API Key（下拉选择已有 Key 或创建新 Key）
- 名称（可选）
- 权重（默认 1）
- 健康检查路径（可选）

验证：
- URL 格式验证
- URL 唯一性检查（包括配置文件来源的）
- API Key 存在性检查

## 6. API 接口设计

### 6.1 REST API

```
GET    /api/admin/llm/targets          # 列出所有 targets
POST   /api/admin/llm/targets          # 创建 target（仅数据库）
GET    /api/admin/llm/targets/:id      # 获取 target 详情
PUT    /api/admin/llm/targets/:id      # 更新 target（仅数据库来源的）
DELETE /api/admin/llm/targets/:id      # 删除 target（仅数据库来源的）
POST   /api/admin/llm/targets/:id/enable   # 启用 target
POST   /api/admin/llm/targets/:id/disable  # 禁用 target
```

### 6.2 请求/响应示例

**创建 Target**：
```json
POST /api/admin/llm/targets
{
  "url": "http://test-ollama.local:11434",
  "provider": "ollama",
  "api_key_id": "key-abc123",
  "name": "测试 Ollama",
  "weight": 1
}

Response 201:
{
  "id": "target-xyz789",
  "url": "http://test-ollama.local:11434",
  "provider": "ollama",
  "name": "测试 Ollama",
  "weight": 1,
  "source": "database",
  "is_editable": true,
  "is_active": true,
  "created_at": "2026-03-09T10:30:00Z"
}
```

## 7. 同步机制实现

### 7.1 启动时同步

```go
func (sp *SProxy) syncConfigTargetsToDatabase(repo *db.LLMTargetRepo) error {
    logger := sp.logger.Named("sync")

    // 1. 加载配置文件中的 targets
    configTargets := sp.loadConfigTargets()
    logger.Info("syncing config targets to database",
        zap.Int("count", len(configTargets)))

    // 2. 同步到数据库
    configURLs := make([]string, 0, len(configTargets))
    for _, ct := range configTargets {
        // 解析 API Key ID（从 api_keys 表查找或创建）
        apiKeyID, err := sp.resolveAPIKeyID(ct.APIKey, ct.Provider)
        if err != nil {
            logger.Warn("failed to resolve api key",
                zap.String("url", ct.URL),
                zap.Error(err))
            continue
        }

        // UPSERT
        target := &db.LLMTarget{
            URL:             ct.URL,
            APIKeyID:        apiKeyID,
            Provider:        ct.Provider,
            Name:            ct.Name,
            Weight:          ct.Weight,
            HealthCheckPath: ct.HealthCheckPath,
            Source:          "config",
            IsEditable:      false,
            IsActive:        true,
        }

        err = repo.Upsert(target)
        if err != nil {
            logger.Error("failed to sync config target",
                zap.String("url", ct.URL),
                zap.Error(err))
            continue
        }

        configURLs = append(configURLs, ct.URL)
        logger.Debug("config target synced",
            zap.String("url", ct.URL))
    }

    // 3. 清理：删除数据库中 source='config' 但不在配置文件中的记录
    deleted, err := repo.DeleteConfigTargetsNotInList(configURLs)
    if err != nil {
        logger.Error("failed to clean up config targets", zap.Error(err))
    } else if deleted > 0 {
        logger.Info("cleaned up removed config targets", zap.Int("count", deleted))
    }

    logger.Info("config targets sync completed",
        zap.Int("synced", len(configURLs)),
        zap.Int("deleted", deleted))

    return nil
}
```

### 7.2 运行时加载

```go
func (sp *SProxy) loadAllTargets(repo *db.LLMTargetRepo) ([]proxy.LLMTarget, error) {
    // 从数据库加载所有 targets（包括 config 和 database 来源的）
    dbTargets, err := repo.ListAll()
    if err != nil {
        return nil, err
    }

    targets := make([]proxy.LLMTarget, 0, len(dbTargets))
    for _, dt := range dbTargets {
        if !dt.IsActive {
            continue  // 跳过禁用的 targets
        }

        // 解密 API Key
        apiKey, err := sp.apiKeyResolver(dt.APIKeyID)
        if err != nil {
            sp.logger.Warn("failed to resolve api key for target",
                zap.String("url", dt.URL),
                zap.String("api_key_id", dt.APIKeyID))
            continue
        }

        targets = append(targets, proxy.LLMTarget{
            URL:             dt.URL,
            APIKey:          apiKey,
            Provider:        dt.Provider,
            Name:            dt.Name,
            Weight:          dt.Weight,
            HealthCheckPath: dt.HealthCheckPath,
        })
    }

    sp.logger.Info("loaded LLM targets",
        zap.Int("total", len(targets)),
        zap.Int("config", countBySource(dbTargets, "config")),
        zap.Int("database", countBySource(dbTargets, "database")))

    return targets, nil
}
```

## 8. 测试策略

### 8.1 单元测试

**数据库层**（`internal/db/llmtarget_repo_test.go`）：
- `TestLLMTargetRepo_Create`
- `TestLLMTargetRepo_Upsert`
- `TestLLMTargetRepo_GetByURL`
- `TestLLMTargetRepo_ListAll`
- `TestLLMTargetRepo_Update`
- `TestLLMTargetRepo_Delete`
- `TestLLMTargetRepo_DeleteConfigTargetsNotInList`
- `TestLLMTargetRepo_URLExists`

**CLI 命令**（`cmd/sproxy/admin_llm_target_test.go`）：
- `TestAdminLLMTargetAdd`
- `TestAdminLLMTargetUpdate`
- `TestAdminLLMTargetDelete`
- `TestAdminLLMTargetList`
- `TestAdminLLMTarget_CannotModifyConfigSource`

**同步机制**（`internal/proxy/sproxy_sync_test.go`）：
- `TestSyncConfigTargetsToDatabase`
- `TestSyncConfigTargets_Upsert`
- `TestSyncConfigTargets_Delete`
- `TestLoadAllTargets`

### 8.2 E2E 测试

**场景测试**（`test/e2e/llm_target_management_e2e_test.go`）：
- 启动时配置文件同步
- CLI 添加/修改/删除数据库 target
- WebUI 添加/修改/删除数据库 target
- URL 冲突检查
- 配置文件来源的 target 不可修改
- 配置文件修改后重启同步

### 8.3 测试覆盖率目标

- 单元测试覆盖率：≥ 85%
- E2E 测试覆盖率：≥ 70%
- 关键路径覆盖率：100%

## 9. 日志规范

### 9.1 日志级别

| 级别 | 场景 |
|-----|------|
| DEBUG | 详细的同步过程、每个 target 的处理 |
| INFO | 同步开始/完成、target 创建/更新/删除 |
| WARN | API Key 解析失败、target 同步失败（非致命） |
| ERROR | 数据库操作失败、同步失败（致命） |

### 9.2 日志示例

```go
// 同步开始
logger.Info("syncing config targets to database",
    zap.Int("count", len(configTargets)))

// 单个 target 同步
logger.Debug("config target synced",
    zap.String("url", ct.URL),
    zap.String("action", "upsert"))

// 清理
logger.Info("cleaned up removed config targets",
    zap.Int("count", deleted))

// 同步完成
logger.Info("config targets sync completed",
    zap.Int("synced", len(configURLs)),
    zap.Int("deleted", deleted))

// CLI 操作
logger.Info("llm target created",
    zap.String("url", target.URL),
    zap.String("source", "database"),
    zap.String("operator", "admin"))

// 错误
logger.Error("failed to sync config target",
    zap.String("url", ct.URL),
    zap.Error(err))
```

## 10. 文档更新

### 10.1 需要更新的文档

1. **CLAUDE.md**：
   - 添加 LLM target 管理命令说明
   - 更新配置文件与数据库双来源的理念

2. **docs/manual.md**：
   - 新增"LLM Target 动态管理"章节
   - 理念说明：配置文件 vs 数据库
   - 实操指南：CLI 命令 + WebUI 操作
   - 故障处理：URL 冲突、权限错误

3. **cmd/sproxy/help_ref.go**：
   - 添加 `llm target` 命令的完整说明

4. **README.md**：
   - 更新功能列表

### 10.2 文档结构

**manual.md 新增章节**：
```markdown
## 8. LLM Target 动态管理

### 8.1 理念说明
- 配置文件：运维团队管理核心端点
- 数据库：业务团队动态添加临时端点
- 数据库包含所有 targets

### 8.2 配置文件管理（运维团队）
- 编辑 sproxy.yaml
- Git 版本控制
- 重启服务同步

### 8.3 数据库管理（业务团队）
- CLI 命令
- WebUI 操作
- 无需重启

### 8.4 常见场景
- 添加核心端点
- 添加临时测试端点
- 修改端点配置
- 删除端点
- URL 冲突处理
```

## 11. 实现计划

### 11.1 阶段划分

**Phase 1: 数据库层**
- 新增 `llm_targets` 表
- 实现 `LLMTargetRepo`
- 单元测试

**Phase 2: 同步机制**
- 实现配置文件同步逻辑
- 实现运行时加载逻辑
- 单元测试

**Phase 3: CLI 命令**
- 实现 `sproxy admin llm target` 命令
- 单元测试

**Phase 4: WebUI**
- 实现 REST API
- 实现前端页面
- E2E 测试

**Phase 5: 文档更新**
- 更新 CLAUDE.md
- 更新 manual.md
- 更新 help_ref.go

**Phase 6: 集成测试**
- E2E 测试
- 手动测试
- 性能测试

### 11.2 预计工作量

- Phase 1: 4 小时
- Phase 2: 4 小时
- Phase 3: 3 小时
- Phase 4: 5 小时
- Phase 5: 3 小时
- Phase 6: 3 小时

**总计**: 约 22 小时

## 12. 风险和缓解

### 12.1 风险

| 风险 | 影响 | 缓解措施 |
|-----|------|---------|
| 配置文件同步失败 | 启动失败 | 详细日志 + 回滚机制 |
| URL 冲突 | 创建失败 | 唯一性约束 + 友好错误提示 |
| API Key 解析失败 | target 不可用 | 跳过该 target + WARN 日志 |
| 数据库迁移失败 | 启动失败 | 迁移脚本测试 + 回滚脚本 |

### 12.2 回滚计划

如果新功能出现问题：
1. 回滚代码到上一个版本
2. 数据库中的 `llm_targets` 表不影响旧版本（旧版本忽略该表）
3. 配置文件中的 targets 仍然有效

## 13. 验收标准

- [ ] 配置文件中的 targets 启动时自动同步到数据库
- [ ] CLI 命令可以添加/修改/删除数据库中的 targets
- [ ] WebUI 可以添加/修改/删除数据库中的 targets
- [ ] 配置文件来源的 targets 不可通过 CLI/WebUI 修改
- [ ] URL 冲突检查正常工作
- [ ] 所有单元测试通过，覆盖率 ≥ 85%
- [ ] 所有 E2E 测试通过
- [ ] 文档完整更新
- [ ] 日志完备，便于定位问题

---

**Co-Authored-By**: Claude Sonnet 4.6 <noreply@anthropic.com>
