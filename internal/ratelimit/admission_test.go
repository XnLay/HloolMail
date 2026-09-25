package ratelimit

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPeekDoesNotAllocateOrSpendTokens(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := NewWithOptions(Options{Now: func() time.Time { return now }})
	policy := Policy{Revision: 1, Enabled: true, Rate: 1, Burst: 2}
	if !limiter.Peek("unseen", policy).Allowed || limiter.Len() != 0 {
		t.Fatal("peek must not allocate a bucket")
	}
	limiter.Check("known", policy)
	for i := 0; i < 10; i++ {
		if !limiter.Peek("known", policy).Allowed {
			t.Fatal("peek consumed or reserved the remaining token")
		}
	}
	if !limiter.Check("known", policy).Allowed || limiter.Check("known", policy).Allowed {
		t.Fatal("peek changed the available budget")
	}
	policy.Revision, policy.Rate = 2, 10
	if decision := limiter.Peek("known", policy); decision.Allowed || decision.RetryAfter != 100*time.Millisecond {
		t.Fatalf("peek did not apply the latest policy: %+v", decision)
	}
	if decision := limiter.Check("known", Policy{Revision: 1, Enabled: true, Rate: 1, Burst: 2}); decision.Allowed || decision.RetryAfter != 100*time.Millisecond {
		t.Fatalf("older check downgraded the peek policy: %+v", decision)
	}
	now = now.Add(100 * time.Millisecond)
	if !limiter.Peek("known", policy).Allowed || !limiter.Check("known", policy).Allowed {
		t.Fatal("peek did not preserve a naturally refilled token")
	}
}

func TestPolicyChangesUpdateSafeReclamationTime(t *testing.T) {
	now := time.Unix(1000, 0)
	limiter := NewWithOptions(Options{TTL: time.Second, Now: func() time.Time { return now }})
	policy := Policy{Revision: 1, Enabled: true, Rate: 10, Burst: 2}
	limiter.Check("key", policy)
	policy.Revision, policy.Rate = 2, 0.001
	limiter.Check("key", policy)
	now = now.Add(10 * time.Minute)
	limiter.mu.Lock()
	limiter.evictExpiredBatchLocked(now)
	limiter.mu.Unlock()
	if limiter.Len() != 1 || limiter.Check("key", policy).Allowed {
		t.Fatal("slowing a policy retained an unsafe reclamation deadline")
	}
	policy.Revision, policy.Rate = 3, 10
	limiter.Peek("key", policy)
	now = now.Add(2 * time.Second)
	limiter.mu.Lock()
	limiter.evictExpiredBatchLocked(now)
	limiter.mu.Unlock()
	if limiter.Len() != 0 {
		t.Fatal("a faster policy did not release safely replenished idle capacity")
	}
}

func TestConcurrentCapacityPressurePreservesOutstandingBudget(t *testing.T) {
	limiter := NewWithOptions(Options{MaxEntries: 1, Now: func() time.Time { return time.Unix(1000, 0) }})
	policy := Policy{Revision: 1, Enabled: true, Rate: 0.001, Burst: 1}
	if !limiter.Check("existing", policy).Allowed {
		t.Fatal("initial request rejected")
	}
	var workers sync.WaitGroup
	var allowed atomic.Int64
	for worker := 0; worker < 8; worker++ {
		workers.Go(func() {
			for i := 0; i < 100; i++ {
				if limiter.Check("new:"+strconv.Itoa(worker), policy).Allowed {
					allowed.Add(1)
				}
				if limiter.Check("existing", policy).Allowed {
					allowed.Add(1)
				}
			}
		})
	}
	workers.Wait()
	if allowed.Load() != 0 || limiter.Len() != 1 {
		t.Fatalf("capacity pressure reset debt: allowed=%d entries=%d", allowed.Load(), limiter.Len())
	}
}

func BenchmarkLimiterSaturatedPreflight(b *testing.B) {
	limiter := NewWithOptions(Options{Now: func() time.Time { return time.Unix(1000, 0) }})
	policy := Policy{Revision: 1, Enabled: true, Rate: 0.001, Burst: 1}
	limiter.Check("instance", policy)
	for i := 1; i < DefaultMaxEntries; i++ {
		limiter.Check(strconv.Itoa(i), policy)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		limiter.Peek("instance", policy)
	}
}
