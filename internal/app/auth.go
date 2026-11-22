package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// apiError 是所有可直接返回给客户端的错误：状态码 + 稳定的错误码 + 中文提示。
type apiError struct {
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
}

func (e apiError) Error() string { return e.Message }

func apiErr(status int, code, message string) apiError {
	return apiError{Status: status, Code: code, Message: message}
}

func asAPIError(err error) (apiError, bool) {
	var target apiError
	if errors.As(err, &target) {
		return target, true
	}
	return apiError{}, false
}

func newID() (string, error) {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func validEmail(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 254 || strings.ContainsAny(value, "\r\n") {
		return false
	}
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value {
		return false
	}
	parts := strings.Split(address.Address, "@")
	return len(parts) == 2 && parts[0] != "" && parts[1] != "" && strings.Contains(parts[1], ".")
}

// usernameRules 返回不满足的规则代码；用户名允许中英文、数字、下划线与连字符。
func usernameRules(username string) []string {
	username = strings.TrimSpace(username)
	if username == "" {
		return []string{"required"}
	}
	rules := []string{}
	count := utf8.RuneCountInString(username)
	if count < 2 {
		rules = append(rules, "min_length")
	}
	if count > 24 {
		rules = append(rules, "max_length")
	}
	for _, char := range username {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == '-' {
			continue
		}
		rules = append(rules, "characters")
		break
	}
	return rules
}

func usernameRuleMessage(rule string) string {
	switch rule {
	case "required":
		return "请输入用户名"
	case "min_length":
		return "用户名至少 2 个字符"
	case "max_length":
		return "用户名不能超过 24 个字符"
	case "characters":
		return "用户名只能包含中英文、数字、下划线与连字符"
	default:
		return "用户名不符合要求"
	}
}

type RegisterInput struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

type LoginInput struct {
	Identifier string `json:"identifier"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Password   string `json:"password"`
}

func (in LoginInput) account() string {
	for _, value := range []string{in.Identifier, in.Email, in.Username} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

type requestMeta struct {
	UserAgent string
	IP        string
}

type AuthService struct {
	config   Config
	users    *Users
	sessions *Sessions
	codes    *EmailCodes
	policy   *AuthPolicyStore
	limiter  *Limiter
	mailer   Mailer
}

func NewAuthService(config Config, users *Users, sessions *Sessions, codes *EmailCodes, policy *AuthPolicyStore, limiter *Limiter, mailer Mailer) *AuthService {
	return &AuthService{config: config, users: users, sessions: sessions, codes: codes, policy: policy, limiter: limiter, mailer: mailer}
}

// SendEmailCode 给已注册邮箱保留找回密码能力，并对不存在的账号执行同样的邮件限额。
func (s *AuthService) SendEmailCode(ctx context.Context, email, purpose string) error {
	email = normalizeEmail(email)
	if !validEmail(email) {
		return apiErr(http.StatusBadRequest, "invalid_email", "请输入有效的邮箱地址")
	}
	if purpose != EmailPurposeRegister && purpose != EmailPurposeReset {
		return apiErr(http.StatusBadRequest, "invalid_purpose", "验证码用途无效")
	}
	if purpose == EmailPurposeRegister {
		if !s.config.AllowRegister {
			return apiErr(http.StatusForbidden, "registration_disabled", "站点暂未开放注册")
		}
		if err := s.policy.snapshot().checkEmail(email); err != nil {
			return err
		}
	}
	if s.mailer == nil {
		return apiErr(http.StatusServiceUnavailable, "mail_unavailable", "验证码邮件服务暂不可用")
	}
	existing, err := s.users.ByEmail(ctx, email)
	if err != nil {
		return err
	}
	if purpose == EmailPurposeRegister && existing != nil {
		return apiErr(http.StatusConflict, "email_taken", "该邮箱已注册，请直接登录")
	}
	deliver := purpose == EmailPurposeRegister || existing != nil
	id, code, err := s.codes.reserve(ctx, email, purpose, deliver)
	if err != nil || !deliver {
		return err
	}
	username := email
	if existing != nil {
		username = existing.Username
	}
	subject, body := verificationMail(s.config.Mail.SiteName, s.config.SiteLink(), username, code, purpose)
	if err := s.mailer.Send(ctx, email, subject, body); err != nil {
		log.Printf("send verification email: %v", err)
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if err := s.codes.revoke(cleanup, id); err != nil {
			log.Printf("revoke undelivered verification code: %v", err)
		}
		return apiErr(http.StatusServiceUnavailable, "mail_unavailable", "验证码发送失败，请稍后重试")
	}
	return nil
}

func (s *AuthService) Register(ctx context.Context, input RegisterInput, meta requestMeta) (*User, string, error) {
	if !s.config.AllowRegister {
		return nil, "", apiErr(http.StatusForbidden, "registration_disabled", "站点暂未开放注册")
	}
	username := strings.TrimSpace(input.Username)
	if rules := usernameRules(username); len(rules) > 0 {
		return nil, "", apiErr(http.StatusBadRequest, "invalid_username", usernameRuleMessage(rules[0]))
	}
	email := normalizeEmail(input.Email)
	if !validEmail(email) {
		return nil, "", apiErr(http.StatusBadRequest, "invalid_email", "请输入有效的邮箱地址")
	}
	if err := s.policy.snapshot().checkEmail(email); err != nil {
		return nil, "", err
	}
	if rules := passwordRules(input.Password); len(rules) > 0 {
		return nil, "", apiErr(http.StatusBadRequest, "invalid_password", passwordRuleMessage(rules[0]))
	}
	if strings.TrimSpace(input.Code) == "" {
		return nil, "", apiErr(http.StatusBadRequest, "code_required", "请填写邮箱验证码")
	}
	// 先做占用检查再消费验证码，避免因为重名让用户白白损失一个验证码。
	if existing, err := s.users.ByUsername(ctx, username); err != nil {
		return nil, "", err
	} else if existing != nil {
		return nil, "", apiErr(http.StatusConflict, "username_taken", "这个用户名已被使用")
	}
	if existing, err := s.users.ByEmail(ctx, email); err != nil {
		return nil, "", err
	} else if existing != nil {
		return nil, "", apiErr(http.StatusConflict, "email_taken", "该邮箱已注册，请直接登录")
	}
	ok, err := s.codes.Consume(ctx, email, EmailPurposeRegister, input.Code)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, "", apiErr(http.StatusBadRequest, "invalid_code", "验证码无效或已过期")
	}
	hash, err := hashPassword(input.Password)
	if err != nil {
		return nil, "", err
	}
	id, err := newID()
	if err != nil {
		return nil, "", err
	}
	now := time.Now()
	user, err := s.users.Create(ctx, User{
		ID: id, Email: email, EmailVerifiedAt: &now, Username: username,
		PasswordHash: hash, Role: RoleUser, Status: StatusActive,
	})
	if err != nil {
		if err == ErrEmailTaken {
			return nil, "", apiErr(http.StatusConflict, "email_taken", "该邮箱已注册，请直接登录")
		}
		if err == ErrUsernameTaken {
			return nil, "", apiErr(http.StatusConflict, "username_taken", "这个用户名已被使用")
		}
		return nil, "", err
	}
	raw, err := s.sessions.Create(ctx, user.ID, s.config.SessionTTL, meta.UserAgent, meta.IP)
	if err != nil {
		return nil, "", err
	}
	// 站点还没有管理员时，第一个注册的用户直接成为管理员。
	if promoted, err := s.users.PromoteIfNoAdmin(ctx, user.ID); err == nil && promoted {
		user.Role = RoleAdmin
	}
	return &user, raw, nil
}

func (s *AuthService) Login(ctx context.Context, input LoginInput, meta requestMeta) (*User, string, error) {
	account := input.account()
	if account == "" || input.Password == "" {
		return nil, "", apiErr(http.StatusBadRequest, "credentials_required", "请输入账号与密码")
	}
	if len(account) > 254 {
		return nil, "", apiErr(http.StatusUnauthorized, "invalid_credentials", "账号或密码错误")
	}
	if err := s.policy.snapshot().checkRate(s.limiter, "auth:login:account:"+strings.ToLower(account)); err != nil {
		return nil, "", err
	}
	user, err := s.users.ByIdentifier(ctx, account)
	if err != nil {
		return nil, "", err
	}
	if user == nil {
		verifyAgainstDummy(input.Password)
		return nil, "", apiErr(http.StatusUnauthorized, "invalid_credentials", "账号或密码错误")
	}
	if !verifyPassword(user.PasswordHash, input.Password) {
		return nil, "", apiErr(http.StatusUnauthorized, "invalid_credentials", "账号或密码错误")
	}
	if !user.IsActive() {
		return nil, "", apiErr(http.StatusForbidden, "account_disabled", "账号已被停用，请联系管理员")
	}
	if !user.EmailVerified() {
		return nil, "", apiErr(http.StatusForbidden, "email_unverified", "请先完成邮箱验证")
	}
	raw, err := s.sessions.Create(ctx, user.ID, s.config.SessionTTL, meta.UserAgent, meta.IP)
	if err != nil {
		return nil, "", err
	}
	if err := s.users.TouchLogin(ctx, user.ID); err != nil {
		return nil, "", err
	}
	_ = s.sessions.DeleteExpired(ctx)
	return user, raw, nil
}

func (s *AuthService) Logout(ctx context.Context, raw string) error {
	return s.sessions.Delete(ctx, raw)
}

func (s *AuthService) ChangePassword(ctx context.Context, user *User, current, next, keepRaw string) error {
	if !verifyPassword(user.PasswordHash, current) {
		return apiErr(http.StatusBadRequest, "invalid_password", "当前密码不正确")
	}
	if rules := passwordRules(next); len(rules) > 0 {
		return apiErr(http.StatusBadRequest, "invalid_password", passwordRuleMessage(rules[0]))
	}
	hash, err := hashPassword(next)
	if err != nil {
		return err
	}
	if err := s.users.SetPassword(ctx, user.ID, hash); err != nil {
		return err
	}
	return s.sessions.DeleteOthers(ctx, user.ID, keepRaw)
}

func (s *AuthService) ResetPassword(ctx context.Context, email, code, password string) error {
	email = normalizeEmail(email)
	if !validEmail(email) {
		return apiErr(http.StatusBadRequest, "invalid_email", "请输入有效的邮箱地址")
	}
	if rules := passwordRules(password); len(rules) > 0 {
		return apiErr(http.StatusBadRequest, "invalid_password", passwordRuleMessage(rules[0]))
	}
	user, err := s.users.ByEmail(ctx, email)
	if err != nil {
		return err
	}
	if user == nil {
		return apiErr(http.StatusBadRequest, "invalid_code", "验证码无效或已过期")
	}
	ok, err := s.codes.Consume(ctx, email, EmailPurposeReset, code)
	if err != nil {
		return err
	}
	if !ok {
		return apiErr(http.StatusBadRequest, "invalid_code", "验证码无效或已过期")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := s.users.SetPassword(ctx, user.ID, hash); err != nil {
		return err
	}
	return s.sessions.DeleteForUser(ctx, user.ID)
}

func (s *AuthService) Rename(ctx context.Context, user *User, username string) (*User, error) {
	username = strings.TrimSpace(username)
	if rules := usernameRules(username); len(rules) > 0 {
		return nil, apiErr(http.StatusBadRequest, "invalid_username", usernameRuleMessage(rules[0]))
	}
	if strings.EqualFold(username, user.Username) {
		return user, nil
	}
	if err := s.users.Rename(ctx, user.ID, username); err != nil {
		if err == ErrUsernameTaken {
			return nil, apiErr(http.StatusConflict, "username_taken", "这个用户名已被使用")
		}
		return nil, err
	}
	updated, err := s.users.ByID(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// BootstrapAdmin 在没有可用管理员时，用配置的账号创建首个管理员。
func (s *AuthService) BootstrapAdmin(ctx context.Context) (bool, error) {
	count, err := s.users.CountAdmins(ctx)
	if err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	if s.config.AdminPassword == "" {
		return false, nil
	}
	hash, err := hashPassword(s.config.AdminPassword)
	if err != nil {
		return false, err
	}
	id, err := newID()
	if err != nil {
		return false, err
	}
	now := time.Now()
	username := s.config.AdminUser
	if rules := usernameRules(username); len(rules) > 0 {
		username = "admin"
	}
	email := normalizeEmail(s.config.AdminEmail)
	if !validEmail(email) {
		email = "admin@gbwater.local"
	}
	_, err = s.users.Create(ctx, User{
		ID: id, Email: email, EmailVerifiedAt: &now,
		Username: username, PasswordHash: hash, Role: RoleAdmin, Status: StatusActive,
	})
	if err != nil {
		if err == ErrEmailTaken || err == ErrUsernameTaken {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
