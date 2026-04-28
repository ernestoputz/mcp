package oauth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// rateLimiter is a per-key token bucket with a separate failure counter
// that temporarily blocks misbehaving clients. Stdlib only, no dependencies.
//
// Two flows feed it:
//   - Allow(key) on every incoming request → enforces request/sec quota.
//   - Fail(key) after authentication errors → after N consecutive failures
//     the key is hard-blocked for a fixed window, regardless of token
//     bucket state. Success(key) resets the failure counter.
type rateLimiter struct {
	rate      float64       // tokens added per second
	burst     float64       // max tokens in the bucket
	failLimit int           // consecutive failures before a hard block
	blockFor  time.Duration // duration of the hard block

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens       float64
	last         time.Time
	failures     int
	blockedUntil time.Time
}

// newRateLimiter returns a limiter that allows `perMinute` requests per minute
// per key, hard-blocks the key for `blockFor` after `failLimit` consecutive
// failures, and gc's idle entries every minute.
func newRateLimiter(perMinute, failLimit int, blockFor time.Duration) *rateLimiter {
	rl := &rateLimiter{
		rate:      float64(perMinute) / 60.0,
		burst:     float64(perMinute),
		failLimit: failLimit,
		blockFor:  blockFor,
		buckets:   make(map[string]*bucket),
	}
	go rl.gcLoop()
	return rl
}

func (r *rateLimiter) gcLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		r.mu.Lock()
		now := time.Now()
		for k, b := range r.buckets {
			if now.Sub(b.last) > 10*time.Minute && now.After(b.blockedUntil) {
				delete(r.buckets, k)
			}
		}
		r.mu.Unlock()
	}
}

// Allow reports whether a request should be served. retryAfter is the
// number of seconds the caller should wait before retrying (0 if allowed).
func (r *rateLimiter) Allow(key string) (ok bool, retryAfter int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	b, found := r.buckets[key]
	if !found {
		b = &bucket{tokens: r.burst, last: now}
		r.buckets[key] = b
	}
	if now.Before(b.blockedUntil) {
		return false, int(time.Until(b.blockedUntil).Seconds()) + 1
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * r.rate
	if b.tokens > r.burst {
		b.tokens = r.burst
	}
	b.last = now
	if b.tokens < 1 {
		// Time until at least 1 token is available again.
		need := 1 - b.tokens
		retryAfter = int(need/r.rate) + 1
		return false, retryAfter
	}
	b.tokens--
	return true, 0
}

// Fail records an authentication failure for the key. After failLimit
// consecutive failures the key is hard-blocked for blockFor.
func (r *rateLimiter) Fail(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, found := r.buckets[key]
	if !found {
		b = &bucket{tokens: r.burst, last: time.Now()}
		r.buckets[key] = b
	}
	b.failures++
	if b.failures >= r.failLimit {
		b.blockedUntil = time.Now().Add(r.blockFor)
		b.failures = 0
	}
}

// Success resets the failure counter for the key.
func (r *rateLimiter) Success(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b, found := r.buckets[key]; found {
		b.failures = 0
	}
}

// clientIP returns the request's source address. We deliberately use
// RemoteAddr only (and not X-Forwarded-For) so an attacker cannot bypass the
// limiter by spoofing the header. In Caddy mode all external traffic shares
// Caddy's container IP — that's a coarser bucket but still kills brute-force
// attempts at the cost of mixing legitimate concurrent users together.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
