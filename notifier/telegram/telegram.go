package telegram

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/we11adam/uddns/internal/redact"
	"github.com/we11adam/uddns/notifier"
)

const requestTimeout = 10 * time.Second

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

		if telegram.Token == "" || telegram.ChatID == "" {
			return nil, fmt.Errorf("missing required fields")
		}

		telegram.hc = newHTTPClient(telegram.Token, telegram.Proxy)

		return &telegram, nil
	})
}

func newHTTPClient(token, proxy string) *resty.Client {
	client := resty.New().
		SetTimeout(requestTimeout).
		SetHeader("Content-Type", "application/json").
		SetBaseURL("https://api.telegram.org/bot" + token + "/sendMessage")
	if proxy != "" {
		client.SetProxy(proxy)
	}
	return client
}

func (t *Telegram) Notify(notification notifier.Notification) error {
	resp, err := t.hc.R().SetBody(map[string]any{
		"chat_id": t.ChatID,
		"text":    notification.Message,
	}).Post("")
	if err != nil {
		return redact.Error(err, t.Token)
	}

	apiResp := apiResponse{}
	decodeErr := json.Unmarshal(resp.Body(), &apiResp)
	if !resp.IsSuccess() {
		return t.apiError(resp.StatusCode(), apiResp)
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
