package auth

import (
	"sync"
	"time"
)

// limiterDecay is how long a key must stay quiet before its backoff resets.
const limiterDecay = time.Hour

// LimiterOptions configures a fixed-window limiter with escalating backoff.
type LimiterOptions struct {
	Limit      int
	Window     time.Duration
	MaxBackoff time.Duration
	Now        func() time.Time
}

// Limiter is an in-memory, per-key rate limiter. It is deliberately simple:
// single-process, no persistence, and it forgets keys over time. Rate limits are
// a speed bump, not an account lock.
type Limiter struct {
	limit      int
	window     time.Duration
	maxBackoff time.Duration
	now        func() time.Time

	mu      sync.Mutex
	entries map[string]*limitEntry
}

type limitEntry struct {
	windowStart  time.Time
	lastSeen     time.Time
	attempts     int
	denials      int
	blockedUntil time.Time
}

// NewLimiter builds a limiter; zero values fall back to sensible defaults.
func NewLimiter(opts LimiterOptions) *Limiter {
	if opts.Limit <= 0 {
		opts.Limit = 60
	}
	if opts.Window <= 0 {
		opts.Window = time.Minute
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 15 * time.Minute
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Limiter{
		limit:      opts.Limit,
		window:     opts.Window,
		maxBackoff: opts.MaxBackoff,
		now:        opts.Now,
		entries:    make(map[string]*limitEntry),
	}
}

// Allow reports whether key may make another attempt. When it may not, the
// second return is how long the caller should wait.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.entries) > 4096 {
		l.sweep(now)
	}

	entry, ok := l.entries[key]
	if !ok {
		entry = &limitEntry{}
		l.entries[key] = entry
	}

	if now.Before(entry.blockedUntil) {
		return false, entry.blockedUntil.Sub(now)
	}

	if entry.windowStart.IsZero() || now.Sub(entry.windowStart) >= l.window {
		// Drop the escalation only after an hour without attempts, so repeated
		// windows keep backing off instead of resetting after one block.
		if !entry.lastSeen.IsZero() && now.Sub(entry.lastSeen) >= limiterDecay {
			entry.denials = 0
		}
		entry.windowStart = now
		entry.attempts = 0
	}
	entry.lastSeen = now

	if entry.attempts >= l.limit {
		entry.denials++
		backoff := l.window
		for i := 1; i < entry.denials && backoff < l.maxBackoff; i++ {
			backoff *= 2
		}
		if backoff > l.maxBackoff {
			backoff = l.maxBackoff
		}
		entry.blockedUntil = now.Add(backoff)
		return false, backoff
	}

	entry.attempts++
	return true, 0
}

// Reset forgets everything about one key, for example after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

func (l *Limiter) sweep(now time.Time) {
	for key, entry := range l.entries {
		if now.Sub(entry.lastSeen) > time.Hour && now.After(entry.blockedUntil) {
			delete(l.entries, key)
		}
	}
}
