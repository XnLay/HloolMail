package webhook

import (
	"context"
	"net/http"
	"testing"
	"time"

	"gptmail/internal/models"
)

func TestWorkerRejectsReclaimedDelivery(t *testing.T) {
	first, second, oldClaim, currentClaim := reclaimedDelivery(t)
	owned, err := first.deliveryStillClaimed(context.Background(), &oldClaim)
	if err != nil {
		t.Fatal(err)
	}
	if owned {
		t.Error("old worker still reports ownership after reclaim")
	}
	oldPosts, currentPosts := 0, 0
	first.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		oldPosts++
		return webhookHTTPResponse(http.StatusAccepted, "accepted"), nil
	})}
	second.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		currentPosts++
		// 当前请求进行中恢复旧 worker，确定性重现接管后的交错执行。
		if err := first.deliver(context.Background(), &oldClaim); err != nil {
			return nil, err
		}
		return webhookHTTPResponse(http.StatusAccepted, "accepted"), nil
	})}
	if err := second.deliver(context.Background(), &currentClaim); err != nil {
		t.Fatal(err)
	}
	if oldPosts != 0 || currentPosts != 1 {
		t.Errorf("posts: old=%d, current=%d; want 0 and 1", oldPosts, currentPosts)
	}
}

func TestOldWorkerCannotFinishReclaimedDelivery(t *testing.T) {
	for _, outcome := range []string{"success", "failure"} {
		t.Run(outcome, func(t *testing.T) {
			first, second, oldClaim, _ := reclaimedDelivery(t)
			var err error
			if outcome == "success" {
				err = first.finishSuccess(context.Background(), &oldClaim, first.now(), http.StatusOK, "")
			} else {
				err = first.finishFailure(context.Background(), &oldClaim, first.now(), "late failure", nil, "", true)
			}
			if err != nil {
				t.Fatal(err)
			}
			var current models.WebhookDelivery
			if err := first.DB.First(&current, "id = ?", oldClaim.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.Status != models.WebhookDeliveryStatusDelivering || current.LockedBy != second.LockedBy {
				t.Errorf("old result changed the new claim: status=%s locked_by=%s", current.Status, current.LockedBy)
			}
		})
	}
}

func TestWorkerRejectsOldClaimEvenWithSameWorkerID(t *testing.T) {
	first, second, oldClaim, _ := reclaimedDelivery(t)
	if err := first.DB.Model(&models.WebhookDelivery{}).Where("id = ?", oldClaim.ID).
		Update("locked_by", first.LockedBy).Error; err != nil {
		t.Fatal(err)
	}
	second.LockedBy = first.LockedBy
	owned, err := first.deliveryStillClaimed(context.Background(), &oldClaim)
	if err != nil {
		t.Fatal(err)
	}
	if owned {
		t.Fatal("old claim matched a newer claim with the same worker ID")
	}
	if err := first.finishSuccess(context.Background(), &oldClaim, first.now(), http.StatusOK, ""); err != nil {
		t.Fatal(err)
	}
	var current models.WebhookDelivery
	if err := first.DB.First(&current, "id = ?", oldClaim.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != models.WebhookDeliveryStatusDelivering || current.LockedBy != second.LockedBy {
		t.Fatal("stale completion overwrote the renewed claim")
	}
}

func reclaimedDelivery(t *testing.T) (*Worker, *Worker, models.WebhookDelivery, models.WebhookDelivery) {
	t.Helper()
	database := webhookTestDB(t)
	endpoint := createWebhookTestEndpoint(t, database, 1, "https://example.com/hook", "test-secret")
	createWebhookTestDelivery(t, database, endpoint, "{}")
	now := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	first, second := NewWorker(database), NewWorker(database)
	first.LockedBy, second.LockedBy = "worker-a", "worker-b"
	first.Now, second.Now = func() time.Time { return now }, func() time.Time { return now }
	first.Resolve, second.Resolve = publicExampleResolver, publicExampleResolver
	oldClaims, err := first.claimDueDeliveries(context.Background())
	if err != nil || len(oldClaims) != 1 {
		t.Fatalf("first claim: count=%d err=%v", len(oldClaims), err)
	}
	now = now.Add(11 * time.Minute)
	currentClaims, err := second.claimDueDeliveries(context.Background())
	if err != nil || len(currentClaims) != 1 {
		t.Fatalf("reclaim: count=%d err=%v", len(currentClaims), err)
	}
	return first, second, oldClaims[0], currentClaims[0]
}
