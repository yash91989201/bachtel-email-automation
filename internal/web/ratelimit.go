package web

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// loginLimiter is a small fixed-window limiter for sign-in attempts.
type loginLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]*loginBucket
}

type loginBucket struct {
	start time.Time
	count int
}

func newLoginLimiter(limit int, window time.Duration) *loginLimiter {
	return &loginLimiter{limit: limit, window: window, buckets: make(map[string]*loginBucket)}
}

// allow reports whether another attempt is permitted for key and, when not,
// how long until the window resets. A successful sign-in calls reset.
func (l *loginLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[key]
	if !ok || now.Sub(bucket.start) >= l.window {
		l.buckets[key] = &loginBucket{start: now, count: 1}
		return true, 0
	}
	if bucket.count >= l.limit {
		return false, l.window - now.Sub(bucket.start)
	}
	bucket.count++
	return true, 0
}

// reset forgets the key after a successful sign-in.
func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}

// loginKey extracts the host part of the client address.
// ponytail: RemoteAddr is the only trusted source; proxy headers such as
// X-Forwarded-For are ignored because they are only trustworthy behind a
// reverse proxy configured as trusted in PocketBase.
func loginKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
