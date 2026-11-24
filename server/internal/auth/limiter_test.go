package auth

import (
	"testing"
	"time"
)

type limiterClock struct{ now time.Time }

func (c *limiterClock) Now() time.Time { return c.now }

func TestLimiterAllowsThenBlocks(t *testing.T) {
	clock := &limiterClock{now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	limiter := NewLimiter(LimiterOptions{Limit: 5, Window: time.Minute, Now: clock.Now})

	for i := 0; i < 5; i++ {
		if ok, _ := limiter.Allow("ip:1.2.3.4"); !ok {
			t.Fatalf("attempt %d denied, want allowed", i+1)
		}
	}

	ok, retry := limiter.Allow("ip:1.2.3.4")
	if ok {
		t.Fatal("6th attempt allowed, want denied")
	}
	if retry != time.Minute {
		t.Errorf("retry = %v, want 1m", retry)
	}

	if ok, _ := limiter.Allow("ip:5.6.7.8"); !ok {
		t.Error("different key denied, want isolated")
	}
}

func TestLimiterBacksOffExponentially(t *testing.T) {
	clock := &limiterClock{now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	limiter := NewLimiter(LimiterOptions{Limit: 2, Window: time.Minute, MaxBackoff: 10 * time.Minute, Now: clock.Now})

	limiter.Allow("k")
	limiter.Allow("k")
	if ok, retry := limiter.Allow("k"); ok || retry != time.Minute {
		t.Fatalf("first denial = %v, %v; want false, 1m", ok, retry)
	}

	clock.now = clock.now.Add(time.Minute + time.Second)
	limiter.Allow("k")
	limiter.Allow("k")
	if ok, retry := limiter.Allow("k"); ok || retry != 2*time.Minute {
		t.Fatalf("second denial = %v, %v; want false, 2m", ok, retry)
	}

	clock.now = clock.now.Add(2*time.Minute + time.Second)
	limiter.Allow("k")
	limiter.Allow("k")
	if ok, retry := limiter.Allow("k"); ok || retry != 4*time.Minute {
		t.Fatalf("third denial = %v, %v; want false, 4m", ok, retry)
	}
}

func TestLimiterResetAndQuietPeriod(t *testing.T) {
	clock := &limiterClock{now: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)}
	limiter := NewLimiter(LimiterOptions{Limit: 1, Window: time.Minute, Now: clock.Now})

	limiter.Allow("k")
	if ok, _ := limiter.Allow("k"); ok {
		t.Fatal("second attempt allowed, want denied")
	}

	limiter.Reset("k")
	if ok, _ := limiter.Allow("k"); !ok {
		t.Error("attempt after Reset denied")
	}

	// A long quiet period drops escalation but not the current window.
	clock.now = clock.now.Add(2 * time.Hour)
	limiter.Allow("k")
	if ok, retry := limiter.Allow("k"); ok || retry != time.Minute {
		t.Errorf("denial after quiet period = %v, %v; want false, 1m", ok, retry)
	}
}
