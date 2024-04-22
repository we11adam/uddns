package notifier

import (
	"github.com/spf13/viper"
)

type Notification struct {
	Title   string
	Message string
}

type Notifier interface {
	Notify(notification Notification) error
}

type constructor func(v *viper.Viper) (Notifier, error)

var Notifiers = make(map[string]constructor)

func Register(name string, constructor constructor) {
	Notifiers[name] = constructor
}

type Noop struct{}

func (n *Noop) Notify(_ Notification) error {
	return nil
}
func GetNotifier(v *viper.Viper) (string, Notifier) {
	for name, c := range Notifiers {
		notifier, err := c(v)
		if err == nil {
			return name, notifier
		}
	}

	return "No-op", &Noop{}
}
