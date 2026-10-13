// Package retry classifies failures and retries transient ones.
package retry

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"
)

// The in-call retry budget shared by HTTP clients: up to MaxRetries retries
// with capped exponential backoff from WaitTime to MaxWaitTime.
const (
	MaxRetries  = 3
	WaitTime    = 100 * time.Millisecond
	MaxWaitTime = 2 * time.Second
)

type permanentError struct {
	err error
}

func (e *permanentError) Error() string { return e.err.Error() }

func (e *permanentError) Unwrap() error { return e.err }

// Permanent marks err as a failure that retrying cannot fix, such as rejected
// credentials or a missing DNS zone.
func Permanent(err error) error {
	if err == nil || IsPermanent(err) {
		return err
	}
	return &permanentError{err: err}
}

// IsPermanent reports whether err or an error it wraps was marked Permanent.
func IsPermanent(err error) bool {
	var permanent *permanentError
	return errors.As(err, &permanent)
}

type delayedError struct {
	err   error
	delay time.Duration
}

func (e *delayedError) Error() string { return e.err.Error() }

func (e *delayedError) Unwrap() error { return e.err }

// After marks err as retryable no sooner than delay, for example from an HTTP
// Retry-After header.
func After(err error, delay time.Duration) error {
	if err == nil || delay <= 0 {
		return err
	}
	return &delayedError{err: err, delay: delay}
}

// RetryAfter returns the delay requested by After.
func RetryAfter(err error) (time.Duration, bool) {
	var delayed *delayedError
	if errors.As(err, &delayed) {
		return delayed.delay, true
	}
	return 0, false
}

// TransientStatus reports whether an HTTP status code indicates a failure that
// may succeed when retried.
func TransientStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= http.StatusInternalServerError
}

// ClassifyStatus marks err Permanent when code is a client error that a retry
// cannot fix.
func ClassifyStatus(code int, err error) error {
	if code >= http.StatusBadRequest && code < http.StatusInternalServerError && !TransientStatus(code) {
		return Permanent(err)
	}
	return err
}

// Do calls operation until it succeeds, returns a Permanent error, ctx ends,
// or MaxRetries retries are used. operation must be safe to repeat.
func Do(ctx context.Context, operation func() error) error {
	return do(ctx, operation, sleep, rand.Float64)
}

func do(ctx context.Context, operation func() error, wait func(context.Context, time.Duration) error, jitter func() float64) error {
	delay := WaitTime
	for retries := 0; ; retries++ {
		err := operation()
		if err == nil || IsPermanent(err) || ctx.Err() != nil || retries == MaxRetries {
			return err
		}

		next := delay/2 + time.Duration(float64(delay/2)*jitter())
		if requested, ok := RetryAfter(err); ok {
			if requested > MaxWaitTime {
				return err
			}
			next = max(next, requested)
		}
		if wait(ctx, next) != nil {
			return err
		}
		delay = min(delay*2, MaxWaitTime)
	}
}

func sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
