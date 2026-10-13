package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/internal/proxyurl"
	"github.com/we11adam/uddns/internal/redact"
	"github.com/we11adam/uddns/internal/restyretry"
	"github.com/we11adam/uddns/internal/retry"
	"github.com/we11adam/uddns/notifier"
)

const (
	requestTimeout    = 10 * time.Second
	responseBodyLimit = 256 << 10
)

type Telegram struct {
	Token  string `mapstructure:"token"`
	ChatID string `mapstructure:"chat_id"`
	Proxy  string `mapstructure:"proxy"`
	hc     *resty.Client
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func init() {
	notifier.Register("Telegram", "notifiers.telegram", func(v notifier.ConfigReader) (notifier.Notifier, error) {
		if !v.IsSet("notifiers.telegram") {
			return nil, notifier.ErrNotConfigured
		}

		telegram := Telegram{}
		err := v.UnmarshalKey("notifiers.telegram", &telegram)
		if err != nil {
			return nil, err
		}

		return New(&telegram)
	})
}

func New(config *Telegram) (*Telegram, error) {
	if config == nil {
		return nil, errors.New("Telegram config is nil")
	}
	if config.Token == "" || config.ChatID == "" {
		return nil, errors.New("missing required fields")
	}

	var proxy *url.URL
	if config.Proxy != "" {
		parsed, err := proxyurl.Parse(config.Proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid Telegram proxy configuration: %w", err)
		}
		proxy = parsed
	}

	telegram := *config
	telegram.hc = newHTTPClient(telegram.Token, proxy)
	return &telegram, nil
}

func newHTTPClient(token string, proxy *url.URL) *resty.Client {
	client := resty.New().
		SetTimeout(requestTimeout).
		SetResponseBodyLimit(responseBodyLimit).
		SetHeader("Content-Type", "application/json").
		SetBaseURL("https://api.telegram.org/bot" + token + "/sendMessage")
	if proxy != nil {
		client.SetProxy(proxy.String())
	}
	return restyretry.ConfigureTransientWithDelay(client, retryDelay)
}

// retryDelay returns the wait Telegram requests in the body of a rate-limited
// response, falling back to the Retry-After header.
func retryDelay(response *resty.Response) time.Duration {
	if response == nil || response.RawResponse == nil {
		return 0
	}
	apiResp := apiResponse{}
	if json.Unmarshal(response.Body(), &apiResp) == nil && apiResp.Parameters.RetryAfter > 0 {
		return time.Duration(apiResp.Parameters.RetryAfter) * time.Second
	}
	return restyretry.HeaderDelay(response)
}

func (t *Telegram) Notify(ctx context.Context, notification notifier.Notification) error {
	resp, err := t.hc.R().SetContext(ctx).SetBody(map[string]any{
		"chat_id": t.ChatID,
		"text":    notification.Message,
	}).Post("")
	if err != nil {
		return redact.Error(err, t.Token)
	}

	apiResp := apiResponse{}
	decodeErr := json.Unmarshal(resp.Body(), &apiResp)
	if !resp.IsSuccess() {
		err := t.apiError(resp.StatusCode(), apiResp)
		return retry.After(retry.ClassifyStatus(resp.StatusCode(), err), retryDelay(resp))
	}
	if decodeErr != nil {
		return redact.Error(fmt.Errorf("failed to decode Telegram API response: %w", decodeErr), t.Token)
	}
	if !apiResp.OK {
		return t.apiError(resp.StatusCode(), apiResp)
	}

	return nil
}

func (t *Telegram) apiError(statusCode int, response apiResponse) error {
	description := redact.String(response.Description, t.Token)
	if description == "" {
		return fmt.Errorf("Telegram API request failed: HTTP status %d, error code %d", statusCode, response.ErrorCode)
	}
	return fmt.Errorf("Telegram API request failed: HTTP status %d, error code %d, description %q", statusCode, response.ErrorCode, description)
}
