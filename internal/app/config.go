package app

import (
	"bufio"
	"database/sql"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Root          string
	Addr          string
	AdminUser     string
	AdminEmail    string
	AdminPassword string
	DatabaseURL   string
	MeiliURL      string
	MeiliKey      string
	SiteURL       string
	// BasePath 是静态资源、前端路由和邮件链接共用的站点挂载路径。
	BasePath    string
	APIBasePath string
	// BuildEnabled 控制此 Go 服务是否运行后台构建任务。
	BuildEnabled   bool
	SecureCookie   bool
	BuildTimeout   time.Duration
	SessionTTL     time.Duration
	AllowRegister  bool
	TrustedProxies []netip.Addr
	Mail           MailConfig
	// Mailer 允许调用方注入发信实现（测试用）；为空时按 Mail.Mode 构造。
	Mailer Mailer
	DB     *sql.DB
}

// MailConfig 描述验证码邮件的发送方式。log 仅供开发环境查看邮件正文。
type MailConfig struct {
	Mode     string
	Addr     string
	User     string
	Password string
	From     string
	TLS      string
	SiteName string
}

func LoadConfig(root string) (Config, error) {
	if root == "" {
		root = "."
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Config{}, err
	}
	loadDotEnv(absolute)

	addr := os.Getenv("GBWATER_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	user := os.Getenv("GBWATER_ADMIN_USER")
	if user == "" {
		user = "admin"
	}
	adminEmail := os.Getenv("GBWATER_ADMIN_EMAIL")
	if adminEmail == "" {
		adminEmail = "admin@gbwater.local"
	}
	meiliURL := os.Getenv("GBWATER_MEILISEARCH_URL")
	if meiliURL == "" {
		meiliURL = "http://127.0.0.1:7700"
	}
	siteURL := os.Getenv("GBWATER_SITE_URL")
	if siteURL == "" {
		siteURL = "https://gbwater.icu"
	}
	basePath := os.Getenv("GBWATER_BASE_PATH")
	if basePath == "" {
		basePath = "/mypdf/"
	}
	basePath, apiBasePath, err := publicPaths(basePath, os.Getenv("GBWATER_API_BASE_PATH"))
	if err != nil {
		return Config{}, err
	}
	sessionTTL := 30 * 24 * time.Hour
	if value := os.Getenv("GBWATER_SESSION_TTL_HOURS"); value != "" {
		if hours, err := strconv.Atoi(value); err == nil && hours > 0 {
			sessionTTL = time.Duration(hours) * time.Hour
		}
	}
	allowRegister := true
	if value := os.Getenv("GBWATER_ALLOW_REGISTRATION"); value != "" {
		allowRegister = value != "false" && value != "0"
	}
	mailMode := strings.ToLower(strings.TrimSpace(os.Getenv("GBWATER_MAIL_MODE")))
	mailAddr := strings.TrimSpace(os.Getenv("GBWATER_SMTP_ADDR"))
	production := os.Getenv("GBWATER_ENV") == "production"
	if mailMode == "" {
		if mailAddr != "" {
			mailMode = "smtp"
		} else if production {
			mailMode = "off"
		} else {
			mailMode = "log"
		}
	}
	mailTLS := strings.ToLower(strings.TrimSpace(os.Getenv("GBWATER_SMTP_TLS")))
	if mailTLS == "" {
		if strings.HasSuffix(mailAddr, ":465") {
			mailTLS = "tls"
		} else {
			mailTLS = "starttls"
		}
	}
	mailConfig := MailConfig{
		Mode: mailMode, Addr: mailAddr, TLS: mailTLS, SiteName: "碧山秋云",
		User: strings.TrimSpace(os.Getenv("GBWATER_SMTP_USER")), Password: os.Getenv("GBWATER_SMTP_PASSWORD"),
		From: strings.TrimSpace(os.Getenv("GBWATER_SMTP_FROM")),
	}
	if mailConfig.From == "" {
		mailConfig.From = mailConfig.User
	}
	if err := mailConfig.validate(production); err != nil {
		return Config{}, err
	}
	var trustedProxies []netip.Addr
	for _, value := range strings.FieldsFunc(os.Getenv("GBWATER_TRUSTED_PROXIES"), func(r rune) bool { return r == ',' || r == ' ' }) {
		ip, err := netip.ParseAddr(value)
		if err != nil || ip.Zone() != "" {
			return Config{}, fmt.Errorf("GBWATER_TRUSTED_PROXIES 需要填写逗号分隔的 IP 地址")
		}
		trustedProxies = append(trustedProxies, ip.Unmap())
	}
	return Config{
		Root:           absolute,
		Addr:           addr,
		AdminUser:      user,
		AdminEmail:     adminEmail,
		AdminPassword:  os.Getenv("GBWATER_ADMIN_PASSWORD"),
		DatabaseURL:    os.Getenv("GBWATER_DATABASE_URL"),
		MeiliURL:       meiliURL,
		MeiliKey:       os.Getenv("GBWATER_MEILISEARCH_KEY"),
		SiteURL:        strings.TrimRight(siteURL, "/"),
		BasePath:       basePath,
		APIBasePath:    apiBasePath,
		BuildEnabled:   os.Getenv("GBWATER_BUILD_ENABLED") != "false",
		SecureCookie:   production,
		BuildTimeout:   15 * time.Minute,
		SessionTTL:     sessionTTL,
		AllowRegister:  allowRegister,
		TrustedProxies: trustedProxies,
		Mail:           mailConfig,
	}, nil
}

func (c Config) ContentDir() string { return filepath.Join(c.Root, "content") }
func (c Config) DataDir() string    { return filepath.Join(c.Root, "data") }

// SiteLink 返回站点绝对地址，用于邮件等站外链接。
func (c Config) SiteLink() string { return strings.TrimRight(c.SiteURL, "/") + c.BasePath }

var publicPathSegment = regexp.MustCompile(`^[A-Za-z0-9_.~-]+$`)

func publicPaths(site, api string) (string, string, error) {
	normalize := func(value, name string) (string, error) {
		value = strings.Trim(strings.TrimSpace(value), "/")
		if value == "" {
			return "/", nil
		}
		for _, segment := range strings.Split(value, "/") {
			if !publicPathSegment.MatchString(segment) || segment == "." || segment == ".." {
				return "", fmt.Errorf("%s 必须是站内目录路径", name)
			}
		}
		return "/" + value + "/", nil
	}
	site, err := normalize(site, "GBWATER_BASE_PATH")
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(api) == "" {
		api = site + "api/"
	}
	api, err = normalize(api, "GBWATER_API_BASE_PATH")
	if err != nil {
		return "", "", err
	}
	if strings.HasPrefix(site, api) {
		return "", "", fmt.Errorf("API 前缀须与站点页面路径区分")
	}
	return site, api, nil
}

// loadDotEnv 读取仓库根目录的 .env，仅补充进程里尚未设置的变量。
// 它刻意只支持 KEY=VALUE 与 # 注释，避免为此引入第三方依赖。
func loadDotEnv(root string) {
	file, err := os.Open(filepath.Join(root, ".env"))
	if err != nil {
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}
