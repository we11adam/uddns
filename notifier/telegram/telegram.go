package telegram

import (
	"errors"
	"github.com/go-resty/resty/v2"
	"github.com/spf13/viper"
	"github.com/we11adam/uddns/notifier"
)

type Telegram struct {
	Token  string `mapstructure:"token"`
	ChatID string `mapstructure:"chat_id"`
	Proxy  string `mapstructure:"proxy"`
	hc     *resty.Client
}

func init() {
	notifier.Register("Telegram", func(v *viper.Viper) (notifier.Notifier, error) {
		telegram := Telegram{}
		err := v.UnmarshalKey("notifiers.telegram", &telegram)
		if err != nil {
			return nil, err
		}

		if telegram.Token == "" || telegram.ChatID == "" {
			return nil, errors.New("missing required fields")
		}

		telegram.hc = resty.New()
		telegram.hc.SetHeader("Content-Type", "application/json").
			SetBaseURL("https://api.telegram.org/bot" + telegram.Token + "/sendMessage")
		if telegram.Proxy != "" {
			telegram.hc.SetProxy(telegram.Proxy)
		}

		return &telegram, nil
	})
}

func (t *Telegram) Notify(notification notifier.Notification) error {
	_, err := t.hc.R().SetBody(map[string]any{
		"chat_id": t.ChatID,
		"text":    notification.Message,
	}).Post("")
	return err
}
