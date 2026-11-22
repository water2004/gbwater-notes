package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

const sessionCookieName = "gbwater_session"

// sessionTouchInterval 控制滑动过期的写放大：只有距上次活动超过该间隔才续期。
const sessionTouchInterval = time.Hour

type Session struct {
	TokenHash  string
	UserID     string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
	UserAgent  string
	IP         string
}

type Sessions struct{ db *sql.DB }

func NewSessions(db *sql.DB) *Sessions { return &Sessions{db: db} }

func sessionToken() (raw, hash string, err error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buffer)
	return raw, hashSessionToken(raw), nil
}

func hashSessionToken(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

func (s *Sessions) Create(ctx context.Context, userID string, ttl time.Duration, userAgent, ip string) (string, error) {
	raw, hash, err := sessionToken()
	if err != nil {
		return "", err
	}
	now := time.Now()
	_, err = s.db.ExecContext(ctx, `INSERT INTO user_sessions(token_hash, user_id, created_at, last_seen_at, expires_at, user_agent, ip)
		VALUES($1,$2,$3,$4,$5,$6,$7)`, hash, userID, now, now, now.Add(ttl), truncate(userAgent, 300), truncate(ip, 64))
	if err != nil {
		return "", err
	}
	return raw, nil
}

// Authenticate 用原始令牌换出用户；同时完成过期清理与滑动续期。
func (s *Sessions) Authenticate(ctx context.Context, raw string, ttl time.Duration) (*User, *Session, error) {
	if raw == "" {
		return nil, nil, nil
	}
	hash := hashSessionToken(raw)
	row := s.db.QueryRowContext(ctx, `SELECT s.token_hash, s.user_id, s.created_at, s.last_seen_at, s.expires_at, s.user_agent, s.ip,
		u.id, u.email, u.email_verified_at, u.username, u.password_hash, u.role, u.status, u.created_at, u.updated_at, u.last_login_at
		FROM user_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1`, hash)
	session := Session{}
	var user User
	var verified, lastLogin sql.NullTime
	err := row.Scan(&session.TokenHash, &session.UserID, &session.CreatedAt, &session.LastSeenAt, &session.ExpiresAt,
		&session.UserAgent, &session.IP,
		&user.ID, &user.Email, &verified, &user.Username, &user.PasswordHash, &user.Role, &user.Status,
		&user.CreatedAt, &user.UpdatedAt, &lastLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if verified.Valid {
		value := verified.Time
		user.EmailVerifiedAt = &value
	}
	if lastLogin.Valid {
		value := lastLogin.Time
		user.LastLoginAt = &value
	}
	now := time.Now()
	if !session.ExpiresAt.After(now) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE token_hash = $1`, hash)
		return nil, nil, nil
	}
	if !user.IsActive() {
		return nil, nil, nil
	}
	if now.Sub(session.LastSeenAt) >= sessionTouchInterval {
		expires := now.Add(ttl)
		_, err := s.db.ExecContext(ctx, `UPDATE user_sessions SET last_seen_at = $2, expires_at = $3 WHERE token_hash = $1`, hash, now, expires)
		if err == nil {
			session.LastSeenAt = now
			session.ExpiresAt = expires
		}
	}
	return &user, &session, nil
}

func (s *Sessions) Delete(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE token_hash = $1`, hashSessionToken(raw))
	return err
}

func (s *Sessions) DeleteForUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE user_id = $1`, userID)
	return err
}

// DeleteOthers 在改密后保留当前会话，踢掉其它设备。
func (s *Sessions) DeleteOthers(ctx context.Context, userID, keepRaw string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE user_id = $1 AND token_hash <> $2`,
		userID, hashSessionToken(keepRaw))
	return err
}

func (s *Sessions) DeleteExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_sessions WHERE expires_at < $1`, time.Now())
	return err
}

func setSessionCookie(w http.ResponseWriter, raw string, ttl time.Duration, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func sessionTokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
