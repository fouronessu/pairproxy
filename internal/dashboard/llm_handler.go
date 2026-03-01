package dashboard

import (
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/l17728/pairproxy/internal/db"
	"github.com/l17728/pairproxy/internal/proxy"
)

// llmPageData LLM 管理页数据
type llmPageData struct {
	baseData
	Targets     []proxy.LLMTargetStatus
	Bindings    []db.LLMBinding
	BoundCount  map[string]int // target URL → 绑定数量
	Users       []db.User
	Groups      []db.Group
	DrainStatus proxy.DrainStatus // 排水状态
}

// handleLLMPage GET /dashboard/llm
func (h *Handler) handleLLMPage(w http.ResponseWriter, r *http.Request) {
	flash := r.URL.Query().Get("flash")
	errMsg := r.URL.Query().Get("error")

	data := llmPageData{
		baseData:   baseData{Flash: flash, Error: errMsg},
		BoundCount: make(map[string]int),
	}

	if h.llmHealthFn != nil {
		data.Targets = h.llmHealthFn()
	}

	if h.llmBindingRepo != nil {
		bindings, err := h.llmBindingRepo.List()
		if err != nil {
			h.logger.Error("list llm bindings", zap.Error(err))
		} else {
			data.Bindings = bindings
			for _, b := range bindings {
				data.BoundCount[b.TargetURL]++
			}
		}
	}

	if h.userRepo != nil {
		users, _ := h.userRepo.ListByGroup("")
		data.Users = users
	}
	if h.groupRepo != nil {
		groups, _ := h.groupRepo.List()
		data.Groups = groups
	}

	// 获取排水状态
	if h.drainStatusFn != nil {
		data.DrainStatus = h.drainStatusFn()
	}

	h.renderPage(w, "llm.html", data)
}

// handleLLMCreateBinding POST /dashboard/llm/bindings
func (h *Handler) handleLLMCreateBinding(w http.ResponseWriter, r *http.Request) {
	if h.llmBindingRepo == nil {
		http.Redirect(w, r, "/dashboard/llm?error=LLM+binding+not+configured", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, "/dashboard/llm?error=invalid+form", http.StatusSeeOther)
		return
	}
	targetURL := r.FormValue("target_url")
	bindType := r.FormValue("bind_type")
	if targetURL == "" {
		http.Redirect(w, r, "/dashboard/llm?error=target_url+required", http.StatusSeeOther)
		return
	}

	var userID, groupID *string
	switch bindType {
	case "group":
		gid := r.FormValue("group_id")
		if gid == "" {
			http.Redirect(w, r, "/dashboard/llm?error=group_id+required", http.StatusSeeOther)
			return
		}
		groupID = &gid
	default:
		uid := r.FormValue("user_id")
		if uid == "" {
			http.Redirect(w, r, "/dashboard/llm?error=user_id+required", http.StatusSeeOther)
			return
		}
		userID = &uid
	}

	if err := h.llmBindingRepo.Set(targetURL, userID, groupID); err != nil {
		h.logger.Error("create llm binding", zap.Error(err))
		http.Redirect(w, r, "/dashboard/llm?error="+err.Error(), http.StatusSeeOther)
		return
	}
	h.logger.Info("llm binding created via dashboard",
		zap.String("target_url", targetURL),
		zap.Any("user_id", userID),
		zap.Any("group_id", groupID),
	)
	http.Redirect(w, r, "/dashboard/llm?flash=绑定已创建", http.StatusSeeOther)
}

// handleLLMDeleteBinding POST /dashboard/llm/bindings/{id}/delete
func (h *Handler) handleLLMDeleteBinding(w http.ResponseWriter, r *http.Request) {
	if h.llmBindingRepo == nil {
		http.Redirect(w, r, "/dashboard/llm?error=LLM+binding+not+configured", http.StatusSeeOther)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.Redirect(w, r, "/dashboard/llm?error=id+required", http.StatusSeeOther)
		return
	}
	if err := h.llmBindingRepo.Delete(id); err != nil {
		h.logger.Error("delete llm binding", zap.String("id", id), zap.Error(err))
		http.Redirect(w, r, "/dashboard/llm?error="+err.Error(), http.StatusSeeOther)
		return
	}
	h.logger.Info("llm binding deleted via dashboard", zap.String("id", id))
	http.Redirect(w, r, "/dashboard/llm?flash=绑定已删除", http.StatusSeeOther)
}

// handleLLMDistribute POST /dashboard/llm/distribute
// 均分所有活跃用户到所有已配置 target。
func (h *Handler) handleLLMDistribute(w http.ResponseWriter, r *http.Request) {
	if h.llmBindingRepo == nil {
		http.Redirect(w, r, "/dashboard/llm?error=LLM+binding+not+configured", http.StatusSeeOther)
		return
	}

	var targetURLs []string
	if h.llmHealthFn != nil {
		for _, s := range h.llmHealthFn() {
			targetURLs = append(targetURLs, s.URL)
		}
	}
	if len(targetURLs) == 0 {
		http.Redirect(w, r, "/dashboard/llm?error=no+LLM+targets+configured", http.StatusSeeOther)
		return
	}

	var userIDs []string
	if h.userRepo != nil {
		users, err := h.userRepo.ListByGroup("")
		if err != nil {
			h.logger.Error("list users for distribute", zap.Error(err))
			http.Redirect(w, r, "/dashboard/llm?error=failed+to+list+users", http.StatusSeeOther)
			return
		}
		for _, u := range users {
			if u.IsActive {
				userIDs = append(userIDs, u.ID)
			}
		}
	}

	if err := h.llmBindingRepo.EvenDistribute(userIDs, targetURLs); err != nil {
		h.logger.Error("llm distribute failed", zap.Error(err))
		http.Redirect(w, r, "/dashboard/llm?error="+err.Error(), http.StatusSeeOther)
		return
	}

	h.logger.Info("llm even distribution via dashboard",
		zap.Int("users", len(userIDs)),
		zap.Int("targets", len(targetURLs)),
	)
	_ = time.Now() // keep import used
	http.Redirect(w, r, "/dashboard/llm?flash=均分完成，共分配"+itoa(len(userIDs))+"个用户", http.StatusSeeOther)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

// ---------------------------------------------------------------------------
// 排水控制
// ---------------------------------------------------------------------------

// handleDrainEnter POST /dashboard/drain/enter
func (h *Handler) handleDrainEnter(w http.ResponseWriter, r *http.Request) {
	if h.drainFn == nil {
		http.Redirect(w, r, "/dashboard/llm?error=排水功能未配置", http.StatusSeeOther)
		return
	}
	if err := h.drainFn(); err != nil {
		h.logger.Error("drain enter failed", zap.Error(err))
		http.Redirect(w, r, "/dashboard/llm?error="+err.Error(), http.StatusSeeOther)
		return
	}
	h.logger.Info("drain mode entered via dashboard")
	http.Redirect(w, r, "/dashboard/llm?flash=已进入排水模式", http.StatusSeeOther)
}

// handleDrainExit POST /dashboard/drain/exit
func (h *Handler) handleDrainExit(w http.ResponseWriter, r *http.Request) {
	if h.undrainFn == nil {
		http.Redirect(w, r, "/dashboard/llm?error=排水功能未配置", http.StatusSeeOther)
		return
	}
	if err := h.undrainFn(); err != nil {
		h.logger.Error("drain exit failed", zap.Error(err))
		http.Redirect(w, r, "/dashboard/llm?error="+err.Error(), http.StatusSeeOther)
		return
	}
	h.logger.Info("drain mode exited via dashboard")
	http.Redirect(w, r, "/dashboard/llm?flash=已退出排水模式", http.StatusSeeOther)
}
