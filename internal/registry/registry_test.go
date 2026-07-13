package registry

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type testConfig map[string]string

type testService interface {
	name() string
}

type testServiceImpl struct{}

func (*testServiceImpl) name() string {
	return "test"
}

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

func TestRegistryConcurrentRegisterAndGet(t *testing.T) {
	r := New[string]("thing", "things.use")
	r.Register("Default", "things.default", func(ConfigReader) (string, error) {
		return "default", nil
	})

	const (
		writers           = 4
		registrationsEach = 50
		readers           = 8
		lookupsEach       = 200
	)
	start := make(chan struct{})
	errs := make(chan error, readers*lookupsEach*2)
	var wg sync.WaitGroup

	for writer := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for registration := range registrationsEach {
				name := fmt.Sprintf("Writer%dEntry%d", writer, registration)
				configKey := fmt.Sprintf("things.writer_%d_entry_%d", writer, registration)
				r.Register(name, configKey, func(ConfigReader) (string, error) {
					return "registered", nil
				})
			}
		}()
	}

	config := testConfig{"things.default": "configured"}
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range lookupsEach {
				name, value, err := r.Get(config)
				if err != nil || name != "Default" || value != "default" {
					errs <- fmt.Errorf("Get returned %q/%q, err=%v", name, value, err)
				}

				name, value, err = r.GetOptional(config, "Fallback", "fallback")
				if err != nil || name != "Default" || value != "default" {
					errs <- fmt.Errorf("GetOptional returned %q/%q, err=%v", name, value, err)
				}
			}
		}()
	}

	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	entries := r.snapshot()
	wantEntries := 1 + writers*registrationsEach
	if len(entries) != wantEntries {
		t.Fatalf("expected %d registered entries, got %d", wantEntries, len(entries))
	}
}

func TestRegistryGetDoesNotHoldLockWhileCallingConstructor(t *testing.T) {
	r := New[string]("thing", "things.use")
	r.Register("First", "things.first", func(ConfigReader) (string, error) {
		r.Register("Second", "things.second", func(ConfigReader) (string, error) {
			return "second", nil
		})
		return "first", nil
	})

	type result struct {
		name  string
		value string
		err   error
	}
	results := make(chan result, 1)
	go func() {
		name, value, err := r.Get(testConfig{})
		results <- result{name: name, value: value, err: err}
	}()

	select {
	case got := <-results:
		if got.err != nil || got.name != "First" || got.value != "first" {
			t.Fatalf("Get returned %q/%q, err=%v", got.name, got.value, got.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Get deadlocked while constructor registered another entry")
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

func TestRegistryGetRejectsNilConstructorValue(t *testing.T) {
	r := New[any]("thing", "things.use")
	r.Register("NilThing", "things.nil_thing", func(ConfigReader) (any, error) {
		return nil, nil
	})

	name, value, err := r.Get(testConfig{})
	if err == nil {
		t.Fatal("expected nil constructor value to return an error")
	}
	if name != "" || value != nil {
		t.Fatalf("expected empty result, got %q/%#v", name, value)
	}
	if !strings.Contains(err.Error(), `thing "NilThing" configuration error: constructor returned nil without an error`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegistryRejectsTypedNilConstructorValue(t *testing.T) {
	r := New[testService]("thing", "things.use")
	r.Register("NilThing", "things.nil_thing", func(ConfigReader) (testService, error) {
		var service *testServiceImpl
		return service, nil
	})

	tests := []struct {
		name       string
		config     testConfig
		lookup     func(testConfig) (string, testService, error)
		wantPrefix string
	}{
		{
			name:   "Get",
			config: testConfig{},
			lookup: func(config testConfig) (string, testService, error) {
				return r.Get(config)
			},
			wantPrefix: `thing "NilThing" configuration error: constructor returned nil without an error`,
		},
		{
			name:   "GetOptional",
			config: testConfig{"things.nil_thing": "configured"},
			lookup: func(config testConfig) (string, testService, error) {
				return r.GetOptional(config, "Fallback", &testServiceImpl{})
			},
			wantPrefix: `thing "NilThing" configuration error: constructor returned nil without an error`,
		},
		{
			name:   "selected",
			config: testConfig{"things.use": "nil_thing"},
			lookup: func(config testConfig) (string, testService, error) {
				return r.Get(config)
			},
			wantPrefix: `selected thing "nil_thing" configuration error: constructor returned nil without an error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, value, err := tt.lookup(tt.config)
			if err == nil {
				t.Fatal("expected typed-nil constructor value to return an error")
			}
			if name != "" || value != nil {
				t.Fatalf("expected empty result, got %q/%#v", name, value)
			}
			if !strings.Contains(err.Error(), tt.wantPrefix) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
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

func TestRegistryConfigKeyResolvesAliases(t *testing.T) {
	r := New[string]("thing", "things.use")
	r.Register("SecondThing", "things.second_thing", func(ConfigReader) (string, error) {
		return "second", nil
	})

	for _, selector := range []string{"SecondThing", "second_thing", "things.second_thing", "second-thing"} {
		configKey, ok := r.ConfigKey(selector)
		if !ok || configKey != "things.second_thing" {
			t.Fatalf("ConfigKey(%q) = %q, %v", selector, configKey, ok)
		}
	}
	if configKey, ok := r.ConfigKey("missing"); ok || configKey != "" {
		t.Fatalf("expected missing selector to return no config key, got %q, %v", configKey, ok)
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
