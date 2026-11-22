//go:build integration

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func requestJSON(t *testing.T, server *Server, method, path string, payload any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, body)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
}

func firstCookie(t *testing.T, recorder *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == sessionCookieName && cookie.Value != "" {
			return cookie
		}
	}
	t.Fatalf("no session cookie in response: %s", recorder.Body.String())
	return nil
}

func decodeInto(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %q: %v", recorder.Body.String(), err)
	}
}

func TestAuthFlowWithEmailVerificationAndReading(t *testing.T) {
	mailer := &capturingMailer{}
	config := integrationConfig(t, freshDatabase(t))
	config.Mailer = mailer
	config.AdminPassword = "AdminPass123"
	config.AdminEmail = "admin@example.com"
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}

	const email = "reader@example.com"
	const password = "ReaderPass1"

	sendCode := requestJSON(t, server, http.MethodPost, "/api/auth/email-code",
		map[string]string{"email": email, "purpose": EmailPurposeRegister}, nil)
	if sendCode.Code != http.StatusOK {
		t.Fatalf("email-code: %d %s", sendCode.Code, sendCode.Body.String())
	}
	code := mailer.lastCode(t, email)

	register := requestJSON(t, server, http.MethodPost, "/api/auth/register",
		map[string]string{"username": "读者", "email": email, "password": password, "code": code}, nil)
	if register.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", register.Code, register.Body.String())
	}
	cookie := firstCookie(t, register)

	session := requestJSON(t, server, http.MethodGet, "/api/auth/session", nil, cookie)
	if session.Code != http.StatusOK {
		t.Fatalf("session: %d", session.Code)
	}
	var sessionPayload struct {
		User struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	decodeInto(t, session, &sessionPayload)
	if sessionPayload.User.Username != "读者" || sessionPayload.User.Role != RoleUser {
		t.Fatalf("session user: %+v", sessionPayload.User)
	}

	const noteID = "math/线性代数/矩阵"
	save := requestJSON(t, server, http.MethodPut, "/api/me/reading",
		map[string]any{"noteId": noteID, "completed": true, "read": true}, cookie)
	if save.Code != http.StatusOK {
		t.Fatalf("save progress: %d %s", save.Code, save.Body.String())
	}
	favorite := requestJSON(t, server, http.MethodPut, "/api/me/favorites", map[string]string{"noteId": noteID}, cookie)
	if favorite.Code != http.StatusOK {
		t.Fatalf("favorite: %d %s", favorite.Code, favorite.Body.String())
	}
	state := requestJSON(t, server, http.MethodGet, "/api/me/reading", nil, cookie)
	if state.Code != http.StatusOK {
		t.Fatalf("reading state: %d", state.Code)
	}
	var reading readingState
	decodeInto(t, state, &reading)
	if len(reading.Progress) != 1 || reading.Progress[0].NoteID != noteID || !reading.Progress[0].Completed {
		t.Fatalf("progress: %+v", reading.Progress)
	}
	if len(reading.Favorites) != 1 || reading.Favorites[0].NoteID != noteID {
		t.Fatalf("favorites: %+v", reading.Favorites)
	}

	// 已注册邮箱不应再收到注册验证码
	duplicate := requestJSON(t, server, http.MethodPost, "/api/auth/email-code",
		map[string]string{"email": email, "purpose": EmailPurposeRegister}, nil)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate email-code: %d %s", duplicate.Code, duplicate.Body.String())
	}

	resetCodeRequest := requestJSON(t, server, http.MethodPost, "/api/auth/email-code",
		map[string]string{"email": email, "purpose": EmailPurposeReset}, nil)
	if resetCodeRequest.Code != http.StatusOK {
		t.Fatalf("reset code: %d %s", resetCodeRequest.Code, resetCodeRequest.Body.String())
	}
	resetCode := mailer.lastCode(t, email)
	const newPassword = "ReaderPass2"
	reset := requestJSON(t, server, http.MethodPost, "/api/auth/password/forgot",
		map[string]string{"email": email, "code": resetCode, "password": newPassword}, nil)
	if reset.Code != http.StatusOK {
		t.Fatalf("reset password: %d %s", reset.Code, reset.Body.String())
	}
	staleSession := requestJSON(t, server, http.MethodGet, "/api/auth/session", nil, cookie)
	var stalePayload struct {
		User any `json:"user"`
	}
	decodeInto(t, staleSession, &stalePayload)
	if stalePayload.User != nil {
		t.Fatal("sessions must be revoked after password reset")
	}
	login := requestJSON(t, server, http.MethodPost, "/api/auth/login",
		map[string]string{"identifier": email, "password": newPassword}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login with new password: %d %s", login.Code, login.Body.String())
	}
	badLogin := requestJSON(t, server, http.MethodPost, "/api/auth/login",
		map[string]string{"identifier": email, "password": password}, nil)
	if badLogin.Code != http.StatusUnauthorized {
		t.Fatalf("old password must fail: %d %s", badLogin.Code, badLogin.Body.String())
	}
}

func TestFirstRegisteredUserBecomesAdmin(t *testing.T) {
	mailer := &capturingMailer{}
	config := integrationConfig(t, freshDatabase(t))
	config.Mailer = mailer
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	// 没有环境变量引导的管理员，首个注册用户应自动成为管理员。
	if err := server.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}

	register := func(username, email, password string) *User {
		t.Helper()
		code := requestJSON(t, server, http.MethodPost, "/api/auth/email-code",
			map[string]string{"email": email, "purpose": EmailPurposeRegister}, nil)
		if code.Code != http.StatusOK {
			t.Fatalf("email-code %s: %d %s", email, code.Code, code.Body.String())
		}
		response := requestJSON(t, server, http.MethodPost, "/api/auth/register",
			map[string]string{"username": username, "email": email, "password": password, "code": mailer.lastCode(t, email)}, nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("register %s: %d %s", username, response.Code, response.Body.String())
		}
		var payload struct {
			User User `json:"user"`
		}
		decodeInto(t, response, &payload)
		return &payload.User
	}

	first := register("owner", "owner@example.com", "OwnerPass123")
	if first.Role != RoleAdmin {
		t.Fatalf("first user should be admin, got %q", first.Role)
	}
	second := register("reader", "reader@example.com", "ReaderPass123")
	if second.Role != RoleUser {
		t.Fatalf("second user should be a normal user, got %q", second.Role)
	}
}

func TestAdminBootstrapAndUserManagement(t *testing.T) {
	mailer := &capturingMailer{}
	config := integrationConfig(t, freshDatabase(t))
	config.Mailer = mailer
	config.AdminUser = "admin"
	config.AdminPassword = "AdminPass123"
	config.AdminEmail = "admin@example.com"
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}

	login := requestJSON(t, server, http.MethodPost, "/api/auth/login",
		map[string]string{"identifier": "admin", "password": "AdminPass123"}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("admin login: %d %s", login.Code, login.Body.String())
	}
	adminCookie := firstCookie(t, login)

	list := requestJSON(t, server, http.MethodGet, "/api/admin/users", nil, adminCookie)
	if list.Code != http.StatusOK {
		t.Fatalf("admin users: %d %s", list.Code, list.Body.String())
	}
	var usersPayload struct {
		Users []struct {
			ID       string `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"users"`
		Total int `json:"total"`
	}
	decodeInto(t, list, &usersPayload)
	if usersPayload.Total != 1 || len(usersPayload.Users) != 1 || usersPayload.Users[0].Role != RoleAdmin {
		t.Fatalf("bootstrap users: %+v", usersPayload)
	}
	adminID := usersPayload.Users[0].ID

	// 注册第二个用户并提升为管理员
	const email = "member@example.com"
	codeRequest := requestJSON(t, server, http.MethodPost, "/api/auth/email-code",
		map[string]string{"email": email, "purpose": EmailPurposeRegister}, nil)
	if codeRequest.Code != http.StatusOK {
		t.Fatalf("member code: %d %s", codeRequest.Code, codeRequest.Body.String())
	}
	register := requestJSON(t, server, http.MethodPost, "/api/auth/register",
		map[string]string{"username": "成员", "email": email, "password": "MemberPass1", "code": mailer.lastCode(t, email)}, nil)
	if register.Code != http.StatusCreated {
		t.Fatalf("member register: %d %s", register.Code, register.Body.String())
	}
	memberCookie := firstCookie(t, register)
	session := requestJSON(t, server, http.MethodGet, "/api/auth/session", nil, memberCookie)
	var memberSession struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	decodeInto(t, session, &memberSession)
	memberID := memberSession.User.ID

	promote := requestJSON(t, server, http.MethodPatch, "/api/admin/users/"+memberID, map[string]string{"role": RoleAdmin}, adminCookie)
	if promote.Code != http.StatusOK {
		t.Fatalf("promote member: %d %s", promote.Code, promote.Body.String())
	}

	selfDemote := requestJSON(t, server, http.MethodPatch, "/api/admin/users/"+adminID, map[string]string{"role": RoleUser}, adminCookie)
	if selfDemote.Code != http.StatusBadRequest {
		t.Fatalf("self demote must be rejected: %d %s", selfDemote.Code, selfDemote.Body.String())
	}
	selfDelete := requestJSON(t, server, http.MethodDelete, "/api/admin/users/"+adminID, nil, adminCookie)
	if selfDelete.Code != http.StatusBadRequest {
		t.Fatalf("self delete must be rejected: %d %s", selfDelete.Code, selfDelete.Body.String())
	}
	remove := requestJSON(t, server, http.MethodDelete, "/api/admin/users/"+memberID, nil, adminCookie)
	if remove.Code != http.StatusOK {
		t.Fatalf("delete member: %d %s", remove.Code, remove.Body.String())
	}

	// 公式元数据只对管理员开放，避免整批 TeX 源码随静态资源外泄。
	nativePath := "/api/admin/note/native?id=" + url.QueryEscape("math/线性代数/矩阵")
	anonymousNative := requestJSON(t, server, http.MethodGet, nativePath, nil, nil)
	if anonymousNative.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous native: %d %s", anonymousNative.Code, anonymousNative.Body.String())
	}
	native := requestJSON(t, server, http.MethodGet, nativePath, nil, adminCookie)
	if native.Code != http.StatusOK {
		t.Fatalf("admin native: %d %s", native.Code, native.Body.String())
	}
	if !strings.Contains(native.Body.String(), `"math"`) {
		t.Fatalf("native payload missing math map: %s", native.Body.String())
	}
	invalidNative := requestJSON(t, server, http.MethodGet, "/api/admin/note/native?id=../../etc/passwd", nil, adminCookie)
	if invalidNative.Code != http.StatusBadRequest {
		t.Fatalf("invalid native id: %d %s", invalidNative.Code, invalidNative.Body.String())
	}

	// 管理员重置自己的密码后，当前会话必须立即失效。
	resetPassword := requestJSON(t, server, http.MethodPost, "/api/admin/users/"+adminID+"/password", map[string]string{}, adminCookie)
	if resetPassword.Code != http.StatusOK {
		t.Fatalf("admin reset password: %d %s", resetPassword.Code, resetPassword.Body.String())
	}
	revoked := requestJSON(t, server, http.MethodGet, "/api/admin/users", nil, adminCookie)
	if revoked.Code != http.StatusUnauthorized {
		t.Fatalf("session must be revoked after password reset: %d %s", revoked.Code, revoked.Body.String())
	}
}
