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
//   - GET  /keygen/                    — 静态 HTML 页面
//   - POST /keygen/api/login           — 用户名+密码登录，返回 key + session token
//   - POST /keygen/api/regenerate      — 用 session token 查看当前 key
//   - POST /keygen/api/change-password — 修改密码，旧 Key 立即失效，返回新 Key
//
// 与 Dashboard 完全独立：使用普通用户密码，不使用管理员密码。
// API Key 由用户自己的 PasswordHash 派生（HMAC-SHA256），改密码即自动轮换 Key。
type KeygenHandler struct {
	logger       *zap.Logger
	userRepo     *db.UserRepo
	jwtMgr       *auth.Manager
	keyCache     *keygen.KeyCache // 可选，改密后立即踢出旧 Key 缓存
	isWorkerNode bool
}

// NewKeygenHandler 创建 KeygenHandler。
func NewKeygenHandler(logger *zap.Logger, userRepo *db.UserRepo, jwtMgr *auth.Manager) *KeygenHandler {
	return &KeygenHandler{
		logger:   logger.Named("keygen_handler"),
		userRepo: userRepo,
		jwtMgr:   jwtMgr,
	}
}

// SetKeyCache 注入 API Key 缓存（改密后立即踢出旧 Key，不等 TTL 自然过期）。
func (h *KeygenHandler) SetKeyCache(cache *keygen.KeyCache) { h.keyCache = cache }

// SetWorkerMode 设置 Worker 节点模式；Worker 节点不允许写操作（API Key 生成/重置）。
//
// 注意：必须在 RegisterRoutes 之前调用，因为封锁逻辑在路由注册时分支，
// 而不是运行时中间件判断。调用顺序颠倒会导致封锁静默失效。
func (h *KeygenHandler) SetWorkerMode(isWorker bool) {
	h.isWorkerNode = isWorker
}

// RegisterRoutes 注册 /keygen/ 相关路由。
func (h *KeygenHandler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /keygen/", h.handleStaticPage)
	if h.isWorkerNode {
		// Worker 节点：写端点返回 403，引导用户到 Primary 节点操作
		blockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.logger.Warn("blocked keygen write operation on worker node",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
			)
			writeKeygenError(w, http.StatusForbidden, "worker_read_only",
				"API key operations are not available on worker nodes; please use the primary node")
		})
		mux.Handle("POST /keygen/api/login", blockHandler)
		mux.Handle("POST /keygen/api/regenerate", blockHandler)
		mux.Handle("POST /keygen/api/change-password", blockHandler)
	} else {
		mux.HandleFunc("POST /keygen/api/login", h.handleLogin)
		mux.HandleFunc("POST /keygen/api/regenerate", h.handleRegenerate)
		mux.HandleFunc("POST /keygen/api/change-password", h.handleChangePassword)
	}
}

// keygenLoginRequest 登录请求体
type keygenLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// keygenLoginResponse 登录响应体
type keygenLoginResponse struct {
	Username  string `json:"username"`
	Key       string `json:"key"`
	Token     string `json:"token"`
	ExpiresIn int    `json:"expires_in"`
}

// keygenRegenerateResponse 重新生成 key 响应体
type keygenRegenerateResponse struct {
	Username string `json:"username"`
	Key      string `json:"key"`
	Message  string `json:"message"`
}

func (h *KeygenHandler) handleStaticPage(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(keygenHTML))
}

func (h *KeygenHandler) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req keygenLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("keygen login: invalid request body", zap.Error(err))
		writeKeygenError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.Username == "" || req.Password == "" {
		h.logger.Warn("keygen login: missing required fields",
			zap.Bool("has_username", req.Username != ""),
			zap.Bool("has_password", req.Password != ""),
		)
		writeKeygenError(w, http.StatusBadRequest, "missing_fields", "username and password required")
		return
	}

	// 查询用户（仅本地账户）
	user, err := h.userRepo.GetByUsernameAndProvider(req.Username, "local")
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

	// 验证密码
	if !auth.VerifyPassword(h.logger, user.PasswordHash, req.Password) {
		h.logger.Warn("keygen login: wrong password", zap.String("username", req.Username))
		writeKeygenError(w, http.StatusUnauthorized, "invalid_credentials", "用户名或密码错误")
		return
	}

	// 生成 API Key（由用户自己的 PasswordHash 派生，改密码即自动轮换）
	apiKey, err := keygen.GenerateKey(req.Username, []byte(user.PasswordHash))
	if err != nil {
		h.logger.Error("keygen login: key generation failed",
			zap.String("username", req.Username), zap.Error(err))
		writeKeygenError(w, http.StatusInternalServerError, "key_gen_error", "failed to generate API key")
		return
	}

	// 生成 session token（1小时有效）
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
	_ = json.NewEncoder(w).Encode(keygenLoginResponse{
		Username:  req.Username,
		Key:       apiKey,
		Token:     token,
		ExpiresIn: 3600,
	})
}

func (h *KeygenHandler) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		h.logger.Warn("keygen regenerate: missing or malformed Authorization header",
			zap.String("remote_addr", r.RemoteAddr),
		)
		writeKeygenError(w, http.StatusUnauthorized, "missing_token", "Authorization: Bearer <token> required")
		return
	}
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")

	claims, err := h.jwtMgr.Parse(tokenStr)
	if err != nil {
		h.logger.Warn("keygen regenerate: invalid session token", zap.Error(err))
		writeKeygenError(w, http.StatusUnauthorized, "session_expired", "会话已过期，请重新登录")
		return
	}

	// 从 DB 重新取用户以获取最新 PasswordHash（密码可能已被管理员重置）
	user, err := h.userRepo.GetByID(claims.UserID)
	if err != nil {
		h.logger.Error("keygen regenerate: user lookup failed",
			zap.String("user_id", claims.UserID), zap.Error(err))
		writeKeygenError(w, http.StatusInternalServerError, "internal_error", "user lookup failed")
		return
	}
	if user == nil || !user.IsActive {
		h.logger.Warn("keygen regenerate: user not found or inactive",
			zap.String("user_id", claims.UserID))
		writeKeygenError(w, http.StatusUnauthorized, "account_disabled", "user account not found or disabled")
		return
	}

	apiKey, err := keygen.GenerateKey(user.Username, []byte(user.PasswordHash))
	if err != nil {
		h.logger.Error("keygen regenerate: key generation failed",
			zap.String("username", user.Username), zap.Error(err))
		writeKeygenError(w, http.StatusInternalServerError, "key_gen_error", "failed to generate API key")
		return
	}

	h.logger.Info("keygen regenerate: key derived",
		zap.String("username", user.Username),
		zap.String("user_id", user.ID),
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(keygenRegenerateResponse{
		Username: user.Username,
		Key:      apiKey,
		Message:  "Key 已获取（由密码派生，改密码可轮换）",
	})
}

// keygenChangePasswordRequest 修改密码请求体
type keygenChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

// keygenChangePasswordResponse 修改密码响应体
type keygenChangePasswordResponse struct {
	Key     string `json:"key"`
	Message string `json:"message"`
}

func (h *KeygenHandler) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeKeygenError(w, http.StatusUnauthorized, "missing_token", "Authorization: Bearer <token> required")
		return
	}
	claims, err := h.jwtMgr.Parse(strings.TrimPrefix(authHeader, "Bearer "))
	if err != nil {
		h.logger.Warn("keygen change-password: invalid session token", zap.Error(err))
		writeKeygenError(w, http.StatusUnauthorized, "session_expired", "会话已过期，请重新登录")
		return
	}

	var req keygenChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeKeygenError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if req.OldPassword == "" || req.NewPassword == "" {
		writeKeygenError(w, http.StatusBadRequest, "missing_fields", "old_password and new_password required")
		return
	}
	if req.OldPassword == req.NewPassword {
		writeKeygenError(w, http.StatusBadRequest, "same_password", "新密码不能与旧密码相同")
		return
	}

	// 从 DB 取用户（仅本地账户可修改密码）
	user, err := h.userRepo.GetByID(claims.UserID)
	if err != nil {
		h.logger.Error("keygen change-password: db error", zap.String("user_id", claims.UserID), zap.Error(err))
		writeKeygenError(w, http.StatusInternalServerError, "internal_error", "user lookup failed")
		return
	}
	if user == nil || !user.IsActive {
		writeKeygenError(w, http.StatusUnauthorized, "account_disabled", "账户不存在或已被禁用")
		return
	}
	if user.AuthProvider != "local" {
		writeKeygenError(w, http.StatusForbidden, "ldap_user", "LDAP 用户请通过 LDAP 管理平台修改密码")
		return
	}

	// 验证旧密码
	if !auth.VerifyPassword(h.logger, user.PasswordHash, req.OldPassword) {
		h.logger.Warn("keygen change-password: wrong old password", zap.String("username", user.Username))
		writeKeygenError(w, http.StatusUnauthorized, "wrong_password", "旧密码错误")
		return
	}

	// Hash 新密码并更新 DB
	newHash, err := auth.HashPassword(h.logger, req.NewPassword)
	if err != nil {
		writeKeygenError(w, http.StatusBadRequest, "invalid_password", "invalid new password")
		return
	}
	if err := h.userRepo.UpdatePassword(user.ID, newHash); err != nil {
		h.logger.Error("keygen change-password: update failed", zap.String("user_id", user.ID), zap.Error(err))
		writeKeygenError(w, http.StatusInternalServerError, "internal_error", "failed to update password")
		return
	}

	// 旧 Key 立即失效
	if h.keyCache != nil {
		h.keyCache.InvalidateByUserID(user.ID)
	}

	// 用新 PasswordHash 派生新 Key
	newKey, err := keygen.GenerateKey(user.Username, []byte(newHash))
	if err != nil {
		h.logger.Error("keygen change-password: key generation failed", zap.String("username", user.Username), zap.Error(err))
		writeKeygenError(w, http.StatusInternalServerError, "key_gen_error", "failed to generate new API key")
		return
	}

	h.logger.Info("keygen change-password: success",
		zap.String("username", user.Username),
		zap.String("user_id", user.ID),
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(keygenChangePasswordResponse{
		Key:     newKey,
		Message: "密码已修改，新 Key 已生成，旧 Key 立即失效",
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
  button.danger{background:#dc2626}
  button.danger:hover{background:#b91c1c}
  .key-box{font-family:monospace;background:#f8f9fa;border:1px solid #e9ecef;padding:14px;border-radius:6px;word-break:break-all;font-size:13px;margin:8px 0}
  .section{border-top:1px solid #eee;margin-top:24px;padding-top:16px}
  pre{background:#1e1e2e;color:#cdd6f4;padding:14px;border-radius:6px;font-size:12px;overflow-x:auto}
  .hidden{display:none}
  .error{color:#dc2626;font-size:13px;margin-top:6px}
  .success{color:#16a34a;font-size:13px;margin-top:6px}
  .tip{color:#6b7280;font-size:12px;margin-top:4px}
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
  <p>您的 API Key（由密码派生，改密码自动更新）：</p>
  <div class="key-box" id="api-key-display"></div>
  <button onclick="copyKey()">复制</button>

  <div class="section">
    <h3>Claude Code 配置</h3>
    <pre id="cc-snippet"></pre>
    <h3>OpenCode 配置</h3>
    <pre id="oc-snippet"></pre>
  </div>

  <div class="section">
    <h3>修改密码 <span class="tip">（修改后旧 Key 立即失效，将生成新 Key）</span></h3>
    <label>当前密码</label><input type="password" id="old-password" autocomplete="current-password">
    <label>新密码</label><input type="password" id="new-password" autocomplete="new-password">
    <label>确认新密码</label><input type="password" id="confirm-password" autocomplete="new-password">
    <br><br>
    <button class="danger" onclick="changePassword()">修改密码并更新 Key</button>
    <div class="error" id="change-error"></div>
    <div class="success hidden" id="change-success"></div>
  </div>

  <div class="section">
    <button class="secondary" onclick="logout()">退出登录</button>
  </div>
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

async function changePassword() {
  const oldPassword = document.getElementById('old-password').value;
  const newPassword = document.getElementById('new-password').value;
  const confirmPassword = document.getElementById('confirm-password').value;
  const errEl = document.getElementById('change-error');
  const okEl = document.getElementById('change-success');
  errEl.textContent = '';
  okEl.classList.add('hidden');

  if (!oldPassword || !newPassword || !confirmPassword) {
    errEl.textContent = '请填写所有密码字段'; return;
  }
  if (newPassword !== confirmPassword) {
    errEl.textContent = '两次输入的新密码不一致'; return;
  }
  if (newPassword === oldPassword) {
    errEl.textContent = '新密码不能与旧密码相同'; return;
  }
  if (newPassword.length < 8) {
    errEl.textContent = '新密码至少 8 个字符'; return;
  }

  try {
    const r = await fetch('/keygen/api/change-password', {
      method: 'POST',
      headers: {'Content-Type':'application/json', 'Authorization': 'Bearer ' + sessionToken},
      body: JSON.stringify({old_password: oldPassword, new_password: newPassword})
    });
    const data = await r.json();
    if (!r.ok) { errEl.textContent = data.message || '修改失败'; return; }

    // 更新显示的 Key
    currentKey = data.key;
    document.getElementById('api-key-display').textContent = data.key;
    document.getElementById('cc-snippet').textContent =
      'export ANTHROPIC_BASE_URL=' + BASE + '/anthropic\nexport ANTHROPIC_API_KEY=' + data.key;
    document.getElementById('oc-snippet').textContent =
      'export OPENAI_BASE_URL=' + BASE + '/v1\nexport OPENAI_API_KEY=' + data.key;

    // 清空密码输入框
    document.getElementById('old-password').value = '';
    document.getElementById('new-password').value = '';
    document.getElementById('confirm-password').value = '';

    okEl.textContent = '✓ 密码已修改，新 Key 已更新，请复制并更新您的配置';
    okEl.classList.remove('hidden');
  } catch(e) { errEl.textContent = '网络错误'; }
}

function logout() {
  sessionToken = ''; currentKey = '';
  document.getElementById('key-section').classList.add('hidden');
  document.getElementById('login-section').classList.remove('hidden');
  document.getElementById('password').value = '';
  document.getElementById('old-password').value = '';
  document.getElementById('new-password').value = '';
  document.getElementById('confirm-password').value = '';
  document.getElementById('change-error').textContent = '';
  document.getElementById('change-success').classList.add('hidden');
}
</script>
</body>
</html>`
