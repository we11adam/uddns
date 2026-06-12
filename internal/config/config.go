package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const DefaultInterval = 30 * time.Second

type Config struct {
	path string
	v    *viper.Viper
}

func Load(providedPath string) (*Config, error) {
	path, err := FindFile(providedPath)
	if err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	return &Config{path: path, v: v}, nil
}

func FindFile(providedPath string) (string, error) {
	if providedPath != "" && isReadable(providedPath) {
		return providedPath, nil
	}

	locations := []string{
		os.Getenv("UDDNS_CONFIG"),
		"./uddns.yaml",
		os.Getenv("HOME") + "/.config/uddns.yaml",
		"/etc/uddns.yaml",
	}

	for _, p := range locations {
		if isReadable(p) {
			return p, nil
		}
	}

	return "", fmt.Errorf("no readable config file found in %v", locations)
}

func (c *Config) Path() string {
	return c.path
}

func (c *Config) GetString(key string) string {
	return c.v.GetString(key)
}

func (c *Config) IsSet(key string) bool {
	return c.v.IsSet(key)
}

func (c *Config) UnmarshalKey(key string, rawVal any) error {
	return c.v.UnmarshalKey(key, rawVal)
}

func (c *Config) Interval() (time.Duration, string, error) {
	value := strings.TrimSpace(os.Getenv("UDDNS_INTERVAL"))
	if value == "" {
		return DefaultInterval, "", nil
	}

	duration, err := time.ParseDuration(value)
	if err != nil {
		return DefaultInterval, value, err
	}
	return duration, value, nil
}

func isReadable(p string) bool {
	if _, err := os.Stat(p); err == nil {
		return true
	}
	return false
}
