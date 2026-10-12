package config

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	DefaultInterval = 30 * time.Second
	MinInterval     = 10 * time.Second
	MaxInterval     = 24 * time.Hour
)

type Config struct {
	path string
	v    *viper.Viper
}

type Job struct {
	Name     string   `mapstructure:"name"`
	Provider []string `mapstructure:"provider"`
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

	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect config file %s: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("config file %s is not a regular file", path)
	}
	if err := validateFilePermissions(path, info); err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigFile(path)
	if err := v.ReadConfig(file); err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	return &Config{path: path, v: v}, nil
}

func validateFilePermissions(path string, info os.FileInfo) error {
	if runtime.GOOS == "windows" {
		return nil
	}

	permissions := info.Mode().Perm()
	if permissions&0o077 == 0 {
		return nil
	}
	if isSystemdCredential(path, info) {
		return nil
	}
	return fmt.Errorf("config file %s permissions %04o expose credentials; use 0600", path, permissions)
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

// StringList reads a value written either as one string, a comma-separated
// string, or a list of strings. Empty items are dropped.
func (c *Config) StringList(key string) ([]string, error) {
	var values []string
	if err := c.v.UnmarshalKey(key, &values); err != nil {
		return nil, fmt.Errorf("%s must be a string or a list of strings: %w", key, err)
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			result = append(result, value)
		}
	}
	return result, nil
}

// Scoped returns a config that contains only value at key. Named instances use
// it to expose their settings below their implementation's config key, so
// constructors read them exactly like legacy configuration.
func (c *Config) Scoped(key string, value any) *Config {
	v := viper.New()
	v.Set(key, value)
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
	if duration < MinInterval || duration > MaxInterval {
		return DefaultInterval, value, fmt.Errorf("interval must be between %s and %s", MinInterval, MaxInterval)
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
