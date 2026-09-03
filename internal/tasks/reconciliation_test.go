package tasks

import (
	"testing"
	"time"
)

func TestReconciliationDelayUsesAbsoluteCheckpoints(t *testing.T) {
	wants := []time.Duration{time.Second, time.Second, 3 * time.Second, 5 * time.Second}
	var elapsed time.Duration
	for attempt, want := range wants {
		got, ok := reconciliationDelay(attempt, false)
		if !ok || got != want {
			t.Fatalf("attempt %d: got %s, %v; want %s, true", attempt, got, ok, want)
		}
		elapsed += got
		if elapsed != reconciliationCheckpoints[attempt] {
			t.Fatalf("attempt %d: cumulative delay %s; want checkpoint %s", attempt, elapsed, reconciliationCheckpoints[attempt])
		}
	}
	if _, ok := reconciliationDelay(len(wants), false); ok {
		t.Fatal("expected attempts after the 10-second checkpoint to be rejected")
	}
}

func TestCloseReconciliationExtendsPositionVerificationToSixtySeconds(t *testing.T) {
	wants := []time.Duration{time.Second, time.Second, 3 * time.Second, 5 * time.Second, 20 * time.Second, 30 * time.Second}
	var elapsed time.Duration
	for attempt, want := range wants {
		got, ok := reconciliationDelay(attempt, true)
		if !ok || got != want {
			t.Fatalf("attempt %d: got %s, %v; want %s, true", attempt, got, ok, want)
		}
		elapsed += got
		if elapsed != closeReconciliationCheckpoints[attempt] {
			t.Fatalf("attempt %d: cumulative delay %s; want checkpoint %s", attempt, elapsed, closeReconciliationCheckpoints[attempt])
		}
	}
	if _, ok := reconciliationDelay(len(wants), true); ok {
		t.Fatal("expected close attempts after the 60-second checkpoint to be rejected")
	}
}

func TestFinalOrderStatuses(t *testing.T) {
	for _, status := range []string{"filled", "canceled", "failed", " FILLED "} {
		if !isFinalOrderStatus(status) {
			t.Fatalf("expected %q to be final", status)
		}
	}
	if isFinalOrderStatus("submitted") {
		t.Fatal("submitted must remain pending")
	}
}
