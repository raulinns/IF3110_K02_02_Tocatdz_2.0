package security

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginLimiterThresholdsAndExpiry(t *testing.T) {
	clock := &authTestClock{now: time.Now().UTC()}
	limiter, _ := NewLoginLimiter(clock)
	for range 5 {
		if err := limiter.CheckAccount(context.Background(), "ALICE"); err != nil {
			t.Fatal(err)
		}
	}
	assertAuthError(t, limiter.CheckAccount(context.Background(), " alice "), ErrRateLimited)
	for range 20 {
		if err := limiter.CheckIP(context.Background(), "192.0.2.1"); err != nil {
			t.Fatal(err)
		}
	}
	assertAuthError(t, limiter.CheckIP(context.Background(), "::ffff:192.0.2.1"), ErrRateLimited)
	clock.add(time.Minute)
	if err := limiter.CheckAccount(context.Background(), "alice"); err != nil {
		t.Fatal("account window did not expire")
	}
	if err := limiter.CheckIP(context.Background(), "192.0.2.1"); err != nil {
		t.Fatal("IP window did not expire")
	}
}

func TestLoginLimiterConcurrentRequestsCannotExceedLimit(t *testing.T) {
	limiter, _ := NewLoginLimiter(SystemClock{})
	var wg sync.WaitGroup
	var allowed atomic.Int32
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if limiter.CheckAccount(context.Background(), "alice") == nil {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Fatalf("allowed %d requests, want 5", allowed.Load())
	}
}

func TestLoginLimiterBoundsStorageAndRejectsInvalidInput(t *testing.T) {
	clock := &authTestClock{now: time.Now().UTC()}
	limiter, _ := NewLoginLimiter(clock)
	for i := range maxLoginBuckets {
		key := loginBucketKey{kind: 'u', digest: sha256.Sum256([]byte(fmt.Sprint(i)))}
		limiter.buckets[key] = loginBucket{started: clock.Now(), count: 1}
	}
	assertAuthError(t, limiter.CheckAccount(context.Background(), "alice"), ErrRateLimited)
	if len(limiter.buckets) > maxLoginBuckets {
		t.Fatal("limiter storage exceeded bound")
	}
	clock.add(time.Minute)
	if err := limiter.CheckAccount(context.Background(), "alice"); err != nil || len(limiter.buckets) != 1 {
		t.Fatal("expired buckets not reclaimed")
	}
	assertAuthError(t, limiter.CheckIP(context.Background(), "spoofed-header"), ErrInvalidInput)
	assertAuthError(t, limiter.CheckAccount(context.Background(), "bad!"), ErrInvalidInput)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assertAuthError(t, limiter.CheckAccount(ctx, "alice"), context.Canceled)
	if _, err := NewLoginLimiter(nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("missing clock accepted")
	}
}

func TestLoginServiceThrottlesKnownAndUnknownAccounts(t *testing.T) {
	for _, username := range []string{"alice", "absent"} {
		f := newAuthFixture(t)
		for range 5 {
			_, err := f.service.Login(context.Background(), Credentials{Username: username, Password: NewSecret("wrong")}, Secret{})
			assertAuthError(t, err, ErrInvalidCredentials)
		}
		_, err := f.service.Login(context.Background(), Credentials{Username: username, Password: NewSecret("wrong")}, Secret{})
		assertAuthError(t, err, ErrRateLimited)
	}
}
