package app

import "testing"

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := hashPassword("CorrectHorse1")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "CorrectHorse1" {
		t.Fatal("password must not be stored in plain text")
	}
	if !verifyPassword(hash, "CorrectHorse1") {
		t.Fatal("correct password should verify")
	}
	if verifyPassword(hash, "correcthorse1") {
		t.Fatal("wrong password must not verify")
	}
	if verifyPassword("", "CorrectHorse1") {
		t.Fatal("empty hash must not verify")
	}
}

func TestPasswordRules(t *testing.T) {
	cases := []struct {
		password string
		want     []string
	}{
		{"", []string{"required"}},
		{"short1", []string{"min_length"}},
		{"alllowercase", []string{"composition"}},
		{"LongEnough1", []string{}},
		{"中文密码需要字母8", []string{"composition"}},
		{"CorrectHorse1", []string{}},
		{string(make([]byte, 73)), []string{"max_length", "composition"}},
	}
	for _, testCase := range cases {
		got := passwordRules(testCase.password)
		if len(got) != len(testCase.want) {
			t.Fatalf("%q: got %v want %v", testCase.password, got, testCase.want)
		}
		for index := range got {
			if got[index] != testCase.want[index] {
				t.Fatalf("%q: got %v want %v", testCase.password, got, testCase.want)
			}
		}
	}
}

func TestTemporaryPasswordMeetsPolicy(t *testing.T) {
	for attempt := 0; attempt < 50; attempt++ {
		password, err := temporaryPassword()
		if err != nil {
			t.Fatal(err)
		}
		if rules := passwordRules(password); len(rules) != 0 {
			t.Fatalf("generated password %q failed rules %v", password, rules)
		}
	}
}

func TestValidEmail(t *testing.T) {
	valid := []string{"a@b.com", "first.last+tag@sub.example.org"}
	invalid := []string{"", "no-at-sign", "a@b", "a@b.", "@b.com", "a b@c.com", "a@b.com\nx"}
	for _, value := range valid {
		if !validEmail(value) {
			t.Fatalf("expected valid: %q", value)
		}
	}
	for _, value := range invalid {
		if validEmail(value) {
			t.Fatalf("expected invalid: %q", value)
		}
	}
}

func TestUsernameRules(t *testing.T) {
	cases := []struct {
		username string
		want     string
	}{
		{"", "required"},
		{"a", "min_length"},
		{"张三", ""},
		{"zhang_san-1", ""},
		{"名字 带空格", "characters"},
		{"name/with/slash", "characters"},
		{"很长很长很长很长很长很长很长很长很长很长很长很长很长的名字", "max_length"},
	}
	for _, testCase := range cases {
		rules := usernameRules(testCase.username)
		got := ""
		if len(rules) > 0 {
			got = rules[0]
		}
		if got != testCase.want {
			t.Fatalf("%q: got %q want %q (%v)", testCase.username, got, testCase.want, rules)
		}
	}
}
