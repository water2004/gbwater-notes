package app

import (
	"net/http"
	"strings"
)

func userPayload(user *User) map[string]any {
	payload := map[string]any{
		"id":            user.ID,
		"email":         user.Email,
		"username":      user.Username,
		"role":          user.Role,
		"status":        user.Status,
		"emailVerified": user.EmailVerified(),
		"isAdmin":       user.IsAdmin(),
		"createdAt":     user.CreatedAt,
	}
	if user.LastLoginAt != nil {
		payload["lastLoginAt"] = user.LastLoginAt
	}
	return payload
}

func (s *Server) registrationOptions(w http.ResponseWriter, _ *http.Request) {
	policy := s.authPolicy.snapshot()
	domains := []string{}
	if s.config.AllowRegister && policy.EmailMode == "allowlist" {
		domains = policy.EmailDomains
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Enabled      bool     `json:"enabled"`
		EmailDomains []string `json:"emailDomains"`
	}{s.config.AllowRegister, domains})
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var input RegisterInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	user, raw, err := s.auth.Register(r.Context(), input, s.requestMeta(r))
	if err != nil {
		fail(w, err)
		return
	}
	setSessionCookie(w, raw, s.config.SessionTTL, s.config.SecureCookie)
	writeJSON(w, http.StatusCreated, map[string]any{"user": userPayload(user)})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var input LoginInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	user, raw, err := s.auth.Login(r.Context(), input, s.requestMeta(r))
	if err != nil {
		fail(w, err)
		return
	}
	setSessionCookie(w, raw, s.config.SessionTTL, s.config.SecureCookie)
	writeJSON(w, http.StatusOK, map[string]any{"user": userPayload(user)})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Logout(r.Context(), sessionTokenFromRequest(r)); err != nil {
		fail(w, err)
		return
	}
	clearSessionCookie(w, s.config.SecureCookie)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleSession 允许匿名访问，前端可据此决定渲染登录入口还是用户信息。
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	user, _, err := s.connect(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "会话校验失败")
		return
	}
	if user == nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userPayload(user)})
}

func (s *Server) handleEmailCode(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email   string `json:"email"`
		Purpose string `json:"purpose"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	if strings.TrimSpace(input.Purpose) == "" {
		input.Purpose = EmailPurposeRegister
	}
	if err := s.auth.SendEmailCode(r.Context(), input.Email, input.Purpose); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "cooldownSeconds": int(emailCodeCooldown.Seconds())})
}

func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Code     string `json:"code"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	if err := s.auth.ResetPassword(r.Context(), input.Email, input.Code, input.Password); err != nil {
		fail(w, err)
		return
	}
	clearSessionCookie(w, s.config.SecureCookie)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var input struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	user := s.currentUser(r)
	if err := s.auth.ChangePassword(r.Context(), user, input.CurrentPassword, input.NewPassword, sessionTokenFromRequest(r)); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	updated, err := s.auth.Rename(r.Context(), s.currentUser(r), input.Username)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userPayload(updated)})
}
