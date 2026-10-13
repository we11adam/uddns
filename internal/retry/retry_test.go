package retry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestPermanentIsDetectedThroughWrapping(t *testing.T) {
	t.Parallel()
	base := errors.New("unauthorized")
	err := fmt.Errorf("update failed: %w", Permanent(base))

	if !IsPermanent(err) {
		t.Fatal("expected wrapped permanent error to be detected")
	}
	if !errors.Is(err, base) {
		t.Fatal("expected permanent error to unwrap to its cause")
	}
	if err.Error() != "update failed: unauthorized" {
		t.Fatalf("unexpected message %q", err.Error())
	}
	if Permanent(nil) != nil {
		t.Fatal("Permanent(nil) must be nil")
	}
	if IsPermanent(base) {
		t.Fatal("unmarked error must not be permanent")
	}
}

func TestRetryAfterIsDetectedThroughWrapping(t *testing.T) {
	t.Parallel()
	err := fmt.Errorf("notify failed: %w", After(errors.New("rate limited"), 3*time.Second))

	delay, ok := RetryAfter(err)
	if !ok || delay != 3*time.Second {
		t.Fatalf("RetryAfter = %s, %t; want 3s, true", delay, ok)
	}
	if _, ok := RetryAfter(errors.New("plain")); ok {
		t.Fatal("unmarked error must not request a delay")
	}
	if err := After(errors.New("plain"), 0); err == nil {
		t.Fatal("After must keep the error")
	} else if _, ok := RetryAfter(err); ok {
		t.Fatal("non-positive delays must be ignored")
	}
}

func TestClassifyStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		code      int
		permanent bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusNotFound, true},
		{http.StatusRequestTimeout, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusBadGateway, false},
		{http.StatusOK, false},
	}
	for _, tt := range tests {
		if got := IsPermanent(ClassifyStatus(tt.code, errors.New("failed"))); got != tt.permanent {
			t.Errorf("ClassifyStatus(%d) permanent = %t, want %t", tt.code, got, tt.permanent)
		}
	}
}

func TestDoRetriesTransientErrorsWithBackoff(t *testing.T) {
	t.Parallel()
	var waits []time.Duration
	calls := 0
	err := do(context.Background(), func() error {
		calls++
		return errors.New("temporary")
	}, recordWaits(&waits), func() float64 { return 1 })

	if err == nil || calls != MaxRetries+1 {
		t.Fatalf("calls = %d, err = %v; want %d calls and an error", calls, err, MaxRetries+1)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond}
	if fmt.Sprint(waits) != fmt.Sprint(want) {
		t.Fatalf("waits = %v, want %v", waits, want)
	}
}

func TestDoStopsOnSuccess(t *testing.T) {
	t.Parallel()
	calls := 0
	err := do(context.Background(), func() error {
		calls++
		if calls < 2 {
			return errors.New("temporary")
		}
		return nil
	}, recordWaits(new([]time.Duration)), func() float64 { return 0 })

	if err != nil || calls != 2 {
		t.Fatalf("calls = %d, err = %v; want success on second call", calls, err)
	}
}

func TestDoDoesNotRetryPermanentErrors(t *testing.T) {
	t.Parallel()
	calls := 0
	err := do(context.Background(), func() error {
		calls++
		return Permanent(errors.New("forbidden"))
	}, recordWaits(new([]time.Duration)), func() float64 { return 0 })

	if !IsPermanent(err) || calls != 1 {
		t.Fatalf("calls = %d, err = %v; want one call and a permanent error", calls, err)
	}
}

func TestDoHonorsShortRetryAfterAndGivesUpOnLongOnes(t *testing.T) {
	t.Parallel()
	var waits []time.Duration
	calls := 0
	err := do(context.Background(), func() error {
		calls++
		if calls == 1 {
			return After(errors.New("slow down"), time.Second)
		}
		return After(errors.New("slow down"), time.Minute)
	}, recordWaits(&waits), func() float64 { return 0 })

	if err == nil || calls != 2 {
		t.Fatalf("calls = %d, err = %v; want to stop after a long Retry-After", calls, err)
	}
	if len(waits) != 1 || waits[0] != time.Second {
		t.Fatalf("waits = %v, want [1s]", waits)
	}
}

func TestDoStopsWhenContextEnds(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := do(ctx, func() error {
		calls++
		cancel()
		return errors.New("temporary")
	}, recordWaits(new([]time.Duration)), func() float64 { return 0 })

	if err == nil || calls != 1 {
		t.Fatalf("calls = %d, err = %v; want one call after cancellation", calls, err)
	}
}

func recordWaits(waits *[]time.Duration) func(context.Context, time.Duration) error {
	return func(ctx context.Context, delay time.Duration) error {
		*waits = append(*waits, delay)
		return ctx.Err()
	}
}
