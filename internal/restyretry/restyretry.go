package restyretry

import (
	"errors"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/we11adam/uddns/internal/retry"
)

const (
	MaxRetries  = retry.MaxRetries
	WaitTime    = retry.WaitTime
	MaxWaitTime = retry.MaxWaitTime
)

// ConfigureTransient adds a bounded retry policy for idempotent requests.
// Resty applies capped exponential backoff with jitter unless the server sends
// a Retry-After header.
func ConfigureTransient(client *resty.Client) *resty.Client {
	return ConfigureTransientWithDelay(client, HeaderDelay)
}

// ConfigureTransientWithDelay is ConfigureTransient for APIs that report the
// requested retry delay outside the Retry-After header. delay must accept a
// nil response and return zero when the server did not request a delay.
// Responses that ask for more than MaxWaitTime are returned without retrying,
// leaving the wait to the caller.
func ConfigureTransientWithDelay(client *resty.Client, delay func(*resty.Response) time.Duration) *resty.Client {
	return client.
		SetRetryCount(MaxRetries).
		SetRetryWaitTime(WaitTime).
		SetRetryMaxWaitTime(MaxWaitTime).
		SetRetryAfter(func(_ *resty.Client, response *resty.Response) (time.Duration, error) {
			return delay(response), nil
		}).
		AddRetryCondition(func(response *resty.Response, err error) bool {
			return shouldRetry(response, err) && delay(response) <= MaxWaitTime
		})
}

// HeaderDelay returns the delay requested by a Retry-After header in seconds
// or as an HTTP date, or zero when there is none.
func HeaderDelay(response *resty.Response) time.Duration {
	if response == nil || response.RawResponse == nil {
		return 0
	}
	value := strings.TrimSpace(response.Header().Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		if seconds <= 0 || math.IsNaN(seconds) {
			return 0
		}
		return time.Duration(min(seconds, maxDelaySeconds) * float64(time.Second))
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(time.Until(date), 0)
	}
	return 0
}

// maxDelaySeconds keeps absurd Retry-After values from overflowing.
const maxDelaySeconds = 24 * 60 * 60

func shouldRetry(response *resty.Response, err error) bool {
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			return true
		}
		var netErr net.Error
		return errors.As(err, &netErr)
	}
	if response == nil || response.RawResponse == nil {
		return false
	}
	status := response.StatusCode()
	return status == http.StatusTooManyRequests || status >= http.StatusInternalServerError
}
