package app

import (
	"crypto/rand"
	"errors"
	"math/big"
	"net/http"
	"strings"
)

func (s *Server) adminUsers(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("query")
	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := parsePositive(value); err == nil {
			limit = parsed
		}
	}
	offset := 0
	if value := r.URL.Query().Get("offset"); value != "" {
		if parsed, err := parsePositive(value); err == nil {
			offset = parsed
		}
	}
	users, total, err := s.users.List(r.Context(), query, limit, offset)
	if err != nil {
		fail(w, err)
		return
	}
	items := make([]map[string]any, 0, len(users))
	for index := range users {
		items = append(items, userPayload(&users[index]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": items, "total": total, "limit": limit, "offset": offset})
}

func (s *Server) adminUpdateUser(w http.ResponseWriter, r *http.Request) {
	target, err := s.users.ByID(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
		return
	}
	var input struct {
		Role   *string `json:"role"`
		Status *string `json:"status"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	role, status := target.Role, target.Status
	if input.Role != nil {
		if *input.Role != RoleAdmin && *input.Role != RoleUser {
			writeError(w, http.StatusBadRequest, "invalid_role", "角色只能是 admin 或 user")
			return
		}
		role = *input.Role
	}
	if input.Status != nil {
		if *input.Status != StatusActive && *input.Status != StatusDisabled {
			writeError(w, http.StatusBadRequest, "invalid_status", "状态只能是 active 或 disabled")
			return
		}
		status = *input.Status
	}
	actor := s.currentUser(r)
	if target.ID == actor.ID && (role != target.Role || status != target.Status) {
		writeError(w, http.StatusBadRequest, "self_change_forbidden", "不能修改自己的角色或状态")
		return
	}
	if target.IsAdmin() && (role != RoleAdmin || status != StatusActive) {
		count, err := s.users.CountAdmins(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		if count <= 1 {
			writeError(w, http.StatusBadRequest, "last_admin", "至少需要保留一个可用的管理员")
			return
		}
	}
	if err := s.users.SetRoleStatus(r.Context(), target.ID, role, status); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
			return
		}
		fail(w, err)
		return
	}
	if status == StatusDisabled {
		_ = s.sessions.DeleteForUser(r.Context(), target.ID)
	}
	updated, err := s.users.ByID(r.Context(), target.ID)
	if err != nil || updated == nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userPayload(updated)})
}

func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	target, err := s.users.ByID(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
		return
	}
	if target.ID == s.currentUser(r).ID {
		writeError(w, http.StatusBadRequest, "self_delete_forbidden", "不能删除自己的账号")
		return
	}
	if target.IsAdmin() {
		count, err := s.users.CountAdmins(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		if count <= 1 {
			writeError(w, http.StatusBadRequest, "last_admin", "至少需要保留一个可用的管理员")
			return
		}
	}
	if err := s.users.Delete(r.Context(), target.ID); err != nil {
		if errors.Is(err, ErrUserNotFound) {
			writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
			return
		}
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminResetPassword 生成一次性临时密码并返回给管理员，同时吊销该用户全部会话。
func (s *Server) adminResetPassword(w http.ResponseWriter, r *http.Request) {
	target, err := s.users.ByID(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if r.ContentLength != 0 {
		if err := decodeJSON(w, r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
			return
		}
	}
	password := strings.TrimSpace(input.Password)
	generated := false
	if password == "" {
		password, err = temporaryPassword()
		if err != nil {
			fail(w, err)
			return
		}
		generated = true
	}
	if rules := passwordRules(password); len(rules) > 0 {
		writeError(w, http.StatusBadRequest, "invalid_password", passwordRuleMessage(rules[0]))
		return
	}
	hash, err := hashPassword(password)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.users.SetPassword(r.Context(), target.ID, hash); err != nil {
		fail(w, err)
		return
	}
	if err := s.sessions.DeleteForUser(r.Context(), target.ID); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "password": password, "generated": generated})
}

const temporaryAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%"

func temporaryPassword() (string, error) {
	length := 16
	buffer := make([]byte, length)
	max := big.NewInt(int64(len(temporaryAlphabet)))
	for index := range buffer {
		value, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		buffer[index] = temporaryAlphabet[value.Int64()]
	}
	return string(buffer), nil
}

func parsePositive(value string) (int, error) {
	number := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, errors.New("not a number")
		}
		number = number*10 + int(char-'0')
		if number > 1000000 {
			return 0, errors.New("too large")
		}
	}
	return number, nil
}
