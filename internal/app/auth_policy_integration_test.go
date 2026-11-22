//go:build integration

package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestAuthPolicyTakesEffectAndPersists(t *testing.T) {
	mailer := &capturingMailer{}
	config := integrationConfig(t, freshDatabase(t))
	config.AdminUser, config.AdminEmail, config.AdminPassword = "admin", "admin@example.com", "FixtureAdmin123"
	config.Mailer = mailer
	server, err := NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := server.Bootstrap(t.Context()); err != nil {
		t.Fatal(err)
	}
	login := requestJSON(t, server, http.MethodPost, "/api/auth/login",
		map[string]string{"identifier": config.AdminUser, "password": config.AdminPassword}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d %s", login.Code, login.Body.String())
	}
	cookie := firstCookie(t, login)
	const path = "/api/admin/auth-policy"
	if response := requestJSON(t, server, http.MethodGet, path, nil, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous policy read: %d", response.Code)
	}
	policy := AuthPolicy{RateEnabled: true, RateAttempts: 10, RateWindowMinutes: 15, EmailMode: "allowlist",
		EmailDomains: []string{" @SCHOOL.EXAMPLE ", "school.example"}}
	if resp := requestJSON(t, server, http.MethodPut, path, policy, cookie); resp.Code != http.StatusOK {
		t.Fatalf("save policy: %d %s", resp.Code, resp.Body.String())
	}

	check := func(body any, path string, status int, code string) {
		t.Helper()
		response := requestJSON(t, server, http.MethodPost, path, body, nil)
		if response.Code != status {
			t.Fatalf("%s: %d %s, want %d", path, response.Code, response.Body.String(), status)
		}
		if code != "" {
			var failure struct {
				Code string `json:"code"`
			}
			decodeInto(t, response, &failure)
			if failure.Code != code {
				t.Fatalf("%s: code %q, want %q", path, failure.Code, code)
			}
		}
		if status == http.StatusTooManyRequests {
			wait, err := strconv.Atoi(response.Header().Get("Retry-After"))
			if err != nil || wait < 1 {
				t.Fatalf("%s: invalid Retry-After: %q", path, response.Header().Get("Retry-After"))
			}
		}
	}
	const codePath = "/api/auth/email-code"
	check(map[string]string{"email": "student@sub.school.example", "purpose": "register"}, codePath, http.StatusBadRequest, "email_domain_denied")
	check(RegisterInput{Username: "reader", Email: "reader@elsewhere.example", Password: "FixtureReader123", Code: "123456"},
		"/api/auth/register", http.StatusBadRequest, "email_domain_denied")
	check(map[string]string{"email": "student@SCHOOL.EXAMPLE", "purpose": "register"}, codePath, http.StatusOK, "")
	check(map[string]string{"email": config.AdminEmail, "purpose": "reset"}, codePath, http.StatusOK, "")
	if mailer.last(t).To != config.AdminEmail {
		t.Fatal("registration policy blocked reset for an existing address")
	}
	unknown := map[string]string{"email": "unknown@elsewhere.example", "purpose": "reset"}
	check(unknown, codePath, http.StatusOK, "")
	check(unknown, codePath, http.StatusTooManyRequests, "cooldown")
	if mailer.last(t).To != config.AdminEmail {
		t.Fatal("reset mail was delivered to an unregistered address")
	}

	retry := map[string]string{"email": "retry@school.example", "purpose": "register"}
	server.auth.mailer = rejectingMailer{}
	check(retry, codePath, http.StatusServiceUnavailable, "mail_unavailable")
	server.auth.mailer = mailer
	check(retry, codePath, http.StatusOK, "")

	policy.RateAttempts, policy.RateWindowMinutes = 2, 1
	if resp := requestJSON(t, server, http.MethodPut, path, policy, cookie); resp.Code != http.StatusOK {
		t.Fatalf("update limits: %d %s", resp.Code, resp.Body.String())
	}
	check(map[string]string{}, "/api/auth/login", http.StatusBadRequest, "credentials_required")
	check(map[string]string{}, "/api/auth/login", http.StatusBadRequest, "credentials_required")
	check(map[string]string{}, "/api/auth/login", http.StatusTooManyRequests, "rate_limited")

	reloaded := newAuthPolicyStore(server.db)
	if err := reloaded.load(t.Context()); err != nil {
		t.Fatal(err)
	}
	saved := reloaded.snapshot()
	if saved.RateAttempts != 2 || saved.EmailMode != "allowlist" || len(saved.EmailDomains) != 1 || saved.EmailDomains[0] != "school.example" {
		t.Fatalf("saved policy: %+v", saved)
	}
}

func TestEmailCodeReservationsAreAtomic(t *testing.T) {
	db, err := openSchemaDatabase(freshDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	codes := NewEmailCodes(db)
	const email = "race@example.com"
	var requests sync.WaitGroup
	outcomes := make(chan error, 4)
	for range cap(outcomes) {
		requests.Go(func() {
			_, _, err := codes.reserve(t.Context(), email, EmailPurposeRegister, true)
			outcomes <- err
		})
	}
	requests.Wait()
	close(outcomes)
	issued := 0
	for err := range outcomes {
		if err == nil {
			issued++
			continue
		}
		apiErr, ok := asAPIError(err)
		if !ok || apiErr.Code != "cooldown" {
			t.Fatalf("reservation: %v", err)
		}
	}
	if issued != 1 {
		t.Fatalf("issued %d codes, want 1", issued)
	}

	const mailbox = "limit@example.com"
	now := time.Now()
	for index := range emailCodeWindowLimit {
		purpose := EmailPurposeRegister
		if index%2 == 1 {
			purpose = EmailPurposeReset
		}
		created := now.Add(-time.Duration(12+2*index) * time.Minute)
		_, err := db.ExecContext(t.Context(), `INSERT INTO email_codes (email, purpose, code_hash, attempts, created_at, expires_at)
			VALUES ($1, $2, '', 0, $3, $4)`, mailbox, purpose, created, created.Add(emailCodeTTL))
		if err != nil {
			t.Fatal(err)
		}
	}
	_, _, err = codes.reserve(t.Context(), mailbox, EmailPurposeReset, false)
	failure, ok := asAPIError(err)
	if !ok || failure.Code != "rate_limited" || failure.RetryAfter <= 0 {
		t.Fatalf("shared hourly mailbox limit: %v", err)
	}
}

type rejectingMailer struct{}

func (rejectingMailer) Send(_ context.Context, _, _, _ string) error {
	return errors.New("SMTP unavailable")
}
