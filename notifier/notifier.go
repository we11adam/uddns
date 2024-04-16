package notifier

import "github.com/spf13/viper"

type Notification struct {
	Message string
}

type Notifier interface {
	Notify(notification Notification) error
}

var Notifiers = make(map[string]func(v *viper.Viper) (Notifier, error))

func Register(name string, constructor func(v *viper.Viper) (Notifier, error)) {
	Notifiers[name] = constructor
}
