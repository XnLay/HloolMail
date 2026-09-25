package ratelimit

import (
	"strconv"
	"testing"
	"time"
)

func TestIdleCleanupPreservesLowRateBudget(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := NewWithOptions(Options{Now: func() time.Time { return now }})
	policy := Policy{Revision: 1, Enabled: true, Rate: 0.001, Burst: 100}
	allowed := 0
	for round := 0; round < 4; round++ {
		if round > 0 {
			now = now.Add(15 * time.Minute)
			limiter.mu.Lock()
			limiter.evictExpiredBatchLocked(now)
			limiter.mu.Unlock()
		}
		for i := 0; i < policy.Burst; i++ {
			if limiter.Check("slow", policy).Allowed {
				allowed++
			}
		}
	}
	// 45 分钟只能自然补充 2.7 个令牌；清理不能额外赠送初始突发额度。
	if allowed != 102 {
		t.Fatalf("allowed %d requests over 45 minutes, want 102", allowed)
	}
}

func TestCapacityPreservesUnrefilledBuckets(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := NewWithOptions(Options{MaxEntries: 2, Now: func() time.Time { return now }})
	policy := Policy{Revision: 1, Enabled: true, Rate: 0.001, Burst: 1}
	for _, key := range []string{"first", "second"} {
		if !limiter.Check(key, policy).Allowed {
			t.Fatalf("initial request for %s rejected", key)
		}
	}
	if result := limiter.Check("overflow", policy); result.Allowed || result.RetryAfter <= 0 {
		t.Fatalf("capacity must reject without discarding debt: %+v", result)
	}
	for _, key := range []string{"first", "second"} {
		if limiter.Check(key, policy).Allowed {
			t.Fatalf("capacity pressure reset %s", key)
		}
	}
	now = now.Add(1000 * time.Second)
	if !limiter.Check("overflow", policy).Allowed || limiter.Len() != 2 {
		t.Fatal("naturally refilled capacity was not reusable")
	}
}

func TestCleanupRetainsDebtAndReclaimsRefilledBuckets(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := NewWithOptions(Options{TTL: time.Second, Now: func() time.Time { return now }})
	limiter.Check("slow", Policy{Revision: 1, Enabled: true, Rate: 0.001, Burst: 100})
	limiter.Check("fast", Policy{Revision: 1, Enabled: true, Rate: 10, Burst: 1})
	now = now.Add(2 * time.Second)
	limiter.mu.Lock()
	limiter.evictExpiredBatchLocked(now)
	limiter.mu.Unlock()
	if limiter.Len() != 1 || limiter.entries["slow"] == nil {
		t.Fatal("cleanup must keep debt while reclaiming fully replenished idle buckets")
	}
}

func BenchmarkLimiterNewKeyAtCapacity(b *testing.B) {
	for _, size := range []int{1000, 100000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			limiter := NewWithOptions(Options{MaxEntries: size, Now: func() time.Time { return time.Unix(1000, 0) }})
			policy := Policy{Revision: 1, Enabled: true, Rate: 0.001, Burst: 1}
			for i := 0; i < size; i++ {
				limiter.Check("existing:"+strconv.Itoa(i), policy)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				limiter.Check("new:"+strconv.Itoa(i), policy)
			}
		})
	}
}
