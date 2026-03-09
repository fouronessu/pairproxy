# API Key 管理指南

## 一、什么是 API Key 管理

### 1.1 概念说明

**API Key** 是访问 LLM 服务商（如 Anthropic、OpenAI）的凭证。在 PairProxy 中：

- **真实 API Key** 存储在服务端数据库（加密存储）
- **用户永远看不到真实 API Key**，只使用 PairProxy 分配的 JWT Token
- **管理员负责管理** 这些真实的 API Keys

### 1.2 为什么需要管理 API Keys

```
传统方式（不安全）:
用户 → 直接使用 Anthropic API Key → Anthropic API
     ❌ API Key 泄露风险
     ❌ 无法追踪用量
     ❌ 无法限流

PairProxy 方式（安全）:
用户 → JWT Token → PairProxy → API Key → Anthropic API
     ✅ API Key 集中管理
     ✅ 精确追踪用量
     ✅ 配额限流
```

### 1.3 API Key 的作用

1. **LLM Target 使用** - 每个 LLM Target 需要关联一个 API Key
2. **多 Provider 支持** - 不同 Provider（Anthropic/OpenAI/Ollama）使用不同的 Key
3. **密钥轮换** - 定期更换 API Key 提升安全性
4. **成本隔离** - 不同团队使用不同的 API Key，便于成本核算

---

## 二、当前管理方式（CLI）

### 2.1 查看所有 API Keys

```bash
./sproxy admin apikey list
```

**输出示例**：
```
ID                                    NAME              PROVIDER    ACTIVE  CREATED
key-abc123                           Anthropic-Prod    anthropic   ✓       2026-03-01
key-def456                           OpenAI-Test       openai      ✓       2026-03-05
key-ghi789                           Ollama-Local      ollama      ✓       2026-03-08
```

### 2.2 添加新的 API Key

```bash
./sproxy admin apikey add <name> \
  --value <api-key-value> \
  --provider <anthropic|openai|ollama>
```

**示例**：
```bash
# 添加 Anthropic API Key
./sproxy admin apikey add "Anthropic-Prod" \
  --value "sk-ant-api03-xxx" \
  --provider anthropic

# 添加 OpenAI API Key
./sproxy admin apikey add "OpenAI-GPT4" \
  --value "sk-proj-xxx" \
  --provider openai

# 添加 Ollama（本地部署，可以用任意值）
./sproxy admin apikey add "Ollama-Local" \
  --value "ollama" \
  --provider ollama
```

### 2.3 分配 API Key 给用户/分组

```bash
# 分配给用户（优先级高）
./sproxy admin apikey assign <key-id> --user <username>

# 分配给分组（兜底）
./sproxy admin apikey assign <key-id> --group <groupname>
```

**示例**：
```bash
# VIP 用户使用专属 API Key
./sproxy admin apikey assign key-abc123 --user alice

# 普通用户分组使用共享 API Key
./sproxy admin apikey assign key-def456 --group default
```

### 2.4 吊销 API Key

```bash
./sproxy admin apikey revoke <key-id>
```

**注意**：吊销后，使用该 Key 的 LLM Target 将无法工作。

---

## 三、API Key 与 LLM Target 的关系

### 3.1 数据模型

```
APIKey (api_keys 表)
  ├─ ID: key-abc123
  ├─ Name: "Anthropic-Prod"
  ├─ EncryptedValue: "encrypted_sk-ant-xxx"
  ├─ Provider: "anthropic"
  └─ IsActive: true

LLMTarget (llm_targets 表)
  ├─ ID: target-xyz789
  ├─ URL: "https://api.anthropic.com"
  ├─ APIKeyID: "key-abc123"  ← 外键关联
  ├─ Provider: "anthropic"
  └─ IsActive: true
```

### 3.2 使用流程

```
1. 管理员添加 API Key
   └─> sproxy admin apikey add "Anthropic-Prod" --value "sk-ant-xxx"

2. 管理员创建 LLM Target，关联 API Key
   └─> sproxy admin llm target add \
         --url https://api.anthropic.com \
         --api-key-id key-abc123 \
         --provider anthropic

3. 用户发送请求
   └─> PairProxy 自动使用关联的 API Key 访问 LLM
```

---

## 四、缺失的 WebUI 功能

### 4.1 当前问题

❌ **只能通过 CLI 管理**，无法在 WebUI 中操作：
- 查看 API Keys 列表
- 添加新的 API Key
- 编辑 API Key（修改名称、Provider）
- 吊销 API Key
- 查看 API Key 使用情况（哪些 Target 在用）

### 4.2 需要实现的 WebUI 功能

#### 页面：`/dashboard/apikeys`

**功能列表**：

1. **API Keys 列表**
   ```
   | 名称            | Provider  | 状态  | 使用中的 Targets | 创建时间   | 操作        |
   |----------------|-----------|-------|-----------------|-----------|-------------|
   | Anthropic-Prod | anthropic | ✓ 活跃 | 3 个            | 2026-03-01| [编辑][吊销] |
   | OpenAI-Test    | openai    | ✓ 活跃 | 1 个            | 2026-03-05| [编辑][吊销] |
   | Old-Key        | anthropic | ✗ 已吊销| 0 个            | 2026-01-15| [删除]      |
   ```

2. **添加 API Key 表单**
   - 名称（必填，唯一）
   - Provider（下拉选择：anthropic/openai/ollama）
   - API Key 值（必填，加密存储）
   - 备注（可选）

3. **编辑 API Key**
   - 修改名称
   - 修改备注
   - ⚠️ 不允许修改 API Key 值（安全考虑）

4. **吊销 API Key**
   - 确认对话框："该 Key 正在被 3 个 Targets 使用，确认吊销？"
   - 吊销后自动禁用相关 Targets

5. **查看使用情况**
   - 点击"使用中的 Targets"链接
   - 显示使用该 Key 的所有 LLM Targets

### 4.3 实现优先级

**P1 高优先级** - 因为：
1. 目前添加 LLM Target 时需要先通过 CLI 创建 API Key
2. 无法在 WebUI 中查看有哪些可用的 API Keys
3. 影响 LLM Target 管理的完整性

---

## 五、使用场景示例

### 场景 1：新增 LLM 服务商

**需求**：公司购买了 OpenAI API，需要添加到系统中

**当前流程（CLI）**：
```bash
# 1. 添加 API Key
./sproxy admin apikey add "OpenAI-GPT4" \
  --value "sk-proj-xxx" \
  --provider openai

# 2. 添加 LLM Target
./sproxy admin llm target add \
  --url https://api.openai.com \
  --api-key-id <从上一步获取的 ID> \
  --provider openai \
  --name "OpenAI GPT-4"
```

**期望流程（WebUI）**：
```
1. 登录 Dashboard → API Keys 页面
2. 点击"添加 API Key"
3. 填写表单：
   - 名称：OpenAI-GPT4
   - Provider：openai
   - API Key：sk-proj-xxx
4. 保存 → 自动获得 Key ID

5. 切换到 LLM 页面
6. 点击"添加 Target"
7. 选择刚创建的 API Key（下拉列表）
8. 填写 URL 和其他信息
9. 保存
```

### 场景 2：API Key 轮换

**需求**：定期更换 Anthropic API Key（安全策略）

**当前流程（CLI）**：
```bash
# 1. 添加新 Key
./sproxy admin apikey add "Anthropic-Prod-2026Q2" \
  --value "sk-ant-new-xxx" \
  --provider anthropic

# 2. 更新所有使用旧 Key 的 Targets
./sproxy admin llm target update https://api.anthropic.com \
  --api-key-id <新 Key ID>

# 3. 吊销旧 Key
./sproxy admin apikey revoke <旧 Key ID>
```

**期望流程（WebUI）**：
```
1. API Keys 页面 → 添加新 Key
2. LLM 页面 → 批量更新 Targets 的 API Key
3. API Keys 页面 → 吊销旧 Key
```

### 场景 3：成本隔离

**需求**：研发团队和产品团队使用不同的 API Key，便于成本核算

**当前流程（CLI）**：
```bash
# 1. 创建两个 API Keys
./sproxy admin apikey add "Anthropic-Dev" --value "sk-ant-dev-xxx"
./sproxy admin apikey add "Anthropic-Prod" --value "sk-ant-prod-xxx"

# 2. 分配给不同分组
./sproxy admin apikey assign key-dev --group dev-team
./sproxy admin apikey assign key-prod --group prod-team
```

**期望流程（WebUI）**：
```
1. API Keys 页面 → 创建两个 Keys
2. 分组页面 → 为每个分组分配对应的 API Key
3. 用量统计页面 → 按 API Key 查看成本
```

---

## 六、安全考虑

### 6.1 API Key 存储

- ✅ **加密存储**：使用 AES-256-GCM 加密
- ✅ **Base64 编码**：加密后再 Base64 编码存储
- ✅ **数据库字段**：`EncryptedValue`（不是明文）

### 6.2 WebUI 显示

- ❌ **永远不显示完整 API Key**
- ✅ **显示脱敏版本**：`sk-ant-***...***xyz`（前 7 位 + 后 3 位）
- ✅ **添加时一次性输入**：保存后无法再查看

### 6.3 权限控制

- ✅ **仅管理员可访问**：需要 Dashboard 登录
- ✅ **审计日志**：所有 API Key 操作记录到 `audit_logs` 表
- ✅ **操作确认**：吊销/删除需要二次确认

---

## 七、实现建议

### 7.1 数据库层（已完成）

✅ 已有表结构：
- `api_keys` 表
- `api_key_assignments` 表
- `APIKeyRepo` 仓库类

### 7.2 REST API 层（需实现）

需要添加以下端点：

```go
GET    /api/admin/apikeys              // 列出所有 API Keys
POST   /api/admin/apikeys              // 创建新 API Key
GET    /api/admin/apikeys/:id          // 获取详情
PUT    /api/admin/apikeys/:id          // 更新（仅名称、备注）
DELETE /api/admin/apikeys/:id          // 删除（需检查是否在使用）
POST   /api/admin/apikeys/:id/revoke   // 吊销
GET    /api/admin/apikeys/:id/targets  // 查看使用该 Key 的 Targets
```

### 7.3 WebUI 层（需实现）

需要添加：
- `internal/dashboard/templates/apikeys.html` - 页面模板
- `internal/dashboard/apikey_handler.go` - 处理器
- 路由注册

### 7.4 工作量估算

- REST API 实现：1-2 天
- WebUI 页面实现：1-2 天
- 测试（单元测试 + E2E）：1 天
- **总计**：3-4 天

---

## 八、总结

### 当前状态
- ✅ CLI 功能完整（add/list/assign/revoke）
- ❌ WebUI 功能缺失
- ⚠️ 影响 LLM Target 管理的完整性

### 建议
**优先级 P1** - 建议尽快实现 WebUI 管理功能，因为：
1. 提升管理效率（无需切换到命令行）
2. 降低操作门槛（图形界面更友好）
3. 完善 LLM Target 管理流程（目前需要 CLI + WebUI 混合操作）

### 临时方案
在 WebUI 实现之前，可以：
1. 在 CLAUDE.md 中添加 API Key CLI 使用指南
2. 在 LLM 页面添加提示："如需添加新 API Key，请使用 CLI 命令"
3. 提供快速参考卡片

---

**文档版本**: v1.0
**更新日期**: 2026-03-09
