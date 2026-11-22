package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func newTestServer(t *testing.T, config Config) (*Server, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	config.DB = db
	if config.Root == "" {
		config.Root = t.TempDir()
	}
	if config.SessionTTL == 0 {
		config.SessionTTL = time.Hour
	}
	if config.BuildTimeout == 0 {
		config.BuildTimeout = time.Minute
	}
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Close()
		_ = db.Close()
	})
	return server, mock
}

func TestHealthAndAnonymousAccess(t *testing.T) {
	server, _ := newTestServer(t, Config{})

	health := httptest.NewRecorder()
	server.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health: %d", health.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/admin/notes", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized: %d %s", unauthorized.Code, unauthorized.Body.String())
	}

	session := httptest.NewRecorder()
	server.ServeHTTP(session, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))
	if session.Code != http.StatusOK {
		t.Fatalf("session: %d", session.Code)
	}
	if !bytes.Contains(session.Body.Bytes(), []byte(`"user":null`)) {
		t.Fatalf("anonymous session payload: %s", session.Body.String())
	}
}

func TestDecodeJSONRequiresOneCompleteValue(t *testing.T) {
	for _, tc := range []struct {
		name, mediaType, body string
		valid                 bool
	}{
		{"valid JSON with charset", "application/json; charset=utf-8", "{\"title\":\"note\"}\n ", true},
		{"missing media type", "", `{"title":"note"}`, false},
		{"wrong media type", "application/jsonp", `{"title":"note"}`, false},
		{"second value", "application/json", `{"title":"note"}{}`, false},
		{"trailing garbage", "application/json", `{"title":"note"} trailing`, false},
		{"unknown field", "application/json", `{"extra":true}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/admin/notes", bytes.NewBufferString(tc.body))
			request.Header.Set("Content-Type", tc.mediaType)
			var input struct {
				Title string `json:"title"`
			}
			err := decodeJSON(httptest.NewRecorder(), request, &input)
			if (err == nil) != tc.valid {
				t.Fatalf("decodeJSON() returned %v, want valid = %t", err, tc.valid)
			}
		})
	}
}

func TestLoginSessionAndLogout(t *testing.T) {
	server, mock := newTestServer(t, Config{AdminUser: "admin", AdminEmail: "admin@example.com", AdminPassword: "test-password"})
	hash, err := hashPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	mock.ExpectQuery(`SELECT id, email, email_verified_at.*FROM users WHERE \(lower\(email\)`).
		WithArgs("admin").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "email_verified_at", "username", "password_hash", "role", "status", "created_at", "updated_at", "last_login_at"}).
			AddRow("user-1", "admin@example.com", now, "admin", hash, "admin", "active", now, now, nil))
	mock.ExpectExec(`INSERT INTO user_sessions`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`UPDATE users SET last_login_at`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(`DELETE FROM user_sessions WHERE expires_at <`).WillReturnResult(sqlmock.NewResult(1, 0))

	login := httptest.NewRecorder()
	body := bytes.NewBufferString(`{"identifier":"admin","password":"test-password"}`)
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", body)
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(login, request)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Name != sessionCookieName {
		t.Fatalf("cookies: %+v", cookies)
	}

	mock.ExpectQuery(`SELECT s.token_hash.*FROM user_sessions s JOIN users u`).
		WithArgs(hashSessionToken(cookies[0].Value)).
		WillReturnRows(sqlmock.NewRows([]string{"token_hash", "user_id", "created_at", "last_seen_at", "expires_at", "user_agent", "ip",
			"id", "email", "email_verified_at", "username", "password_hash", "role", "status", "created_at", "updated_at", "last_login_at"}).
			AddRow(hashSessionToken(cookies[0].Value), "user-1", now, now, now.Add(time.Hour), "test", "127.0.0.1",
				"user-1", "admin@example.com", now, "admin", hash, "admin", "active", now, now, nil))

	authorized := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/admin/notes", nil)
	request.AddCookie(cookies[0])
	server.ServeHTTP(authorized, request)
	if authorized.Code != http.StatusOK {
		t.Fatalf("authorized: %d %s", authorized.Code, authorized.Body.String())
	}

	mock.ExpectExec(`DELETE FROM user_sessions WHERE token_hash`).WillReturnResult(sqlmock.NewResult(1, 1))
	logout := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.AddCookie(cookies[0])
	server.ServeHTTP(logout, request)
	if logout.Code != http.StatusOK {
		t.Fatalf("logout: %d %s", logout.Code, logout.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminRoleIsRequired(t *testing.T) {
	server, mock := newTestServer(t, Config{})
	now := time.Now()
	hash, _ := hashPassword("member-password")
	token := "member-token"
	mock.ExpectQuery(`SELECT s.token_hash.*FROM user_sessions s JOIN users u`).
		WillReturnRows(sqlmock.NewRows([]string{"token_hash", "user_id", "created_at", "last_seen_at", "expires_at", "user_agent", "ip",
			"id", "email", "email_verified_at", "username", "password_hash", "role", "status", "created_at", "updated_at", "last_login_at"}).
			AddRow(hashSessionToken(token), "user-2", now, now, now.Add(time.Hour), "test", "127.0.0.1",
				"user-2", "member@example.com", now, "member", hash, "user", "active", now, now, nil))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("member admin access: %d %s", response.Code, response.Body.String())
	}
}

func TestPublicPagesAreServedByNginx(t *testing.T) {
	server, _ := newTestServer(t, Config{BasePath: "/notes-app/"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/notes-app/", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("public route: %d %q", response.Code, response.Body.String())
	}
}

func TestSameOriginIgnoresPort(t *testing.T) {
	server, _ := newTestServer(t, Config{})
	// 开发时页面在 5173、后端在 8080，端口不同但仍属同源服务器。
	request := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Origin", "http://127.0.0.1:5173")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("same host different port should pass: %d %s", response.Code, response.Body.String())
	}

	crossSite := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	crossSite.Host = "127.0.0.1:8080"
	crossSite.Header.Set("Origin", "https://evil.example")
	blocked := httptest.NewRecorder()
	server.ServeHTTP(blocked, crossSite)
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("cross-site must be rejected: %d", blocked.Code)
	}
}

func TestCrossSiteRequestIsRejected(t *testing.T) {
	server, _ := newTestServer(t, Config{})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"identifier":"x","password":"y"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-site: %d %s", response.Code, response.Body.String())
	}
}

func TestRegisterValidatesBeforeTouchingDatabase(t *testing.T) {
	server, _ := newTestServer(t, Config{AllowRegister: true})
	cases := []struct {
		name string
		body string
		want int
	}{
		{"short password", `{"username":"张三","email":"a@b.com","password":"short","code":"123456"}`, http.StatusBadRequest},
		{"bad username", `{"username":"a b","email":"a@b.com","password":"LongEnough1","code":"123456"}`, http.StatusBadRequest},
		{"bad email", `{"username":"zhangsan","email":"not-an-email","password":"LongEnough1","code":"123456"}`, http.StatusBadRequest},
		{"missing code", `{"username":"zhangsan","email":"a@b.com","password":"LongEnough1"}`, http.StatusBadRequest},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(testCase.body))
			request.Header.Set("Content-Type", "application/json")
			server.ServeHTTP(response, request)
			if response.Code != testCase.want {
				t.Fatalf("status: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestRegistrationCanBeDisabled(t *testing.T) {
	server, _ := newTestServer(t, Config{AllowRegister: false})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(`{"username":"zhangsan","email":"a@b.com","password":"LongEnough1","code":"123456"}`))
	request.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("registration disabled: %d %s", response.Code, response.Body.String())
	}
}
