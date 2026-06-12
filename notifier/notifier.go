package notifier

import (
	"github.com/we11adam/uddns/internal/registry"
)

type Notification struct {
	Title   string
	Message string
}

type Notifier interface {
	Notify(notification Notification) error
}

type ConfigReader = registry.ConfigReader

type constructor = registry.Constructor[Notifier]

var ErrNotConfigured = registry.ErrNotConfigured

var notifiers = registry.New[Notifier]("notifier", "notifiers.use")

type Noop struct{}

func (n *Noop) Notify(_ Notification) error {
	return nil
}

func Register(name, configKey string, constructor constructor) {
	notifiers.Register(name, configKey, constructor)
}

func GetNotifier(config ConfigReader) (string, Notifier, error) {
	return notifiers.GetOptional(config, "No-op", &Noop{})
}
