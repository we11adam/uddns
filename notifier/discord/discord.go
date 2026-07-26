package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/we11adam/uddns/internal/proxyurl"
	"github.com/we11adam/uddns/internal/redact"
	"github.com/we11adam/uddns/notifier"
)

const (
	requestTimeout    = 10 * time.Second
	responseBodyLimit = 256 << 10
	userAgent         = "DiscordBot (https://github.com/we11adam/uddns, 1.0)"
)

type Discord struct {
	URL string `mapstructure:"url"`
	// Optionally provide ID and Token instead of URL. If all are provided, an error will be returned.
	ID string `mapstructure:"id"`
	// Optionally provide ID and Token instead of URL. If all are provided, an error will be returned.
	Token string `mapstructure:"token"`
	Proxy string `mapstructure:"proxy"`
	hc    *resty.Client
}

type discordEmbed struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Color       int    `json:"color"`
}

type webhookMessage struct {
	Embeds []discordEmbed `json:"embeds"`
}

type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func init() {
	notifier.Register("Discord", "notifiers.discord", func(v notifier.ConfigReader) (notifier.Notifier, error) {
		if !v.IsSet("notifiers.discord") {
			return nil, notifier.ErrNotConfigured
		}

		discord := Discord{}
		err := v.UnmarshalKey("notifiers.discord", &discord)
		if err != nil {
			return nil, err
		}

		return New(&discord)
	})
}

func New(config *Discord) (discord *Discord, err error) {
	if config == nil {
		return nil, fmt.Errorf("Discord config is nil")
	}
	if config.URL != "" && (config.ID != "" || config.Token != "") {
		return nil, fmt.Errorf("Discord config must either provide url or id and token, not a combination")
	}
	if config.URL == "" {
		if config.ID == "" || config.Token == "" {
			return nil, fmt.Errorf("Discord config must provide url or id and token")
		}
		config.URL = fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", config.ID, config.Token)
	}

	discord = new(Discord)
	*discord = *config
	discord.hc, err = discord.newHTTPClient()
	if err != nil {
		return nil, err
	}
	return discord, nil
}

func (d *Discord) newHTTPClient() (*resty.Client, error) {
	webhookURL := d.URL
	if webhookURL == "" {
		webhookURL = fmt.Sprintf("https://discord.com/api/webhooks/%s/%s", url.PathEscape(d.ID), url.PathEscape(d.Token))
	}
	client := resty.New().
		SetTimeout(requestTimeout).
		SetResponseBodyLimit(responseBodyLimit).
		SetHeader("Content-Type", "application/json").
		SetHeader("User-Agent", userAgent).
		SetBaseURL(webhookURL)

	if d.Proxy != "" {
		_, err := proxyurl.Parse(d.Proxy)
		if err != nil {
			return nil, fmt.Errorf("invalid Discord proxy configuration: %w", err)
		}
		client.SetProxy(d.Proxy)
	}

	return client, nil
}

func (d *Discord) Notify(ctx context.Context, notification notifier.Notification) error {
	color := 0x000000
	switch notification.Reason {
	case notifier.ReasonIPChange:
		color = 0x3498DB
	case notifier.ReasonUpdateFailure:
		color = 0xFF0000
	case notifier.ReasonUpdateSuccess:
		color = 0x00FF00
	}

	resp, err := d.hc.R().SetContext(ctx).SetBody(&webhookMessage{
		Embeds: []discordEmbed{
			{
				Title:       notification.Title,
				Description: notification.Message,
				Color:       color,
			},
		},
	}).Post("")
	if err != nil {
		return d.redactError(err)
	}

	switch resp.StatusCode() {
	case http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusTooManyRequests:
		return d.redactError(fmt.Errorf("Discord API request failed, rate limited, retry after %s", resp.Header().Get("Retry-After")))
	}
	if resp.IsSuccess() {
		return nil
	}

	apiResp := apiResponse{}
	_ = json.Unmarshal(resp.Body(), &apiResp)
	return d.redactError(d.apiError(resp.StatusCode(), apiResp))
}

func (d *Discord) apiError(statusCode int, response apiResponse) error {
	if response.Message == "" {
		return fmt.Errorf("Discord API request failed: HTTP status %d, code %d", statusCode, response.Code)
	}
	return fmt.Errorf("Discord API request failed: HTTP status %d, code %d, message %q", statusCode, response.Code, response.Message)
}

func (d *Discord) redactError(err error) error {
	return redact.Error(err, d.Token, d.URL)
}
