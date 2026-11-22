package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Server struct {
	config      Config
	db          *sql.DB
	ownsDB      bool
	store       *ContentStore
	assets      *AssetStore
	users       *Users
	sessions    *Sessions
	codes       *EmailCodes
	reading     *Reading
	annotations *Annotations
	settings    *UserSettingsStore
	auth        *AuthService
	authPolicy  *AuthPolicyStore
	limiter     *Limiter
	history     *History
	builds      *BuildManager
	search      *SearchClient
	mux         *http.ServeMux
}

func NewServer(config Config) (*Server, error) {
	db, ownsDB, err := prepareDatabase(config)
	if err != nil {
		return nil, err
	}
	limiter := NewLimiter()
	mailer := config.Mailer
	if mailer == nil {
		mailer = newMailer(config.Mail)
	}
	users := NewUsers(db)
	sessions := NewSessions(db)
	codes := NewEmailCodes(db)
	authPolicy := newAuthPolicyStore(db)
	server := &Server{
		config:      config,
		db:          db,
		ownsDB:      ownsDB,
		store:       NewContentStore(config.ContentDir()),
		assets:      &AssetStore{root: config.Root},
		users:       users,
		sessions:    sessions,
		codes:       codes,
		reading:     NewReading(db),
		annotations: NewAnnotations(db),
		settings:    NewUserSettingsStore(db),
		auth:        NewAuthService(config, users, sessions, codes, authPolicy, limiter, mailer),
		authPolicy:  authPolicy,
		limiter:     limiter,
		history:     NewHistory(db),
		builds:      NewBuildManager(config.Root, config.DataDir(), config.BuildTimeout, config.BasePath),
		search:      NewSearchClient(config.MeiliURL, config.MeiliKey),
		mux:         http.NewServeMux(),
	}
	server.store.record = server.history.RecordRevision
	server.builds.SetOnFinish(func(status BuildStatus) {
		if err := server.history.RecordBuild(status); err != nil {
			log.Printf("record build history: %v", err)
		}
		if status.State == "success" {
			server.SyncContent()
		}
	})
	server.routes()
	return server, nil
}

// prepareDatabase 打开并初始化连接池。注入的 *sql.DB 视为调用方已准备好的测试库，
// 此时不再执行建表语句，避免测试需要为 DDL 写期望。
func prepareDatabase(config Config) (*sql.DB, bool, error) {
	if config.DB != nil {
		return config.DB, false, nil
	}
	if config.DatabaseURL == "" {
		return nil, false, errors.New("请配置 GBWATER_DATABASE_URL")
	}
	db, err := openSchemaDatabase(config.DatabaseURL)
	return db, err == nil, err
}

// Bootstrap 在服务启动时补齐首个管理员并清理过期凭证。
func (s *Server) Bootstrap(ctx context.Context) error {
	if err := s.authPolicy.load(ctx); err != nil {
		return err
	}
	created, err := s.auth.BootstrapAdmin(ctx)
	if err != nil {
		return err
	}
	if created {
		log.Printf("已根据环境变量创建管理员账号 %q", s.config.AdminUser)
	} else if s.config.AdminPassword == "" {
		log.Print("数据库里还没有管理员：第一个注册的用户将自动成为管理员")
	}
	if err := s.sessions.DeleteExpired(ctx); err != nil {
		log.Printf("cleanup sessions: %v", err)
	}
	if err := s.codes.DeleteExpired(ctx); err != nil {
		log.Printf("cleanup email codes: %v", err)
	}
	return nil
}

func (s *Server) Close() error {
	if s.ownsDB && s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Server) SyncContent() {
	notes, err := s.store.List()
	if err != nil {
		log.Printf("list content for sync: %v", err)
		return
	}
	documents := []SearchDocument{}
	drafts := []string{}
	for _, note := range notes {
		document, err := s.store.Read(note.ID)
		if err != nil {
			log.Printf("read content for sync: %v", err)
			continue
		}
		if err := s.history.IndexDocument(document); err != nil {
			log.Printf("index PostgreSQL content: %v", err)
			continue
		}
		if note.Draft {
			drafts = append(drafts, note.Slug)
		} else {
			documents = append(documents, searchDocument(document))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := s.search.IndexDocuments(ctx, documents); err != nil {
		log.Printf("index Meilisearch content: %v", err)
	}
	if err := s.search.DeleteDocuments(ctx, drafts); err != nil {
		log.Printf("remove Meilisearch drafts: %v", err)
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// 公开内容接口
	s.mux.HandleFunc("GET /api/search", s.requireUser(s.searchNotes))
	s.mux.HandleFunc("POST /api/public/note-view", s.recordNoteView)
	s.mux.HandleFunc("GET /api/public/popular-notes", s.popularNotes)

	// 账号：注册、登录、会话、找回密码
	s.mux.HandleFunc("POST /api/auth/register", s.limitAuth("register", s.handleRegister))
	s.mux.HandleFunc("POST /api/auth/login", s.limitAuth("login", s.handleLogin))
	s.mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/auth/session", s.handleSession)
	s.mux.HandleFunc("POST /api/auth/email-code", s.limitAuth("email-code", s.handleEmailCode))
	s.mux.HandleFunc("POST /api/auth/password/forgot", s.limitAuth("reset", s.handleForgotPassword))
	s.mux.HandleFunc("POST /api/auth/password/change", s.requireUser(s.handleChangePassword))
	s.mux.HandleFunc("PATCH /api/auth/profile", s.requireUser(s.handleUpdateProfile))

	// 阅读进度与收藏
	s.mux.HandleFunc("GET /api/me/reading", s.requireUser(s.handleReadingState))
	s.mux.HandleFunc("PUT /api/me/reading", s.requireUser(s.handleSaveProgress))
	s.mux.HandleFunc("DELETE /api/me/reading", s.requireUser(s.handleDeleteProgress))
	s.mux.HandleFunc("POST /api/me/reading/merge", s.requireUser(s.handleMergeProgress))
	s.mux.HandleFunc("PUT /api/me/favorites", s.requireUser(s.handleAddFavorite))
	s.mux.HandleFunc("DELETE /api/me/favorites", s.requireUser(s.handleRemoveFavorite))
	s.mux.HandleFunc("POST /api/me/favorites/merge", s.requireUser(s.handleMergeFavorites))

	// 划线、书签与单篇便条
	s.mux.HandleFunc("GET /api/me/annotations", s.requireUser(s.listAnnotations))
	s.mux.HandleFunc("GET /api/me/annotations/summary", s.requireUser(s.annotationSummary))
	s.mux.HandleFunc("POST /api/me/highlights", s.requireUser(s.createHighlight))
	s.mux.HandleFunc("DELETE /api/me/highlights/{id}", s.requireUser(s.deleteHighlight))
	s.mux.HandleFunc("POST /api/me/bookmarks", s.requireUser(s.createBookmark))
	s.mux.HandleFunc("PATCH /api/me/bookmarks/{id}", s.requireUser(s.updateBookmark))
	s.mux.HandleFunc("DELETE /api/me/bookmarks/{id}", s.requireUser(s.deleteBookmark))
	s.mux.HandleFunc("PUT /api/me/memo", s.requireUser(s.saveMemo))
	s.mux.HandleFunc("GET /api/me/settings", s.requireUser(s.getSettings))
	s.mux.HandleFunc("PUT /api/me/settings", s.requireUser(s.saveSettings))
	s.mux.HandleFunc("POST /api/me/sticky-notes", s.requireUser(s.createStickyNote))
	s.mux.HandleFunc("PATCH /api/me/sticky-notes/{id}", s.requireUser(s.updateStickyNote))
	s.mux.HandleFunc("DELETE /api/me/sticky-notes/{id}", s.requireUser(s.deleteStickyNote))

	// 后台管理接口，统一使用 /api/auth/* 建立的会话
	s.mux.HandleFunc("GET /api/admin/overview", s.requireAdmin(s.overview))
	s.mux.HandleFunc("GET /api/admin/site-settings", s.requireAdmin(s.getSiteSettings))
	s.mux.HandleFunc("PUT /api/admin/site-settings", s.requireAdmin(s.updateSiteSettings))
	s.mux.HandleFunc("GET /api/admin/auth-policy", s.requireAdmin(s.getAuthPolicy))
	s.mux.HandleFunc("PUT /api/admin/auth-policy", s.requireAdmin(s.saveAuthPolicy))
	s.mux.HandleFunc("GET /api/admin/notes", s.requireAdmin(s.notes))
	s.mux.HandleFunc("POST /api/admin/notes", s.requireAdmin(s.createNote))
	s.mux.HandleFunc("GET /api/admin/assets", s.requireAdmin(s.listAssets))
	s.mux.HandleFunc("POST /api/admin/assets", s.requireAdmin(s.uploadAsset))
	s.mux.HandleFunc("PATCH /api/admin/assets", s.requireAdmin(s.moveAsset))
	s.mux.HandleFunc("GET /api/admin/asset/content", s.requireAdmin(s.serveAsset))
	s.mux.HandleFunc("GET /api/admin/asset/references", s.requireAdmin(s.listAssetReferences))
	s.mux.HandleFunc("GET /api/admin/revisions", s.requireAdmin(s.revisions))
	s.mux.HandleFunc("GET /api/admin/revisions/{id}", s.requireAdmin(s.readRevision))
	s.mux.HandleFunc("GET /api/admin/note", s.requireAdmin(s.readNote))
	s.mux.HandleFunc("GET /api/admin/note/download", s.requireAdmin(s.downloadNote))
	s.mux.HandleFunc("PATCH /api/admin/note", s.requireAdmin(s.setNoteDraft))
	s.mux.HandleFunc("GET /api/admin/note/native", s.requireAdmin(s.readNoteNative))
	s.mux.HandleFunc("PUT /api/admin/note", s.requireAdmin(s.updateNote))
	s.mux.HandleFunc("GET /api/admin/build", s.requireAdmin(s.buildStatus))
	s.mux.HandleFunc("GET /api/admin/build/history", s.requireAdmin(s.buildHistory))
	s.mux.HandleFunc("POST /api/admin/build", s.requireAdmin(s.startBuild))
	s.mux.HandleFunc("GET /api/admin/users", s.requireAdmin(s.adminUsers))
	s.mux.HandleFunc("PATCH /api/admin/users/{id}", s.requireAdmin(s.adminUpdateUser))
	s.mux.HandleFunc("DELETE /api/admin/users/{id}", s.requireAdmin(s.adminDeleteUser))
	s.mux.HandleFunc("POST /api/admin/users/{id}/password", s.requireAdmin(s.adminResetPassword))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Cache-Control", "no-store")
	}
	if !safeMethod(r.Method) && !sameOrigin(r) {
		log.Printf("cross-site request rejected: method=%s origin=%q host=%q", r.Method, r.Header.Get("Origin"), r.Host)
		writeError(w, http.StatusForbidden, "cross_site", "跨站请求已被拒绝")
		return
	}
	s.mux.ServeHTTP(w, r)
}

func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// sameOrigin 在请求带 Origin 时校验主机名。开发和部署代理可能改写端口。
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if origin == "null" {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	requestHost := r.Host
	if forwarded := r.Header.Get("X-Forwarded-Host"); forwarded != "" {
		requestHost = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	return strings.EqualFold(hostname(parsed.Host), hostname(requestHost))
}

func hostname(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return strings.Trim(hostport, "[]")
}

type userContextKey struct{}

func (s *Server) currentUser(r *http.Request) *User {
	user, _ := r.Context().Value(userContextKey{}).(*User)
	return user
}

func (s *Server) requestMeta(r *http.Request) requestMeta {
	return requestMeta{UserAgent: truncate(r.UserAgent(), 300), IP: clientIP(r, s.config.TrustedProxies)}
}

// connect 解析会话 Cookie，返回用户与原始令牌。
func (s *Server) connect(r *http.Request) (*User, string, error) {
	raw := sessionTokenFromRequest(r)
	user, _, err := s.sessions.Authenticate(r.Context(), raw, s.config.SessionTTL)
	return user, raw, err
}

func (s *Server) requireUser(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, _, err := s.connect(r)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "会话校验失败")
			return
		}
		if user == nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "请先登录")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userContextKey{}, user)))
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return s.requireUser(func(w http.ResponseWriter, r *http.Request) {
		user := s.currentUser(r)
		if user == nil || !user.IsAdmin() {
			writeError(w, http.StatusForbidden, "forbidden", "需要管理员权限")
			return
		}
		next(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": message, "code": code})
}

// fail 把业务层的 apiError 映射为响应，其余错误统一记日志并返回 500。
func fail(w http.ResponseWriter, err error) {
	if apiErr, ok := asAPIError(err); ok {
		if apiErr.RetryAfter > 0 {
			w.Header().Set("Retry-After", retryAfterSeconds(apiErr.RetryAfter))
		}
		writeError(w, apiErr.Status, apiErr.Code, apiErr.Message)
		return
	}
	log.Printf("request failed: %v", err)
	writeError(w, http.StatusInternalServerError, "server_error", "服务器内部错误")
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("请求必须使用 application/json")
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra json.RawMessage
	err = decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("请求只能包含一个 JSON 值")
}

// throttleUser 按登录用户计数，返回 false 时已经写出 429。
func (s *Server) throttleUser(w http.ResponseWriter, r *http.Request, scope string, limit int, window time.Duration) bool {
	user := s.currentUser(r)
	if user == nil {
		return false
	}
	allowed, retry := s.limiter.Allow("user:"+scope+":"+user.ID, limit, window)
	if allowed {
		return true
	}
	fail(w, rateLimited("rate_limited", "操作过于频繁，请稍后再试", retry))
	return false
}

func (s *Server) searchNotes(w http.ResponseWriter, r *http.Request) {
	if !s.throttleUser(w, r, "search", 30, time.Minute) {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		writeJSON(w, http.StatusOK, map[string]any{"results": []SearchResult{}})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 1200*time.Millisecond)
	defer cancel()
	results, err := s.search.Search(ctx, query, 30)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "search_unavailable", "搜索服务暂时不可用，请稍后重试")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=15")
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

func (s *Server) recordNoteView(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	input.ID = strings.TrimSpace(input.ID)
	if input.ID == "" || len(input.ID) > 512 {
		writeError(w, http.StatusBadRequest, "invalid_note", "笔记 id 无效")
		return
	}
	recorded, err := s.history.RecordNoteView(input.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "analytics_unavailable", "访问统计暂时不可用")
		return
	}
	if !recorded {
		writeError(w, http.StatusNotFound, "note_not_found", "公开笔记不存在")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, map[string]bool{"ok": true})
}

func (s *Server) popularNotes(w http.ResponseWriter, r *http.Request) {
	limit := 6
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			limit = parsed
		}
	}
	notes, err := s.history.PopularNotes(limit)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "analytics_unavailable", "热门笔记暂时不可用")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=30")
	writeJSON(w, http.StatusOK, map[string]any{"notes": notes})
}
