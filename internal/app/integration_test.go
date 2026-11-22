//go:build integration

package app

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

var (
	embeddedPort  uint32
	databaseIndex atomic.Int32
)

type capturedMail struct {
	To      string
	Subject string
	Body    string
}

// capturingMailer 记录验证码邮件，让集成测试能拿到真实验证码而不依赖外部邮箱。
type capturingMailer struct {
	mu       sync.Mutex
	messages []capturedMail
}

func (m *capturingMailer) Send(_ context.Context, to, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, capturedMail{To: to, Subject: subject, Body: body})
	return nil
}

func (m *capturingMailer) last(t *testing.T) capturedMail {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		t.Fatal("no mail was captured")
	}
	return m.messages[len(m.messages)-1]
}

var codePattern = regexp.MustCompile(`\b(\d{6})\b`)

func (m *capturingMailer) lastCode(t *testing.T, to string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	for index := len(m.messages) - 1; index >= 0; index-- {
		if !strings.EqualFold(m.messages[index].To, to) {
			continue
		}
		if match := codePattern.FindStringSubmatch(m.messages[index].Body); match != nil {
			return match[1]
		}
	}
	t.Fatalf("no verification code captured for %s", to)
	return ""
}

func reservePort() (uint32, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return uint32(listener.Addr().(*net.TCPAddr).Port), nil
}

func TestMain(m *testing.M) {
	os.Exit(runIntegrationTests(m))
}

func runIntegrationTests(m *testing.M) int {
	// 同一轮测试共用 PostgreSQL 实例，每轮使用独立的二进制和数据目录。
	root, err := os.MkdirTemp("", "gbwater-integration-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepare PostgreSQL directory: %v\n", err)
		return 1
	}
	defer os.RemoveAll(root)
	port, err := reservePort()
	if err != nil {
		fmt.Fprintf(os.Stderr, "reserve port: %v\n", err)
		return 1
	}
	database := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Username("postgres").Password("postgres").Database("postgres").
		Port(port).BinariesPath(filepath.Join(root, "bin")).RuntimePath(filepath.Join(root, "runtime")).
		StartTimeout(120 * time.Second))
	if err := database.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start embedded postgres: %v\n", err)
		return 1
	}
	defer database.Stop()
	embeddedPort = port
	return m.Run()
}

// freshDatabase 在同一个实例里为每个测试创建独立数据库，互不干扰。
func freshDatabase(t *testing.T) string {
	t.Helper()
	if embeddedPort == 0 {
		t.Fatal("embedded postgres is not running")
	}
	name := fmt.Sprintf("gbwater_%d", databaseIndex.Add(1))
	admin, err := openDatabase(fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", embeddedPort))
	if err != nil {
		t.Fatalf("connect maintenance database: %v", err)
	}
	if _, err := admin.Exec(fmt.Sprintf(`CREATE DATABASE %s`, name)); err != nil {
		admin.Close()
		t.Fatalf("create database %s: %v", name, err)
	}
	admin.Close()
	t.Cleanup(func() {
		admin, err := openDatabase(fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", embeddedPort))
		if err != nil {
			return
		}
		defer admin.Close()
		_, _ = admin.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, name))
	})
	return fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/%s?sslmode=disable", embeddedPort, name)
}

func openDatabase(url string) (*sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func integrationConfig(t *testing.T, databaseURL string) Config {
	t.Helper()
	return Config{
		Root:          filepath.Join("..", ".."),
		DatabaseURL:   databaseURL,
		SiteURL:       "https://gbwater.test",
		BasePath:      "/mypdf/",
		SessionTTL:    time.Hour,
		BuildTimeout:  time.Minute,
		AllowRegister: true,
	}
}
