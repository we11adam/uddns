package registry

import (
	"errors"
	"strings"
	"testing"
)

type testConfig map[string]string

func (c testConfig) GetString(key string) string {
	return c[key]
}

func (c testConfig) IsSet(key string) bool {
	_, ok := c[key]
	return ok
}

func (c testConfig) UnmarshalKey(_ string, _ any) error {
	return nil
}

func TestRegistryGetUsesRegistrationOrder(t *testing.T) {
	r := New[string]("thing", "things.use")
	r.Register("First", "things.first", func(ConfigReader) (string, error) {
		return "", ErrNotConfigured
	})
	r.Register("Second", "things.second", func(ConfigReader) (string, error) {
		return "second", nil
	})

	name, value, err := r.Get(testConfig{})
	if err != nil {
		t.Fatalf("expected registry lookup to succeed, got %v", err)
	}
	if name != "Second" || value != "second" {
		t.Fatalf("expected Second/second, got %s/%s", name, value)
	}
}

func TestRegistryRegisterRejectsAliasCollisions(t *testing.T) {
	tests := []struct {
		name              string
		existingName      string
		existingConfigKey string
		newName           string
		newConfigKey      string
	}{
		{
			name:              "new config basename matches existing name",
			existingName:      "Legacy",
			existingConfigKey: "things.first",
			newName:           "Second",
			newConfigKey:      "things.legacy",
		},
		{
			name:              "new config basename matches existing config basename",
			existingName:      "FirstThing",
			existingConfigKey: "things.first",
			newName:           "SecondThing",
			newConfigKey:      "other.first",
		},
		{
			name:              "new config basename matches normalized existing config key",
			existingName:      "First",
			existingConfigKey: "things.first",
			newName:           "Second",
			newConfigKey:      "other.things-first",
		},
	}

	constructor := func(ConfigReader) (string, error) { return "value", nil }
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New[string]("thing", "things.use")
			r.Register(tt.existingName, tt.existingConfigKey, constructor)

			defer func() {
				if recovered := recover(); recovered == nil {
					t.Fatal("expected alias collision to panic")
				}
			}()
			r.Register(tt.newName, tt.newConfigKey, constructor)
		})
	}
}

func TestRegistryGetStopsOnConfigurationError(t *testing.T) {
	r := New[string]("thing", "things.use")
	configErr := errors.New("bad config")
	r.Register("First", "things.first", func(ConfigReader) (string, error) {
		return "", configErr
	})
	r.Register("Second", "things.second", func(ConfigReader) (string, error) {
		return "second", nil
	})

	_, _, err := r.Get(testConfig{})
	if err == nil {
		t.Fatal("expected configuration error")
	}
	if !strings.Contains(err.Error(), `thing "First" configuration error`) {
		t.Fatalf("expected first config error, got %v", err)
	}
}

func TestRegistryGetUsesExplicitSelector(t *testing.T) {
	r := New[string]("thing", "things.use")
	r.Register("First", "things.first", func(ConfigReader) (string, error) {
		return "first", nil
	})
	r.Register("SecondThing", "things.second_thing", func(ConfigReader) (string, error) {
		return "second", nil
	})

	name, value, err := r.Get(testConfig{"things.use": "second_thing"})
	if err != nil {
		t.Fatalf("expected selected lookup to succeed, got %v", err)
	}
	if name != "SecondThing" || value != "second" {
		t.Fatalf("expected SecondThing/second, got %s/%s", name, value)
	}
}

func TestRegistryGetReportsSelectedButUnconfigured(t *testing.T) {
	r := New[string]("thing", "things.use")
	r.Register("First", "things.first", func(ConfigReader) (string, error) {
		return "", ErrNotConfigured
	})

	_, _, err := r.Get(testConfig{"things.use": "first"})
	if err == nil {
		t.Fatal("expected selected unconfigured error")
	}
	if !strings.Contains(err.Error(), `selected thing "first" requires config key "things.first"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistryGetReportsUnknownSelector(t *testing.T) {
	r := New[string]("thing", "things.use")
	r.Register("First", "things.first", func(ConfigReader) (string, error) {
		return "first", nil
	})

	_, _, err := r.Get(testConfig{"things.use": "missing"})
	if err == nil {
		t.Fatal("expected unknown selector error")
	}
	if !strings.Contains(err.Error(), `unknown thing "missing"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistryGetOptionalUsesFallbackWhenUnconfigured(t *testing.T) {
	r := New[string]("thing", "things.use")
	r.Register("First", "things.first", func(ConfigReader) (string, error) {
		return "", ErrNotConfigured
	})

	name, value, err := r.GetOptional(testConfig{}, "Fallback", "fallback")
	if err != nil {
		t.Fatalf("expected optional registry lookup to succeed, got %v", err)
	}
	if name != "Fallback" || value != "fallback" {
		t.Fatalf("expected Fallback/fallback, got %s/%s", name, value)
	}
}

func TestRegistryGetOptionalReportsConfiguredError(t *testing.T) {
	r := New[string]("thing", "things.use")
	r.Register("First", "things.first", func(ConfigReader) (string, error) {
		return "", errors.New("bad optional config")
	})

	_, _, err := r.GetOptional(testConfig{"things.first": "configured"}, "Fallback", "fallback")
	if err == nil {
		t.Fatal("expected configuration error")
	}
	if !strings.Contains(err.Error(), `thing "First" configuration error`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
