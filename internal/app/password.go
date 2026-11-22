package app

import (
	"crypto/rand"
	"encoding/base64"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// bcrypt 只使用前 72 字节，超出部分会被静默截断，因此先按字节数拒绝过长的密码。
const (
	maxPasswordBytes = 72
	passwordCost     = 12
)

// dummyPasswordHash 让"用户不存在"与"密码错误"消耗相近的时间，
// 避免通过响应时间枚举已注册账号。
var dummyPasswordHash = mustDummyHash()

func mustDummyHash() []byte {
	buffer := make([]byte, 32)
	_, _ = rand.Read(buffer)
	hash, err := bcrypt.GenerateFromPassword([]byte(base64.RawURLEncoding.EncodeToString(buffer)), passwordCost)
	if err != nil {
		return []byte("$2a$12$C6UzMDM.H6dfI/f/IKcEeO7ZBpRsdJcJq5o8K0z0R8yWl7sJZ5q1W")
	}
	return hash
}

func hashPassword(password string) (string, error) {
	digest, err := bcrypt.GenerateFromPassword([]byte(password), passwordCost)
	if err != nil {
		return "", err
	}
	return string(digest), nil
}

func verifyPassword(hash, password string) bool {
	if hash == "" {
		// 仍然执行一次比较，保持不存在用户的响应耗时稳定。
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// verifyAgainstDummy 只用于账号不存在时的等时兜底。
func verifyAgainstDummy(password string) {
	_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(password))
}

// passwordRules 返回不满足的规则代码，空切片表示通过。
// 规则：8–72 字节，且至少包含下列四类中的两类。
func passwordRules(password string) []string {
	if password == "" {
		return []string{"required"}
	}
	rules := []string{}
	if len(password) > maxPasswordBytes {
		rules = append(rules, "max_length")
	}
	if utf8.RuneCountInString(password) < 8 {
		rules = append(rules, "min_length")
	}
	var lower, upper, digit, special bool
	for _, char := range password {
		switch {
		case unicode.IsLower(char):
			lower = true
		case unicode.IsUpper(char):
			upper = true
		case unicode.IsDigit(char):
			digit = true
		case unicode.IsPunct(char) || unicode.IsSymbol(char):
			special = true
		}
	}
	categories := 0
	for _, present := range []bool{lower, upper, digit, special} {
		if present {
			categories++
		}
	}
	if categories < 2 {
		rules = append(rules, "composition")
	}
	return rules
}

func passwordRuleMessage(rule string) string {
	switch rule {
	case "required":
		return "请输入密码"
	case "min_length":
		return "密码至少需要 8 个字符"
	case "max_length":
		return "密码不能超过 72 字节"
	case "composition":
		return "密码需至少包含小写字母、大写字母、数字、符号中的两类"
	default:
		return "密码不符合要求"
	}
}
