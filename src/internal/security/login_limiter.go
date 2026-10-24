package security

import (
	"context"
	"crypto/sha256"
	"net/netip"
	"sync"
	"time"
)

const maxLoginBuckets = 10000

type loginBucketKey struct {
	kind   byte
	digest [32]byte
}

type loginBucket struct {
	started time.Time
	count   int
}

type LoginLimiter struct {
	mu      sync.Mutex
	clock   Clock
	buckets map[loginBucketKey]loginBucket
}

var _ LoginThrottle = (*LoginLimiter)(nil)

func NewLoginLimiter(clock Clock) (*LoginLimiter, error) {
	if missingDependency(clock) {
		return nil, ErrInvalidInput
	}
	return &LoginLimiter{clock: clock, buckets: make(map[loginBucketKey]loginBucket)}, nil
}

func (l *LoginLimiter) CheckAccount(ctx context.Context, username string) error {
	canonical, err := NormalizeUsername(username)
	if err != nil {
		return ErrInvalidInput
	}
	return l.check(ctx, loginBucketKey{kind: 'u', digest: sha256.Sum256([]byte(canonical))}, 5)
}

func (l *LoginLimiter) CheckIP(ctx context.Context, ip string) error {
	address, err := netip.ParseAddr(ip)
	if err != nil || address.Zone() != "" {
		return ErrInvalidInput
	}
	return l.check(ctx, loginBucketKey{kind: 'i', digest: sha256.Sum256([]byte(address.Unmap().String()))}, 20)
}

func (l *LoginLimiter) check(ctx context.Context, key loginBucketKey, limit int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now().UTC()
	bucket, exists := l.buckets[key]
	if exists && !now.Before(bucket.started.Add(time.Minute)) {
		exists = false
	}
	if !exists {
		if len(l.buckets) >= maxLoginBuckets {
			for oldKey, old := range l.buckets {
				if !now.Before(old.started.Add(time.Minute)) {
					delete(l.buckets, oldKey)
				}
			}
			if len(l.buckets) >= maxLoginBuckets {
				return ErrRateLimited
			}
		}
		bucket = loginBucket{started: now}
	}
	if bucket.count >= limit {
		return ErrRateLimited
	}
	bucket.count++
	l.buckets[key] = bucket
	return nil
}
