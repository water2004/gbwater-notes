package app

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"math/big"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	EmailPurposeRegister = "register"
	EmailPurposeReset    = "reset"

	emailCodeTTL         = 10 * time.Minute
	emailCodeMaxAttempts = 5
	emailCodeCooldown    = 60 * time.Second
	emailCodeWindow      = time.Hour
	emailCodeWindowLimit = 5
)

type EmailCodes struct{ db *sql.DB }

func NewEmailCodes(db *sql.DB) *EmailCodes { return &EmailCodes{db: db} }

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func emailCode() (string, error) {
	max := big.NewInt(1000000)
	value, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	digits := value.String()
	for len(digits) < 6 {
		digits = "0" + digits
	}
	return digits, nil
}

// reserve 在同一个事务中检查邮箱限额并登记本次发送请求。
func (s *EmailCodes) reserve(ctx context.Context, email, purpose string, deliver bool) (int64, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	if err := lockEmail(ctx, tx, email); err != nil {
		return 0, "", err
	}

	now := time.Now()
	var oldest, latest sql.NullTime
	var count int
	err = tx.QueryRowContext(ctx, `SELECT MIN(created_at), MAX(created_at) FILTER (WHERE purpose = $3), COUNT(*) FROM email_codes
		WHERE email = $1 AND created_at > $2`, email, now.Add(-emailCodeWindow), purpose).
		Scan(&oldest, &latest, &count)
	if err != nil {
		return 0, "", err
	}
	if latest.Valid {
		if retry := latest.Time.Add(emailCodeCooldown).Sub(now); retry > 0 {
			return 0, "", rateLimited("cooldown", "请等待 "+retryAfterSeconds(retry)+" 秒后再获取验证码", retry)
		}
	}
	if count >= emailCodeWindowLimit {
		return 0, "", rateLimited("rate_limited", "该邮箱获取验证码过于频繁，请稍后再试", oldest.Time.Add(emailCodeWindow).Sub(now))
	}
	var code string
	var digest []byte
	if deliver {
		code, err = emailCode()
		if err != nil {
			return 0, "", err
		}
		digest, err = bcrypt.GenerateFromPassword([]byte(code), passwordCost)
		if err != nil {
			return 0, "", err
		}
	}
	var id int64
	err = tx.QueryRowContext(ctx, `INSERT INTO email_codes(email, purpose, code_hash, attempts, created_at, expires_at)
		VALUES($1,$2,$3,0,$4,$5) RETURNING id`, email, purpose, string(digest), now, now.Add(emailCodeTTL)).Scan(&id)
	if err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return id, code, nil
}

func lockEmail(ctx context.Context, tx *sql.Tx, email string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, email)
	return err
}

func (s *EmailCodes) revoke(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM email_codes WHERE id = $1 AND consumed_at IS NULL`, id)
	return err
}

// Consume 串行核对同一邮箱的验证码，只接受对应场景最新的一条。
func (s *EmailCodes) Consume(ctx context.Context, email, purpose, code string) (bool, error) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false, nil
	}
	if _, err := strconv.ParseUint(code, 10, 32); err != nil {
		return false, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := lockEmail(ctx, tx, email); err != nil {
		return false, err
	}
	var id int64
	var digest string
	var attempts int
	var expiry time.Time
	var consumed sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id, code_hash, attempts, expires_at, consumed_at FROM email_codes
		WHERE email = $1 AND purpose = $2 ORDER BY id DESC LIMIT 1 FOR UPDATE`, email, purpose).
		Scan(&id, &digest, &attempts, &expiry, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if digest == "" || consumed.Valid || attempts >= emailCodeMaxAttempts || !expiry.After(time.Now()) {
		return false, nil
	}
	if bcrypt.CompareHashAndPassword([]byte(digest), []byte(code)) != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE email_codes SET attempts = attempts + 1 WHERE id = $1`, id); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE email_codes SET consumed_at = $2 WHERE id = $1`, id, time.Now()); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *EmailCodes) DeleteExpired(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM email_codes WHERE created_at <= $1`, time.Now().Add(-emailCodeWindow))
	return err
}
