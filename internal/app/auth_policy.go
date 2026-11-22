package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AuthPolicy struct {
	RateEnabled       bool     `json:"rateEnabled"`
	RateAttempts      int      `json:"rateAttempts"`
	RateWindowMinutes int      `json:"rateWindowMinutes"`
	EmailMode         string   `json:"emailMode"`
	EmailDomains      []string `json:"emailDomains"`
}

type AuthPolicyStore struct {
	db      *sql.DB
	mu      sync.RWMutex
	current AuthPolicy
}

func newAuthPolicyStore(db *sql.DB) *AuthPolicyStore {
	return &AuthPolicyStore{db: db, current: AuthPolicy{
		RateEnabled: true, RateAttempts: 5, RateWindowMinutes: 15,
		EmailMode: "disabled", EmailDomains: []string{},
	}}
}

func (s *AuthPolicyStore) load(ctx context.Context) error {
	var policy AuthPolicy
	var domains []byte
	err := s.db.QueryRowContext(ctx, `SELECT rate_enabled, rate_attempts, rate_window_minutes, email_mode, array_to_json(email_domains)
		FROM auth_policy WHERE id = 1`).Scan(&policy.RateEnabled, &policy.RateAttempts, &policy.RateWindowMinutes, &policy.EmailMode, &domains)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(domains, &policy.EmailDomains); err != nil {
		return err
	}
	s.mu.Lock()
	s.current = policy
	s.mu.Unlock()
	return nil
}

func (s *AuthPolicyStore) snapshot() AuthPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *AuthPolicyStore) save(ctx context.Context, policy AuthPolicy) (AuthPolicy, error) {
	policy, err := normalizeAuthPolicy(policy)
	if err != nil {
		return AuthPolicy{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.ExecContext(ctx, `INSERT INTO auth_policy (id, rate_enabled, rate_attempts, rate_window_minutes, email_mode, email_domains)
		VALUES (1, $1, $2, $3, $4, $5) ON CONFLICT (id) DO UPDATE SET
		rate_enabled = EXCLUDED.rate_enabled, rate_attempts = EXCLUDED.rate_attempts,
		rate_window_minutes = EXCLUDED.rate_window_minutes, email_mode = EXCLUDED.email_mode, email_domains = EXCLUDED.email_domains`,
		policy.RateEnabled, policy.RateAttempts, policy.RateWindowMinutes, policy.EmailMode, policy.EmailDomains)
	if err != nil {
		return AuthPolicy{}, err
	}
	s.current = policy
	return policy, nil
}

var emailDomainLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func normalizeAuthPolicy(policy AuthPolicy) (AuthPolicy, error) {
	invalid := func(message string) (AuthPolicy, error) {
		return AuthPolicy{}, apiErr(http.StatusBadRequest, "invalid_auth_policy", message)
	}
	if policy.RateAttempts < 1 || policy.RateAttempts > 100 || policy.RateWindowMinutes < 1 || policy.RateWindowMinutes > 1440 {
		return invalid("限流次数须在 1 到 100 之间，时间窗口须在 1 到 1440 分钟之间")
	}
	switch policy.EmailMode {
	case "disabled", "allowlist", "denylist":
	default:
		return invalid("邮箱限制模式无效")
	}
	if len(policy.EmailDomains) > 256 {
		return invalid("邮箱域名最多填写 256 个")
	}
	domains := make([]string, 0, len(policy.EmailDomains))
	seen := make(map[string]bool, len(policy.EmailDomains))
	for _, input := range policy.EmailDomains {
		domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(input)), "@")
		labels := strings.Split(domain, ".")
		if len(domain) > 253 || len(labels) < 2 {
			return invalid("请输入完整的邮箱域名，例如 example.com")
		}
		for _, label := range labels {
			if !emailDomainLabel.MatchString(label) {
				return invalid("请输入完整的邮箱域名，例如 example.com")
			}
		}
		if !seen[domain] {
			domains = append(domains, domain)
			seen[domain] = true
		}
	}
	if policy.EmailMode == "allowlist" && len(domains) == 0 {
		return invalid("启用邮箱白名单时至少填写一个域名")
	}
	policy.EmailDomains = domains
	return policy, nil
}

func (p AuthPolicy) checkEmail(email string) error {
	_, domain, _ := strings.Cut(email, "@")
	matched := slices.Contains(p.EmailDomains, domain)
	if p.EmailMode == "allowlist" && !matched {
		return apiErr(http.StatusBadRequest, "email_domain_denied", "请使用以下域名的邮箱注册："+strings.Join(p.EmailDomains, "、"))
	}
	if p.EmailMode == "denylist" && matched {
		return apiErr(http.StatusBadRequest, "email_domain_denied", "此邮箱域名暂不支持注册，请换一个邮箱")
	}
	return nil
}

func (p AuthPolicy) checkRate(limiter *Limiter, key string) error {
	if !p.RateEnabled {
		return nil
	}
	key += ":window:" + strconv.Itoa(p.RateWindowMinutes)
	allowed, retry := limiter.Allow(key, p.RateAttempts, time.Duration(p.RateWindowMinutes)*time.Minute)
	if !allowed {
		return rateLimited("rate_limited", "操作过于频繁，请在 "+retryAfterSeconds(retry)+" 秒后重试", retry)
	}
	return nil
}

func (s *Server) limitAuth(scope string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := "auth:" + scope + ":ip:" + clientIP(r, s.config.TrustedProxies)
		if err := s.authPolicy.snapshot().checkRate(s.limiter, key); err != nil {
			fail(w, err)
			return
		}
		next(w, r)
	}
}

type authPolicyResponse struct {
	Policy   AuthPolicy `json:"policy"`
	MailMode string     `json:"mailMode"`
	MailTLS  string     `json:"mailTls,omitempty"`
}

func (s *Server) authPolicyResponse(policy AuthPolicy) authPolicyResponse {
	response := authPolicyResponse{Policy: policy, MailMode: s.config.Mail.Mode}
	if s.config.Mail.Mode == "smtp" {
		response.MailTLS = s.config.Mail.TLS
	}
	return response
}

func (s *Server) getAuthPolicy(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.authPolicyResponse(s.authPolicy.snapshot()))
}

func (s *Server) saveAuthPolicy(w http.ResponseWriter, r *http.Request) {
	var input AuthPolicy
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}
	policy, err := s.authPolicy.save(r.Context(), input)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.authPolicyResponse(policy))
}
