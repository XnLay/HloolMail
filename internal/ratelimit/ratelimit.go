package ratelimit

import (
	"container/heap"
	"math"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	DefaultMaxEntries      = 100_000
	DefaultTTL             = 10 * time.Minute
	DefaultCleanupInterval = 5 * time.Minute
	DefaultCleanupBatch    = 1_000
)

type Options struct {
	MaxEntries      int
	TTL             time.Duration
	CleanupInterval time.Duration
	CleanupBatch    int
	Now             func() time.Time
}

type Limiter struct {
	mu           sync.Mutex
	entries      map[string]*clientLimiter
	expirations  expiryQueue
	maxEntries   int
	cleanupBatch int
	ttl          time.Duration
	now          func() time.Time
}

type clientLimiter struct {
	key       string
	limiter   *rate.Limiter
	policy    Policy
	lastSeen  time.Time
	expiresAt time.Time
	index     int
}

type Policy struct {
	Revision int64
	Enabled  bool
	Rate     rate.Limit
	Burst    int
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

func New() *Limiter {
	return NewWithOptions(Options{CleanupInterval: DefaultCleanupInterval})
}

func NewWithOptions(opts Options) *Limiter {
	if opts.MaxEntries <= 0 {
		opts.MaxEntries = DefaultMaxEntries
	}
	if opts.TTL <= 0 {
		opts.TTL = DefaultTTL
	}
	if opts.CleanupBatch <= 0 {
		opts.CleanupBatch = DefaultCleanupBatch
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	limiter := &Limiter{
		entries:      make(map[string]*clientLimiter),
		maxEntries:   opts.MaxEntries,
		cleanupBatch: opts.CleanupBatch,
		ttl:          opts.TTL,
		now:          opts.Now,
	}
	if opts.CleanupInterval > 0 {
		go limiter.cleanup(opts.CleanupInterval)
	}
	return limiter
}

func (l *Limiter) Allow(key string, r rate.Limit, burst int) bool {
	return l.Check(key, Policy{Enabled: true, Rate: r, Burst: burst}).Allowed
}

// Check 将参数、额度和回收索引一起更新，避免仍在使用的桶被回收并重建。
func (l *Limiter) Check(key string, policy Policy) Decision {
	return l.check(key, policy, true)
}

// Peek 更新已有桶的规则并检查额度，不扣令牌、不预留额度，也不创建新桶。
func (l *Limiter) Peek(key string, policy Policy) Decision {
	return l.check(key, policy, false)
}

func (l *Limiter) check(key string, policy Policy, consume bool) Decision {
	if l == nil {
		return Decision{Allowed: true}
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	entry, exists := l.entries[key]
	if !exists {
		if !policy.Enabled || !consume {
			return Decision{Allowed: true}
		}
		if decision := l.ensureCapacityLocked(now); !decision.Allowed {
			return decision
		}
		entry = &clientLimiter{
			key: key, limiter: rate.NewLimiter(policy.Rate, policy.Burst), policy: policy, index: -1,
		}
		l.entries[key] = entry
	}
	if now.Before(entry.lastSeen) {
		now = entry.lastSeen
	}
	entry.lastSeen = now
	if policy.Revision > entry.policy.Revision {
		if policy.Rate != entry.policy.Rate {
			entry.limiter.SetLimitAt(now, policy.Rate)
		}
		if policy.Burst != entry.policy.Burst {
			entry.limiter.SetBurstAt(now, policy.Burst)
		}
		entry.policy = policy
	}

	decision := Decision{Allowed: true}
	if entry.policy.Enabled {
		if consume {
			decision.Allowed = entry.limiter.AllowN(now, 1)
		} else {
			decision.Allowed = entry.policy.Rate == rate.Inf || entry.limiter.TokensAt(now) >= 1
		}
		if !decision.Allowed {
			decision.RetryAfter = refillDelay(1-entry.limiter.TokensAt(now), entry.policy.Rate)
		}
	}
	l.scheduleExpiryLocked(entry, now)
	return decision
}

func (l *Limiter) Len() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

func (l *Limiter) ensureCapacityLocked(now time.Time) Decision {
	if len(l.entries) < l.maxEntries {
		return Decision{Allowed: true}
	}
	// 满容量只检查队首候选，禁止扫描全表或丢弃欠额桶来腾出空间。
	entry := l.expirations[0]
	missing := float64(entry.policy.Burst) - entry.limiter.TokensAt(now)
	if missing > 0 {
		return Decision{RetryAfter: refillDelay(missing, entry.policy.Rate)}
	}
	l.removeFirstLocked()
	return Decision{Allowed: true}
}

func (l *Limiter) scheduleExpiryLocked(entry *clientLimiter, now time.Time) {
	fullAt := now.Add(refillDelay(float64(entry.policy.Burst)-entry.limiter.TokensAt(now), entry.policy.Rate))
	entry.expiresAt = entry.lastSeen.Add(l.ttl)
	if fullAt.After(entry.expiresAt) {
		entry.expiresAt = fullAt
	}
	if entry.index < 0 {
		heap.Push(&l.expirations, entry)
	} else {
		heap.Fix(&l.expirations, entry.index)
	}
}

func (l *Limiter) removeFirstLocked() {
	entry := heap.Pop(&l.expirations).(*clientLimiter)
	delete(l.entries, entry.key)
}

func (l *Limiter) cleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		l.evictExpiredBatchLocked(l.now())
		l.mu.Unlock()
	}
}

func (l *Limiter) evictExpiredBatchLocked(now time.Time) {
	for i := 0; i < l.cleanupBatch && len(l.expirations) > 0; i++ {
		entry := l.expirations[0]
		if now.Before(entry.expiresAt) {
			return
		}
		// TTL 和自然补满必须同时满足；再次检查额度防止浮点取整提前回收。
		if entry.limiter.TokensAt(now) < float64(entry.policy.Burst) {
			l.scheduleExpiryLocked(entry, now)
			continue
		}
		l.removeFirstLocked()
	}
}

func refillDelay(tokens float64, limit rate.Limit) time.Duration {
	if tokens <= 0 {
		return 0
	}
	if limit <= 0 {
		return time.Duration(math.MaxInt64)
	}
	delay := math.Ceil(tokens / float64(limit) * float64(time.Second))
	if delay >= float64(math.MaxInt64) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(delay)
}
