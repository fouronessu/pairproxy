# PRD: Dashboard 用户用量统计功能

## 文档信息

| 项目 | 内容 |
|------|------|
| 标题 | Dashboard 用户 Token 用量统计功能 |
| 版本 | 1.0.0 |
| 作者 | PairProxy Team |
| 状态 | 待评审 |
| 日期 | 2026-03-07 |

---

## 1. 背景与问题描述

### 1.1 当前问题

管理员在 Web UI 的"用户管理"页面中，无法看到每个用户的 Token 用量统计信息。当前页面仅显示用户基本信息（用户名、分组、状态、最后登录时间），缺少用量数据。

### 1.2 业务需求

管理员需要：
1. 直观查看每个用户的累计 Token 用量
2. 按用量排序，识别高用量用户
3. 了解用户的平均使用模式（日/月平均）
4. 支持历史累计数据统计

---

## 2. 目标

### 2.1 主要目标

在现有 Dashboard 的"用户管理"页面添加完整的 Token 用量统计功能，支持排序和历史数据分析。

### 2.2 成功指标

- 用户列表显示累计用量、平均日用量、平均月用量
- 支持按用量升序/降序排序
- 数据统计范围覆盖所有历史数据
- 页面加载时间 < 2 秒（1000 用户以内）

---

## 3. 需求详细说明

### 3.1 功能需求

#### FR-1: 用户列表展示用量统计

**描述**: 在用户管理页面表格中新增用量统计列

**优先级**: P0 (必须)

**详细说明**:

新增展示列：

| 列名 | 说明 | 数据单位 |
|------|------|----------|
| 累计输入 Tokens | 用户历史累计输入 Token 数 | 智能格式化 (K/M) |
| 累计输出 Tokens | 用户历史累计输出 Token 数 | 智能格式化 (K/M) |
| 累计总 Tokens | 输入 + 输出 | 智能格式化 (K/M) |
| 平均日用量 | 总 Tokens / 使用天数 | 智能格式化 |
| 平均月用量 | 总 Tokens / 使用月数 | 智能格式化 |
| 首次使用 | 该用户第一条用量记录时间 | YYYY-MM-DD |
| 最后使用 | 该用户最近一条用量记录时间 | YYYY-MM-DD |

#### FR-2: 用量排序功能

**描述**: 支持按 Token 用量升序/降序排列用户列表

**优先级**: P0 (必须)

**详细说明**:
- 点击表头可进行排序
- 支持排序的列：累计输入、累计输出、累计总 Tokens、平均日用量、平均月用量
- 排序状态持久化（刷新页面保持）
- 支持组合排序（主排序 + 次排序）

**排序图标**：
- ↑ 升序 (Ascending)
- ↓ 降序 (Descending)
- ⇅ 可排序（默认状态）

#### FR-3: 历史累计统计

**描述**: 统计范围覆盖数据库中所有历史用量数据

**优先级**: P1 (重要)

**详细说明**:
- 不限于特定时间范围
- 统计该用户所有时间段的用量记录
- 计算使用天数：从首次使用到当天的自然日数
- 计算使用月数：从首次使用到当天的自然月数（向下取整）

#### FR-4: 用量格式化显示

**描述**: 智能格式化大数字显示

**优先级**: P1 (重要)

**详细说明**:
```
< 1,000: 原样显示 (e.g., 999)
1,000 - 999,999: 显示为 K (e.g., 12.5K)
1,000,000 - 999,999,999: 显示为 M (e.g., 5.2M)
≥ 1,000,000,000: 显示为 B (e.g., 1.5B)
```

**Tooltip**: 鼠标悬停显示精确数字

---

### 3.2 非功能需求

| 需求 | 说明 | 优先级 |
|------|------|--------|
| 性能 | 1000 用户数据查询 < 2 秒 | P0 |
| 缓存 | 用量统计缓存 5 分钟 | P1 |
| 响应式 | 表格支持横向滚动 | P1 |
| 可访问性 | 支持键盘导航 | P2 |

---

## 4. 技术方案

### 4.1 架构图

```
┌─────────────────────────────────────────────────────────────────┐
│                     Dashboard 用户管理页面                        │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  用户列表表格                                                │  │
│  │  ┌────────┬──────┬──────────┬──────────┬──────────┬──────┐ │  │
│  │  │用户名  │分组  │累计输入  │累计输出  │平均日用  │...   │ │  │
│  │  │       │     │    ↑↓   │    ↑↓   │    ↑↓   │     │ │  │
│  │  └────────┴──────┴──────────┴──────────┴──────────┴──────┘ │  │
│  │                    ↑ 点击排序                               │  │
│  └──────────────────────────────────────────────────────────┘  │
│                           │                                     │
│                           ▼                                     │
│                  GET /dashboard/api/user-stats                  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                       Dashboard Handler                         │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  handleUserStats()                                       │  │
│  │  1. 检查缓存                                             │  │
│  │  2. 调用 UsageRepo.GetUserAllTimeStats()                 │  │
│  │  3. 格式化数据 + 计算平均值                               │  │
│  │  4. 写入缓存                                             │  │
│  │  5. 返回 JSON                                            │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                        UsageRepo (DB)                           │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  GetUserAllTimeStats()                                   │  │
│  │  SQL: 按 user_id 聚合所有用量记录                         │  │
│  │  返回: user_id, total_input, total_output,               │  │
│  │         first_used_at, last_used_at                      │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### 4.2 数据库查询设计

#### 新增方法: `GetUserAllTimeStats`

**文件**: `internal/db/usage_repo.go`

```go
// UserAllTimeStat 用户历史累计统计
type UserAllTimeStat struct {
    UserID        string
    TotalInput    int64
    TotalOutput   int64
    TotalTokens   int64
    FirstUsedAt   time.Time
    LastUsedAt    time.Time
    DaysActive    int  // 使用天数
    MonthsActive  int  // 使用月数
}

// GetUserAllTimeStats 获取所有用户的历史累计用量统计
// 按用户聚合所有时间段的用量数据
func (r *UsageRepo) GetUserAllTimeStats() ([]UserAllTimeStat, error) {
    var stats []UserAllTimeStat
    
    err := r.db.Model(&UsageLog{}).
        Select(`user_id,
            COALESCE(SUM(input_tokens), 0) as total_input,
            COALESCE(SUM(output_tokens), 0) as total_output,
            COALESCE(SUM(input_tokens + output_tokens), 0) as total_tokens,
            MIN(created_at) as first_used_at,
            MAX(created_at) as last_used_at,
            COUNT(DISTINCT DATE(created_at)) as days_active,
            (julianday('now') - julianday(MIN(created_at))) / 30 as months_active`).
        Group("user_id").
        Scan(&stats).Error
    
    if err != nil {
        return nil, fmt.Errorf("get user all time stats: %w", err)
    }
    
    return stats, nil
}
```

**SQL 说明**：
- `SUM(input_tokens/output_tokens)`：累计用量
- `MIN(created_at)`：首次使用时间
- `MAX(created_at)`：最后使用时间
- `COUNT(DISTINCT DATE(created_at))`：实际使用天数
- `julianday('now') - julianday(MIN(created_at))`：从首次到现在总天数

### 4.3 后端 API 设计

#### 新增 Endpoint

**文件**: `internal/dashboard/handler.go`

```go
// 注册路由
mux.Handle("GET /dashboard/api/user-stats", requireAdmin(http.HandlerFunc(h.handleUserStats)))
```

**Handler 实现**:

```go
// userStatsResponse 用户用量统计响应
type userStatsResponse struct {
    UserID        string `json:"user_id"`
    Username      string `json:"username"`
    GroupName     string `json:"group_name"`
    TotalInput    int64  `json:"total_input"`
    TotalOutput   int64  `json:"total_output"`
    TotalTokens   int64  `json:"total_tokens"`
    AvgDaily      int64  `json:"avg_daily"`      // 平均日用量
    AvgMonthly    int64  `json:"avg_monthly"`    // 平均月用量
    DaysActive    int    `json:"days_active"`    // 使用天数
    MonthsActive  int    `json:"months_active"`  // 使用月数
    FirstUsedAt   string `json:"first_used_at"`
    LastUsedAt    string `json:"last_used_at"`
    IsActive      bool   `json:"is_active"`
}

// handleUserStats 处理用户用量统计请求
func (h *DashboardHandler) handleUserStats(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    
    // 尝试从缓存获取
    cacheKey := "dashboard:user_stats"
    if cached, ok := h.cache.Get(cacheKey); ok {
        writeJSON(w, http.StatusOK, cached)
        return
    }
    
    // 获取用量统计
    usageStats, err := h.usageRepo.GetUserAllTimeStats()
    if err != nil {
        h.logger.Error("failed to get user stats", zap.Error(err))
        writeJSONError(w, http.StatusInternalServerError, "获取用量统计失败")
        return
    }
    
    // 获取所有用户信息
    users, err := h.userRepo.ListAll()
    if err != nil {
        h.logger.Error("failed to list users", zap.Error(err))
        writeJSONError(w, http.StatusInternalServerError, "获取用户列表失败")
        return
    }
    
    // 构建 user_id -> username 映射
    userMap := make(map[string]*db.User)
    for i := range users {
        userMap[users[i].ID] = &users[i]
    }
    
    // 合并数据
    resp := make([]userStatsResponse, 0, len(users))
    
    for _, stat := range usageStats {
        user, ok := userMap[stat.UserID]
        if !ok {
            continue // 跳过已删除用户
        }
        
        // 计算平均值
        var avgDaily, avgMonthly int64
        if stat.DaysActive > 0 {
            avgDaily = stat.TotalTokens / int64(stat.DaysActive)
        }
        if stat.MonthsActive > 0 {
            avgMonthly = stat.TotalTokens / int64(stat.MonthsActive)
        }
        
        resp = append(resp, userStatsResponse{
            UserID:       stat.UserID,
            Username:     user.Username,
            GroupName:    getGroupName(user),
            TotalInput:   stat.TotalInput,
            TotalOutput:  stat.TotalOutput,
            TotalTokens:  stat.TotalTokens,
            AvgDaily:     avgDaily,
            AvgMonthly:   avgMonthly,
            DaysActive:   stat.DaysActive,
            MonthsActive: stat.MonthsActive,
            FirstUsedAt:  stat.FirstUsedAt.Format("2006-01-02"),
            LastUsedAt:   stat.LastUsedAt.Format("2006-01-02"),
            IsActive:     user.IsActive,
        })
    }
    
    // 写入缓存（5分钟）
    h.cache.Set(cacheKey, resp, 5*time.Minute)
    
    writeJSON(w, http.StatusOK, resp)
}
```

### 4.4 前端实现

#### 修改文件: `internal/dashboard/templates/users.html`

**新增样式**:
```html
<style>
    .sortable {
        cursor: pointer;
        user-select: none;
    }
    .sortable:hover {
        background-color: #f3f4f6;
    }
    .sortable::after {
        content: " ⇅";
        color: #9ca3af;
        font-size: 12px;
    }
    .sortable.asc::after {
        content: " ↑";
        color: #4f46e5;
    }
    .sortable.desc::after {
        content: " ↓";
        color: #4f46e5;
    }
    .token-count {
        font-family: 'Courier New', monospace;
        font-weight: 600;
    }
    .token-input {
        color: #3b82f6;
    }
    .token-output {
        color: #10b981;
    }
    .token-total {
        color: #7c3aed;
        font-size: 14px;
    }
    .avg-stat {
        font-size: 12px;
        color: #6b7280;
    }
</style>
```

**修改表格头部**:
```html
<thead class="bg-gray-50 text-xs text-gray-500 uppercase">
    <tr>
        <th class="px-4 py-3 text-left">用户名</th>
        <th class="px-4 py-3 text-left">分组</th>
        <th class="px-4 py-3 text-right sortable" data-sort="total_input">
            累计输入
        </th>
        <th class="px-4 py-3 text-right sortable" data-sort="total_output">
            累计输出
        </th>
        <th class="px-4 py-3 text-right sortable" data-sort="total_tokens">
            累计总用量
        </th>
        <th class="px-4 py-3 text-right sortable" data-sort="avg_daily">
            平均日用量
        </th>
        <th class="px-4 py-3 text-right sortable" data-sort="avg_monthly">
            平均月用量
        </th>
        <th class="px-4 py-3 text-left">首次使用</th>
        <th class="px-4 py-3 text-left">最后使用</th>
        <th class="px-4 py-3 text-left">状态</th>
        <th class="px-4 py-3 text-left">操作</th>
    </tr>
</thead>
```

**新增 JavaScript**:
```html
<script>
// 格式化大数字
function formatTokens(num) {
    if (num < 1000) return num.toString();
    if (num < 1000000) return (num / 1000).toFixed(1) + 'K';
    if (num < 1000000000) return (num / 1000000).toFixed(1) + 'M';
    return (num / 1000000000).toFixed(1) + 'B';
}

// 用户统计数据
let userStats = [];
let currentSort = { field: 'total_tokens', order: 'desc' };

// 加载用户用量统计
async function loadUserStats() {
    try {
        const res = await fetch('/dashboard/api/user-stats');
        const data = await res.json();
        userStats = data;
        sortAndRender();
    } catch (err) {
        console.error('Failed to load user stats:', err);
    }
}

// 排序
function sortUsers(field, order) {
    currentSort = { field, order };
    sortAndRender();
}

function sortAndRender() {
    const { field, order } = currentSort;
    
    userStats.sort((a, b) => {
        let valA = a[field];
        let valB = b[field];
        
        if (typeof valA === 'string') {
            valA = valA.toLowerCase();
            valB = valB.toLowerCase();
        }
        
        if (order === 'asc') {
            return valA > valB ? 1 : -1;
        } else {
            return valA < valB ? 1 : -1;
        }
    });
    
    renderTable();
}

// 渲染表格
function renderTable() {
    const tbody = document.getElementById('userStatsBody');
    tbody.innerHTML = userStats.map(user => `
        <tr class="hover:bg-gray-50 ${!user.is_active ? 'opacity-50' : ''}">
            <td class="px-4 py-3 font-medium">${user.username}</td>
            <td class="px-4 py-3 text-gray-500">${user.group_name || '—'}</td>
            <td class="px-4 py-3 text-right token-count token-input" 
                title="${user.total_input.toLocaleString()}">
                ${formatTokens(user.total_input)}
            </td>
            <td class="px-4 py-3 text-right token-count token-output"
                title="${user.total_output.toLocaleString()}">
                ${formatTokens(user.total_output)}
            </td>
            <td class="px-4 py-3 text-right token-count token-total"
                title="${user.total_tokens.toLocaleString()}">
                ${formatTokens(user.total_tokens)}
            </td>
            <td class="px-4 py-3 text-right avg-stat"
                title="使用 ${user.days_active} 天">
                ${formatTokens(user.avg_daily)}/天
            </td>
            <td class="px-4 py-3 text-right avg-stat"
                title="使用 ${user.months_active} 个月">
                ${formatTokens(user.avg_monthly)}/月
            </td>
            <td class="px-4 py-3 text-gray-500">${user.first_used_at}</td>
            <td class="px-4 py-3 text-gray-500">${user.last_used_at}</td>
            <td class="px-4 py-3">
                ${user.is_active 
                    ? '<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-green-100 text-green-700">启用</span>'
                    : '<span class="inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium bg-red-100 text-red-600">禁用</span>'
                }
            </td>
            <td class="px-4 py-3">
                <!-- 原有操作按钮 -->
            </td>
        </tr>
    `).join('');
}

// 排序点击事件
document.querySelectorAll('.sortable').forEach(th => {
    th.addEventListener('click', () => {
        const field = th.dataset.sort;
        let order = 'desc';
        
        if (th.classList.contains('desc')) {
            order = 'asc';
        } else if (th.classList.contains('asc')) {
            order = 'desc';
        }
        
        // 更新表头样式
        document.querySelectorAll('.sortable').forEach(el => {
            el.classList.remove('asc', 'desc');
        });
        th.classList.add(order);
        
        sortUsers(field, order);
    });
});

// 页面加载时获取数据
loadUserStats();
</script>
```

---

## 5. 验收标准

### 5.1 功能验收

| 验收项 | 验收标准 | 验证方法 |
|--------|----------|----------|
| 用量显示 | 用户列表显示累计输入/输出/总量 | 目视检查 |
| 平均值计算 | 正确计算平均日/月用量 | 数据验证 |
| 排序功能 | 点击表头可升序/降序排序 | 功能测试 |
| 大数字格式化 | >1000 显示为 K/M/B | 目视检查 |
| Tooltip | 悬停显示精确数值 | 交互测试 |

### 5.2 性能验收

| 验收项 | 验收标准 | 验证方法 |
|--------|----------|----------|
| 查询性能 | 1000 用户 < 2 秒 | 压力测试 |
| 缓存生效 | 5 分钟内重复查询使用缓存 | 日志验证 |
| 页面加载 | 完整渲染 < 3 秒 | 性能测试 |

---

## 6. 风险分析

| 风险 | 可能性 | 影响 | 缓解措施 |
|------|--------|------|----------|
| 大数据量查询慢 | 中 | 高 | 添加数据库索引、分页加载 |
| 缓存数据不一致 | 低 | 中 | 设置合理 TTL，支持手动刷新 |
| 用户量过多 | 低 | 中 | 实现分页或虚拟滚动 |
| 浏览器兼容性 | 低 | 低 | 使用标准 ES6+，避免新特性 |

---

## 7. 实施计划

```
Phase 1: 后端开发 (1 天)
  ├─ [ ] 新增 GetUserAllTimeStats 查询方法
  ├─ [ ] 实现 /dashboard/api/user-stats API
  └─ [ ] 添加缓存机制

Phase 2: 前端开发 (1 天)
  ├─ [ ] 修改 users.html 模板
  ├─ [ ] 实现排序功能
  ├─ [ ] 实现数字格式化
  └─ [ ] 添加样式

Phase 3: 测试 (1 天)
  ├─ [ ] 单元测试
  ├─ [ ] 集成测试
  └─ [ ] 性能测试

Phase 4: Code Review & 发布 (1 天)
  ├─ [ ] PR Review
  ├─ [ ] 合并发布
  └─ [ ] 更新文档
```

---

## 8. 附录

### 8.1 相关代码文件

| 文件 | 说明 |
|------|------|
| `internal/db/usage_repo.go` | 新增 GetUserAllTimeStats 方法 |
| `internal/dashboard/handler.go` | 新增 handleUserStats handler |
| `internal/dashboard/templates/users.html` | 修改用户列表页面 |

### 8.2 数据库索引建议

```sql
-- 为用户统计查询优化
CREATE INDEX idx_usage_logs_user_created 
ON usage_logs(user_id, created_at);
```

### 8.3 变更日志

| 版本 | 日期 | 变更内容 | 作者 |
|------|------|----------|------|
| 1.0.0 | 2026-03-07 | 初始版本 | PairProxy Team |

---

**文档结束**
