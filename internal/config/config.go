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

type Job struct {
	Name     string   `mapstructure:"name"`
	Provider string   `mapstructure:"provider"`
	Updater  string   `mapstructure:"updater"`
	Record   string   `mapstructure:"record"`
	Zone     string   `mapstructure:"zone"`
	Families []string `mapstructure:"families"`
	Verify   string   `mapstructure:"verify"`
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
	if providedPath != "" {
		if isReadable(providedPath) {
			return providedPath, nil
		}
		return "", fmt.Errorf("provided config file is not readable: %s", providedPath)
	}

	if envPath := os.Getenv("UDDNS_CONFIG"); envPath != "" {
		if isReadable(envPath) {
			return envPath, nil
		}
		return "", fmt.Errorf("UDDNS_CONFIG file is not readable: %s", envPath)
	}

	locations := []string{
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

func (c *Config) Jobs() ([]Job, bool, error) {
	if !c.IsSet("jobs") {
		return nil, false, nil
	}

	var jobs []Job
	if err := c.UnmarshalKey("jobs", &jobs); err != nil {
		return nil, true, err
	}
	return jobs, true, nil
}

func (c *Config) Verify() string {
	return c.GetString("verify")
}

func (j Job) VerifyMode() string {
	return j.Verify
}

func (c *Config) WithOverrides(overrides map[string]any) *Config {
	v := viper.New()
	for key, value := range c.v.AllSettings() {
		v.Set(key, value)
	}
	for key, value := range overrides {
		v.Set(key, value)
	}
	return &Config{path: c.path, v: v}
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
	file, err := os.Open(p)
	if err != nil {
		return false
	}
	defer file.Close()

	info, err := file.Stat()
	return err == nil && info.Mode().IsRegular()
}
