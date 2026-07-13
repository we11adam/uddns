package notifier

import (
	"context"

	"github.com/we11adam/uddns/internal/registry"
)

type Notification struct {
	Title   string
	Message string
}

type Notifier interface {
	Notify(context.Context, Notification) error
}

type ConfigReader = registry.ConfigReader

type constructor = registry.Constructor[Notifier]

var ErrNotConfigured = registry.ErrNotConfigured

var notifiers = registry.New[Notifier]("notifier", "notifiers.use")

type Noop struct{}

func (n *Noop) Notify(_ context.Context, _ Notification) error {
	return nil
}

func Register(name, configKey string, constructor constructor) {
	notifiers.Register(name, configKey, constructor)
}

func GetNotifier(config ConfigReader) (string, Notifier, error) {
	return notifiers.GetOptional(config, "No-op", &Noop{})
}
