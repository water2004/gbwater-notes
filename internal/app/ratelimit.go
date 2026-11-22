package app

import (
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type limitBucket struct {
	count int
	reset time.Time
}

// Limiter 是单进程内的固定窗口计数器。gbwater 以单实例部署，
// 因此不引入 Redis；多实例部署时需要换成共享存储。
type Limiter struct {
	mu        sync.Mutex
	buckets   map[string]*limitBucket
	nextSweep time.Time
}

func NewLimiter() *Limiter {
	return &Limiter{buckets: map[string]*limitBucket{}}
}

// Allow 记录一次尝试，触发限制时返回剩余等待时间。
func (l *Limiter) Allow(key string, limit int, window time.Duration) (bool, time.Duration) {
	if limit <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if !now.Before(l.nextSweep) {
		for bucketKey, bucket := range l.buckets {
			if !bucket.reset.After(now) {
				delete(l.buckets, bucketKey)
			}
		}
		l.nextSweep = now.Add(time.Minute)
	}
	bucket, ok := l.buckets[key]
	if !ok && len(l.buckets) >= 65536 {
		return false, l.nextSweep.Sub(now)
	}
	if !ok || !bucket.reset.After(now) {
		l.buckets[key] = &limitBucket{count: 1, reset: now.Add(window)}
		return true, 0
	}
	if bucket.count >= limit {
		return false, bucket.reset.Sub(now)
	}
	bucket.count++
	return true, 0
}

// clientIP 在直连时取 RemoteAddr；只有来自本机反代（nginx）时才信任
// X-Forwarded-For，避免外部请求伪造 IP 绕过限流。
func clientIP(r *http.Request, trusted []netip.Addr) string {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	ip, err := netip.ParseAddr(remote)
	if err != nil {
		return remote
	}
	ip = ip.Unmap()
	if ip.IsLoopback() || slices.Contains(trusted, ip) {
		forwarded := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
		last := forwarded[strings.LastIndexByte(forwarded, ',')+1:]
		if candidate, err := netip.ParseAddr(strings.TrimSpace(last)); err == nil && candidate.Zone() == "" {
			return candidate.Unmap().String()
		}
	}
	return ip.String()
}

func retryAfterSeconds(retry time.Duration) string {
	return strconv.FormatInt(max(1, int64((retry+time.Second-1)/time.Second)), 10)
}

func rateLimited(code, message string, retry time.Duration) apiError {
	return apiError{Status: http.StatusTooManyRequests, Code: code, Message: message, RetryAfter: retry}
}
