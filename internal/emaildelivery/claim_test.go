package emaildelivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"gptmail/internal/mailer"
	"gptmail/internal/models"
)

func TestReclaimedDeliveryRejectsOldStageAndResult(t *testing.T) {
	for _, sameWorker := range []bool{false, true} {
		name := "different worker"
		if sameWorker {
			name = "same worker new claim"
		}
		t.Run(name, func(t *testing.T) {
			database := testDB(t)
			sender := &recordingSender{}
			delivery, err := Enqueue(database, EnqueueInput{
				Purpose:   models.EmailDeliveryPurposeRegistrationVerification,
				Recipient: "recipient@example.test",
				Settings:  mailer.Settings{Mode: models.EmailVerificationModeSMTP},
				Message:   mailer.Message{To: "recipient@example.test", Text: "verification"},
			})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Add(time.Second)
			first, second := NewWorker(database, sender), NewWorker(database, &recordingSender{})
			first.Now, second.Now = func() time.Time { return now }, func() time.Time { return now }
			if sameWorker {
				second.LockedBy = first.LockedBy
			}
			oldClaims, err := first.claimDueDeliveries(context.Background())
			if err != nil || len(oldClaims) != 1 {
				t.Fatalf("first claim = %d, %v", len(oldClaims), err)
			}
			now = now.Add(11 * time.Minute)
			currentClaims, err := second.claimDueDeliveries(context.Background())
			if err != nil || len(currentClaims) != 1 {
				t.Fatalf("reclaim = %d, %v", len(currentClaims), err)
			}
			if err := second.recordStage(context.Background(), &currentClaims[0], "connecting", "current attempt"); err != nil {
				t.Fatal(err)
			}
			old := &oldClaims[0]
			for _, action := range []func() error{
				func() error { return first.recordStage(context.Background(), old, "stale", "stale attempt") },
				func() error { return first.finishFailure(context.Background(), old, now, "stale failure", false) },
				func() error { return first.finishSuccess(context.Background(), old, now) },
				func() error { return first.deliver(context.Background(), old) },
			} {
				if err := action(); err != nil {
					t.Fatal(err)
				}
			}
			var current models.EmailDelivery
			if err := database.First(&current, "id = ?", delivery.ID).Error; err != nil {
				t.Fatal(err)
			}
			if current.Status != models.EmailDeliveryStatusDelivering || current.LockedBy != second.LockedBy ||
				current.Stage != "connecting" || strings.Contains(current.StageLog, "stale") || len(sender.messages) != 0 {
				t.Fatalf("stale claim changed current attempt: status=%s stage=%s log=%s sent=%d",
					current.Status, current.Stage, current.StageLog, len(sender.messages))
			}
		})
	}
}
