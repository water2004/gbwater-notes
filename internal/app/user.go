package app

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	StatusActive   = "active"
	StatusDisabled = "disabled"
)

var (
	ErrEmailTaken    = errors.New("email already registered")
	ErrUsernameTaken = errors.New("username already taken")
	ErrUserNotFound  = errors.New("user not found")
)

type User struct {
	ID              string     `json:"id"`
	Email           string     `json:"email"`
	EmailVerifiedAt *time.Time `json:"emailVerifiedAt,omitempty"`
	Username        string     `json:"username"`
	PasswordHash    string     `json:"-"`
	Role            string     `json:"role"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	LastLoginAt     *time.Time `json:"lastLoginAt,omitempty"`
}

func (u User) IsAdmin() bool       { return u.Role == RoleAdmin }
func (u User) IsActive() bool      { return u.Status == StatusActive }
func (u User) EmailVerified() bool { return u.EmailVerifiedAt != nil }

const userColumns = `id, email, email_verified_at, username, password_hash, role, status, created_at, updated_at, last_login_at`

type Users struct{ db *sql.DB }

func NewUsers(db *sql.DB) *Users { return &Users{db: db} }

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var user User
	var verified, lastLogin sql.NullTime
	err := row.Scan(&user.ID, &user.Email, &verified, &user.Username, &user.PasswordHash,
		&user.Role, &user.Status, &user.CreatedAt, &user.UpdatedAt, &lastLogin)
	if err != nil {
		return User{}, err
	}
	if verified.Valid {
		value := verified.Time
		user.EmailVerifiedAt = &value
	}
	if lastLogin.Valid {
		value := lastLogin.Time
		user.LastLoginAt = &value
	}
	return user, nil
}

func isUniqueViolation(err error) (string, bool) {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		return "", false
	}
	return pgErr.ConstraintName, true
}

func (s *Users) Create(ctx context.Context, user User) (User, error) {
	now := time.Now()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = user.CreatedAt
	if user.Role == "" {
		user.Role = RoleUser
	}
	if user.Status == "" {
		user.Status = StatusActive
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO users(id, email, email_verified_at, username, password_hash, role, status, created_at, updated_at, last_login_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,NULL)`,
		user.ID, user.Email, user.EmailVerifiedAt, user.Username, user.PasswordHash,
		user.Role, user.Status, user.CreatedAt, user.UpdatedAt)
	if err != nil {
		if constraint, ok := isUniqueViolation(err); ok {
			if strings.Contains(constraint, "email") {
				return User{}, ErrEmailTaken
			}
			if strings.Contains(constraint, "username") {
				return User{}, ErrUsernameTaken
			}
			return User{}, err
		}
		return User{}, err
	}
	return user, nil
}

func (s *Users) get(ctx context.Context, where string, argument any) (*User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE `+where, argument)
	user, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *Users) ByID(ctx context.Context, id string) (*User, error) {
	return s.get(ctx, "id = $1", id)
}

func (s *Users) ByEmail(ctx context.Context, email string) (*User, error) {
	return s.get(ctx, "lower(email) = lower($1)", strings.TrimSpace(email))
}

func (s *Users) ByUsername(ctx context.Context, username string) (*User, error) {
	return s.get(ctx, "lower(username) = lower($1)", strings.TrimSpace(username))
}

// ByIdentifier 支持用邮箱或用户名登录，两者都是大小写不敏感的唯一键。
func (s *Users) ByIdentifier(ctx context.Context, identifier string) (*User, error) {
	identifier = strings.TrimSpace(identifier)
	return s.get(ctx, "(lower(email) = lower($1) OR lower(username) = lower($1))", identifier)
}

func (s *Users) TouchLogin(ctx context.Context, id string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = $2, updated_at = $2 WHERE id = $1`, id, now)
	return err
}

func (s *Users) Rename(ctx context.Context, id, username string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET username = $2, updated_at = $3 WHERE id = $1`, id, username, time.Now())
	if err != nil {
		if constraint, ok := isUniqueViolation(err); ok && strings.Contains(constraint, "username") {
			return ErrUsernameTaken
		}
		return err
	}
	return nil
}

func (s *Users) SetPassword(ctx context.Context, id, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = $2, updated_at = $3 WHERE id = $1`, id, hash, time.Now())
	return err
}

func (s *Users) SetRoleStatus(ctx context.Context, id, role, status string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET role = $2, status = $3, updated_at = $4 WHERE id = $1`,
		id, role, status, time.Now())
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *Users) Delete(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrUserNotFound
	}
	return nil
}

// PromoteIfNoAdmin 让"站点还没有管理员时的第一个注册用户"直接成为管理员。
// 单条 UPDATE 里判断，避免并发注册都拿到管理员。
func (s *Users) PromoteIfNoAdmin(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE users SET role = 'admin', updated_at = $2
		WHERE id = $1 AND NOT EXISTS (SELECT 1 FROM users WHERE role = 'admin' AND id <> $1)`, id, time.Now())
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
func (s *Users) CountAdmins(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'`).Scan(&count)
	return count, err
}

func (s *Users) Count(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// List 返回分页用户与总数，query 同时匹配邮箱与用户名。
func (s *Users) List(ctx context.Context, query string, limit, offset int) ([]User, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	query = strings.TrimSpace(query)
	where := ""
	arguments := []any{}
	if query != "" {
		pattern := "%" + strings.ToLower(query) + "%"
		where = ` WHERE lower(email) LIKE $1 OR lower(username) LIKE $1`
		arguments = append(arguments, pattern)
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`+where, arguments...).Scan(&total); err != nil {
		return nil, 0, err
	}
	pageArguments := append(append([]any{}, arguments...), limit, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users`+where+
		` ORDER BY created_at DESC, id LIMIT $`+strconv.Itoa(len(arguments)+1)+` OFFSET $`+strconv.Itoa(len(arguments)+2),
		pageArguments...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, 0, err
		}
		users = append(users, user)
	}
	return users, total, rows.Err()
}
