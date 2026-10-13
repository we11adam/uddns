package restyretry

import (
	"errors"
	"net"
	"net/http"
	"net/url"

	"github.com/go-resty/resty/v2"

	"github.com/we11adam/uddns/internal/retry"
)

const (
	MaxRetries  = retry.MaxRetries
	WaitTime    = retry.WaitTime
	MaxWaitTime = retry.MaxWaitTime
)

// ConfigureTransient adds a bounded retry policy for idempotent requests.
// Resty applies capped exponential backoff with jitter when RetryAfter is nil.
func ConfigureTransient(client *resty.Client) *resty.Client {
	return client.
		SetRetryCount(MaxRetries).
		SetRetryWaitTime(WaitTime).
		SetRetryMaxWaitTime(MaxWaitTime).
		AddRetryCondition(shouldRetry)
}

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
