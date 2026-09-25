package ratelimit

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestPolicyUpdatesExistingBucketWithoutRefilling(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	limiter := NewWithOptions(Options{Now: func() time.Time { return now }})
	policy := Policy{Revision: 1, Enabled: true, Rate: 2, Burst: 2}
	for i := 0; i < 2; i++ {
		if !limiter.Check("key", policy).Allowed {
			t.Fatal("initial burst rejected")
		}
	}
	if result := limiter.Check("key", policy); result.Allowed || result.RetryAfter != 500*time.Millisecond {
		t.Fatalf("exhausted bucket: %+v", result)
	}
	policy.Revision, policy.Rate, policy.Burst = 2, 10, 4
	if result := limiter.Check("key", policy); result.Allowed || result.RetryAfter != 100*time.Millisecond {
		t.Fatalf("update refilled the bucket or kept the old rate: %+v", result)
	}
	now = now.Add(100 * time.Millisecond)
	if !limiter.Check("key", policy).Allowed {
		t.Fatal("new rate did not refill the existing bucket")
	}
	now = now.Add(time.Second)
	policy.Revision, policy.Burst = 3, 1
	if !limiter.Check("key", policy).Allowed {
		t.Fatal("expected one token after shrinking burst")
	}
	if limiter.Check("key", policy).Allowed {
		t.Fatal("old burst capacity survived reduction")
	}
	if result := limiter.Check("key", Policy{Revision: 1, Enabled: true, Rate: 2, Burst: 2}); result.Allowed || result.RetryAfter != 100*time.Millisecond {
		t.Fatalf("old request reverted bucket parameters: %+v", result)
	}
	policy.Revision, policy.Enabled = 4, false
	for i := 0; i < 20; i++ {
		if !limiter.Check("key", policy).Allowed {
			t.Fatal("disabled policy rejected a request")
		}
	}
	policy.Revision, policy.Enabled = 5, true
	if limiter.Check("key", policy).Allowed {
		t.Fatal("re-enabling granted a fresh burst")
	}
	now = now.Add(100 * time.Millisecond)
	if !limiter.Check("key", policy).Allowed {
		t.Fatal("re-enabled bucket stopped refilling")
	}
}

func TestUnrelatedRevisionDoesNotResetQuota(t *testing.T) {
	limiter := NewWithOptions(Options{Now: func() time.Time { return time.Unix(100, 0) }})
	policy := Policy{Revision: 1, Enabled: true, Rate: 1, Burst: 1}
	if !limiter.Check("key", policy).Allowed {
		t.Fatal("initial token rejected")
	}
	policy.Revision++
	if limiter.Check("key", policy).Allowed {
		t.Fatal("unchanged policy received new tokens")
	}
	if !limiter.Check("disabled", Policy{Revision: 2}).Allowed {
		t.Fatal("disabled policy rejected")
	}
	if limiter.Len() != 1 {
		t.Fatal("disabled policy allocated a new bucket")
	}
}

func TestConcurrentPolicyChangesCannotDowngradeBucket(t *testing.T) {
	limiter := NewWithOptions(Options{})
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Go(func() {
			for i := 1; i <= 100; i++ {
				limiter.Check("shared", Policy{Revision: int64(worker*100 + i), Enabled: true, Rate: rate.Limit(worker + 1), Burst: i%5 + 1})
			}
		})
	}
	workers.Wait()
	entry := limiter.entries["shared"]
	if entry.policy.Revision != 800 || entry.policy.Rate != 8 || entry.policy.Burst != 1 {
		t.Fatalf("stale policy won concurrent update: %+v", entry.policy)
	}
}

func BenchmarkLimiterHotKey(b *testing.B) {
	limiter := NewWithOptions(Options{})
	policy := Policy{Revision: 1, Enabled: true, Rate: rate.Inf, Burst: 1}
	limiter.Check("key", policy)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			limiter.Check("key", policy)
		}
	})
}

func BenchmarkLimiterManyKeys(b *testing.B) {
	limiter := NewWithOptions(Options{})
	policy := Policy{Revision: 1, Enabled: true, Rate: rate.Inf, Burst: 1}
	keys := make([]string, 1024)
	for i := range keys {
		keys[i] = strconv.Itoa(i)
		limiter.Check(keys[i], policy)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			limiter.Check(keys[i%len(keys)], policy)
			i++
		}
	})
}
