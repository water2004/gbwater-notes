package app

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestLimiterBlocksAfterLimit(t *testing.T) {
	limiter := NewLimiter()
	for attempt := 0; attempt < 3; attempt++ {
		allowed, _ := limiter.Allow("login:ip:1.2.3.4", 3, time.Minute)
		if !allowed {
			t.Fatalf("attempt %d should be allowed", attempt+1)
		}
	}
	allowed, retry := limiter.Allow("login:ip:1.2.3.4", 3, time.Minute)
	if allowed {
		t.Fatal("fourth attempt should be blocked")
	}
	if retry <= 0 || retry > time.Minute {
		t.Fatalf("retry window: %v", retry)
	}
}

func TestLimiterWindowExpires(t *testing.T) {
	limiter := NewLimiter()
	if allowed, _ := limiter.Allow("code:email", 1, 10*time.Millisecond); !allowed {
		t.Fatal("first attempt should pass")
	}
	if allowed, _ := limiter.Allow("code:email", 1, 10*time.Millisecond); allowed {
		t.Fatal("second attempt inside the window should fail")
	}
	time.Sleep(15 * time.Millisecond)
	if allowed, _ := limiter.Allow("code:email", 1, 10*time.Millisecond); !allowed {
		t.Fatal("attempt after the window should pass")
	}
}

func TestClientIPOnlyTrustsForwardedHeaderFromLoopback(t *testing.T) {
	loopback := httptest.NewRequest(http.MethodGet, "/", nil)
	loopback.RemoteAddr = "127.0.0.1:5555"
	loopback.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := clientIP(loopback, nil); got != "10.0.0.1" {
		t.Fatalf("loopback forwarded ip: %q", got)
	}

	remote := httptest.NewRequest(http.MethodGet, "/", nil)
	remote.RemoteAddr = "198.51.100.7:5555"
	remote.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := clientIP(remote, nil); got != "198.51.100.7" {
		t.Fatalf("remote ip must not trust header: %q", got)
	}
	if got := clientIP(remote, []netip.Addr{netip.MustParseAddr("198.51.100.7")}); got != "203.0.113.9" {
		t.Fatalf("configured proxy: %q", got)
	}
}
