package app

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"log"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Mailer 抽象发信，测试可注入实现；生产环境见 smtpMailer。
type Mailer interface {
	Send(ctx context.Context, to, subject, body string) error
}

type logMailer struct{}

func (logMailer) Send(_ context.Context, to, subject, body string) error {
	log.Printf("[mail] to=%s subject=%q\n%s", to, subject, body)
	return nil
}

func (c MailConfig) validate(production bool) error {
	switch c.Mode {
	case "log":
		if production {
			return fmt.Errorf("生产环境请使用 SMTP 或设置 GBWATER_MAIL_MODE=off")
		}
		return nil
	case "off":
		return nil
	case "smtp":
	default:
		return fmt.Errorf("GBWATER_MAIL_MODE 只支持 smtp、log 或 off")
	}
	host, port, err := net.SplitHostPort(c.Addr)
	number, portErr := strconv.Atoi(port)
	if err != nil || strings.TrimSpace(host) == "" || portErr != nil || number < 1 || number > 65535 {
		return fmt.Errorf("GBWATER_SMTP_ADDR 必须包含主机名和有效端口")
	}
	if (c.User == "") != (c.Password == "") {
		return fmt.Errorf("GBWATER_SMTP_USER 和 GBWATER_SMTP_PASSWORD 需要配对设置")
	}
	sender, err := mail.ParseAddress(c.From)
	if err != nil || sender.Address == "" || strings.ContainsAny(c.From, "\r\n") {
		return fmt.Errorf("请在 GBWATER_SMTP_FROM 中填写发件人地址，或使用邮箱格式的 SMTP 用户名")
	}
	switch c.TLS {
	case "starttls", "tls":
	case "none":
		ip := net.ParseIP(host)
		if c.User != "" || (!strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback())) {
			return fmt.Errorf("GBWATER_SMTP_TLS=none 只适用于不需要凭据的本机中继")
		}
	default:
		return fmt.Errorf("GBWATER_SMTP_TLS 只支持 starttls、tls 或 none")
	}
	return nil
}

type smtpMailer struct{ config MailConfig }

func newMailer(config MailConfig) Mailer {
	switch config.Mode {
	case "smtp":
		return smtpMailer{config: config}
	case "log":
		return logMailer{}
	default:
		return nil
	}
}

func (m smtpMailer) Send(ctx context.Context, to, subject, body string) error {
	host, _, err := net.SplitHostPort(m.config.Addr)
	if err != nil {
		return err
	}
	sender, err := mail.ParseAddress(m.config.From)
	if err != nil {
		return err
	}
	if sender.Name == "" {
		sender.Name = m.config.SiteName
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	var connection net.Conn
	if m.config.TLS == "tls" {
		connection, err = (&tls.Dialer{Config: tlsConfig}).DialContext(ctx, "tcp", m.config.Addr)
	} else {
		connection, err = (&net.Dialer{}).DialContext(ctx, "tcp", m.config.Addr)
	}
	if err != nil {
		return fmt.Errorf("连接 SMTP: %w", err)
	}
	defer connection.Close()
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return err
	}
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		return fmt.Errorf("SMTP 握手: %w", err)
	}
	if m.config.TLS == "starttls" {
		if err := client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}

	if m.config.User != "" {
		if err := client.Auth(smtp.PlainAuth("", m.config.User, m.config.Password, host)); err != nil {
			return err
		}
	}
	if err := client.Mail(sender.Address); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write([]byte(buildMailMessage(sender.String(), to, subject, body))); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	_ = client.Quit()
	return nil
}

func buildMailMessage(from, to, subject, body string) string {
	headers := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"Date: " + time.Now().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: base64",
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	lines := make([]string, 0, len(encoded)/76+1)
	for len(encoded) > 76 {
		lines = append(lines, encoded[:76])
		encoded = encoded[76:]
	}
	lines = append(lines, encoded)
	return strings.Join(headers, "\r\n") + "\r\n\r\n" + strings.Join(lines, "\r\n") + "\r\n"
}

func verificationMail(siteName, siteLink, username, code, purpose string) (subject, body string) {
	action := "注册账号"
	if purpose == EmailPurposeReset {
		action = "重置密码"
	}
	subject = fmt.Sprintf("%s %s验证码", siteName, action)
	body = fmt.Sprintf(`%s，你好：

你正在%s%s，验证码为：

    %s

验证码 %d 分钟内有效，请勿转发给他人。如果这不是你的操作，请忽略本邮件。

站点：%s
`, username, siteName, action, code, int(emailCodeTTL/time.Minute), siteLink)
	return subject, body
}
