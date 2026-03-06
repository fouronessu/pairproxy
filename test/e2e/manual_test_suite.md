# 手动测试套件 - 完整测试用例

本文档提供完整的手动测试用例，用于验证 pairproxy 的所有关键功能。

---

## 前置准备

### 1. 启动服务
```bash
# 启动 mockllm
./mockllm.exe --addr :11434 &

# 启动 sproxy
./sproxy.exe start --config test-sproxy.yaml &

# 启动 cproxy
./cproxy.exe start --config test-cproxy.yaml &
```

### 2. 创建测试用户
```bash
# 创建普通用户
./sproxy.exe admin user add --config test-sproxy.yaml --password testpass123 testuser

# 创建测试分组（带配额）
./sproxy.exe admin group add --config test-sproxy.yaml testgroup
./sproxy.exe admin group set-quota --config test-sproxy.yaml testgroup --daily 10000 --monthly 100000 --rpm 10

# 将用户加入分组
./sproxy.exe admin user set-group --config test-sproxy.yaml testuser --group testgroup
```

### 3. 登录
```bash
echo -e "testuser\ntestpass123" | ./cproxy.exe login --server http://localhost:9000
```

---

## 测试用例

### 类别 1: 基础功能测试

#### 1.1 流式请求
```bash
./mockagent.exe --url http://localhost:8080 --count 10 --stream --v
```
**预期结果**: ✅ 10/10 通过，收到流式响应

#### 1.2 非流式请求
```bash
./mockagent.exe --url http://localhost:8080 --count 10 --stream=false --v
```
**预期结果**: ✅ 10/10 通过，收到完整 JSON 响应

#### 1.3 并发请求
```bash
./mockagent.exe --url http://localhost:8080 --count 20 --concurrency 5 --v
```
**预期结果**: ✅ 20/20 通过，无并发冲突

---

### 类别 2: 认证测试

#### 2.1 有效 Token 请求
```bash
# 已登录状态下的正常请求
./mockagent.exe --url http://localhost:8080 --count 5 --v
```
**预期结果**: ✅ 5/5 通过

#### 2.2 无效 Token 请求
```bash
# 手动发送带无效 token 的请求
curl -X POST http://localhost:8080/v1/messages \
  -H "Authorization: Bearer invalid-token-12345" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-5-sonnet-20241022","max_tokens":100,"messages":[{"role":"user","content":"test"}]}'
```
**预期结果**: ❌ HTTP 401, 错误信息 "invalid_token"

#### 2.3 登出后请求
```bash
# 登出
./cproxy.exe logout --server http://localhost:9000

# 尝试请求
./mockagent.exe --url http://localhost:8080 --count 1 --v
```
**预期结果**: ❌ 请求失败，提示需要登录

#### 2.4 重新登录
```bash
# 重新登录
echo -e "testuser\ntestpass123" | ./cproxy.exe login --server http://localhost:9000

# 验证请求成功
./mockagent.exe --url http://localhost:8080 --count 5 --v
```
**预期结果**: ✅ 5/5 通过

---

### 类别 3: 配额限制测试

#### 3.1 查看当前配额
```bash
./sproxy.exe admin quota status --config test-sproxy.yaml --user testuser
```
**预期结果**: 显示当前用量和配额限制

#### 3.2 RPM 限流测试
```bash
# 快速发送超过 RPM 限制的请求（testgroup RPM=10）
./mockagent.exe --url http://localhost:8080 --count 20 --concurrency 20
```
**预期结果**: 部分请求被限流（HTTP 429）

#### 3.3 日配额测试（需要修改配额）
```bash
# 设置很小的日配额
./sproxy.exe admin group set-quota --config test-sproxy.yaml testgroup --daily 100 --monthly 100000 --rpm 10

# 发送请求直到超过配额
./mockagent.exe --url http://localhost:8080 --count 50 --v
```
**预期结果**: 达到配额后请求被拒绝（HTTP 429）

#### 3.4 恢复配额
```bash
# 恢复正常配额
./sproxy.exe admin group set-quota --config test-sproxy.yaml testgroup --daily 10000 --monthly 100000 --rpm 10
```

---

### 类别 4: 压力测试

#### 4.1 中等并发
```bash
./mockagent.exe --url http://localhost:8080 --count 100 --concurrency 10
```
**预期结果**: ✅ 100/100 通过，耗时 < 5秒

#### 4.2 高并发
```bash
./mockagent.exe --url http://localhost:8080 --count 500 --concurrency 50
```
**预期结果**: ✅ 500/500 通过，耗时 < 30秒

#### 4.3 长时间运行
```bash
./mockagent.exe --url http://localhost:8080 --count 1000 --concurrency 20
```
**预期结果**: ✅ 1000/1000 通过，无内存泄漏

---

### 类别 5: Dashboard 功能测试

#### 5.1 访问 Dashboard
```bash
# 在浏览器中打开
open http://localhost:9000/dashboard/
```
**预期结果**: 显示登录页面

#### 5.2 管理员登录
- 用户名: `admin`
- 密码: `testpass123`（配置文件中的 password_hash）

**预期结果**: 成功登录，显示管理界面

#### 5.3 查看用量趋势
- 导航到「用量统计」或「趋势图」
- 选择时间范围（7天、30天）

**预期结果**: 显示用量趋势图表

#### 5.4 用户管理
- 导航到「用户管理」
- 查看用户列表
- 尝试禁用/启用用户

**预期结果**: 操作成功，用户状态更新

---

### 类别 6: API 端点测试

#### 6.1 健康检查
```bash
# cproxy 健康检查
curl http://localhost:8080/health

# sproxy 健康检查
curl http://localhost:9000/health
```
**预期结果**: 返回 `{"status":"ok",...}`

#### 6.2 用户配额状态 API
```bash
# 获取用户 JWT token
TOKEN=$(cat ~/.config/pairproxy/token.json | jq -r '.access_token')

# 查询配额状态
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9000/api/user/quota-status
```
**预期结果**: 返回配额和用量信息

#### 6.3 用量历史 API
```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/api/user/usage-history?limit=10"
```
**预期结果**: 返回最近 10 条用量记录

#### 6.4 趋势图 API
```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9000/api/user/trends?days=7"
```
**预期结果**: 返回 7 天的用量趋势数据

---

### 类别 7: 错误处理测试

#### 7.1 上游服务不可用
```bash
# 停止 mockllm
pkill -f mockllm

# 尝试请求
./mockagent.exe --url http://localhost:8080 --count 5 --v
```
**预期结果**: 请求失败，返回上游错误

#### 7.2 恢复上游服务
```bash
# 重启 mockllm
./mockllm.exe --addr :11434 &
sleep 2

# 验证请求恢复
./mockagent.exe --url http://localhost:8080 --count 5 --v
```
**预期结果**: ✅ 5/5 通过

#### 7.3 无效请求格式
```bash
curl -X POST http://localhost:8080/v1/messages \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"invalid": "json"}'
```
**预期结果**: HTTP 400, 错误信息说明缺少必需字段

---

### 类别 8: OpenAI 兼容性测试

#### 8.1 OpenAI 格式请求
```bash
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Hello"}],
    "stream": true
  }'
```
**预期结果**: 返回流式响应，自动注入 stream_options

#### 8.2 OpenAI Bearer 认证
```bash
# 使用 Bearer token（而非 X-PairProxy-Auth）
curl -X POST http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4",
    "messages": [{"role": "user", "content": "Test"}]
  }'
```
**预期结果**: 认证成功，返回响应

---

### 类别 9: 数据库验证

#### 9.1 查看用量记录
```bash
sqlite3 test-chain.db "SELECT user_id, model, input_tokens, output_tokens, created_at FROM usage_logs ORDER BY created_at DESC LIMIT 10;"
```
**预期结果**: 显示最近 10 条用量记录

#### 9.2 验证用量统计
```bash
./sproxy.exe admin stats --config test-sproxy.yaml --user testuser --days 1
```
**预期结果**: 显示今日用量统计

---

## 测试清理

```bash
# 停止所有服务
pkill -f "mockllm|sproxy|cproxy"

# 清理测试数据（可选）
rm -f test-chain.db test-chain.db-shm test-chain.db-wal

# 清理 token
rm -f ~/.config/pairproxy/token.json
```

---

## 测试检查清单

### 基础功能 (3/3)
- [ ] 1.1 流式请求
- [ ] 1.2 非流式请求
- [ ] 1.3 并发请求

### 认证测试 (4/4)
- [ ] 2.1 有效 Token 请求
- [ ] 2.2 无效 Token 请求
- [ ] 2.3 登出后请求
- [ ] 2.4 重新登录

### 配额限制 (4/4)
- [ ] 3.1 查看当前配额
- [ ] 3.2 RPM 限流测试
- [ ] 3.3 日配额测试
- [ ] 3.4 恢复配额

### 压力测试 (3/3)
- [ ] 4.1 中等并发 (100 req, 10 concurrency)
- [ ] 4.2 高并发 (500 req, 50 concurrency)
- [ ] 4.3 长时间运行 (1000 req)

### Dashboard 功能 (4/4)
- [ ] 5.1 访问 Dashboard
- [ ] 5.2 管理员登录
- [ ] 5.3 查看用量趋势
- [ ] 5.4 用户管理

### API 端点 (4/4)
- [ ] 6.1 健康检查
- [ ] 6.2 用户配额状态 API
- [ ] 6.3 用量历史 API
- [ ] 6.4 趋势图 API

### 错误处理 (3/3)
- [ ] 7.1 上游服务不可用
- [ ] 7.2 恢复上游服务
- [ ] 7.3 无效请求格式

### OpenAI 兼容性 (2/2)
- [ ] 8.1 OpenAI 格式请求
- [ ] 8.2 OpenAI Bearer 认证

### 数据库验证 (2/2)
- [ ] 9.1 查看用量记录
- [ ] 9.2 验证用量统计

---

## 总计

**总测试用例**: 29
**必须通过**: 所有用例
**预计耗时**: 30-45 分钟

---

## 注意事项

1. **测试顺序**: 建议按照文档顺序执行，某些测试依赖前面的设置
2. **配额恢复**: 测试配额限制后记得恢复正常配额
3. **服务重启**: 错误处理测试后需要重启服务
4. **数据清理**: 测试完成后清理测试数据
5. **并发测试**: 高并发测试可能需要调整系统限制（ulimit）

---

## 故障排查

### 问题: 请求失败 401
**解决**: 检查 token 是否过期，重新登录

### 问题: 端口被占用
**解决**: 使用 `lsof -i :8080` 查找并杀死占用进程

### 问题: 数据库锁定
**解决**: 确保所有服务已停止，删除 `.db-shm` 和 `.db-wal` 文件

### 问题: 高并发测试失败
**解决**: 检查系统文件描述符限制 `ulimit -n`，必要时增加
