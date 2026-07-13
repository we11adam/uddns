package registry

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

var (
	ErrNotConfigured       = errors.New("not configured")
	errNilConstructorValue = errors.New("constructor returned nil without an error")
)

type ConfigReader interface {
	GetString(key string) string
	IsSet(key string) bool
	UnmarshalKey(key string, rawVal any) error
}

type Constructor[T any] func(ConfigReader) (T, error)

type Entry[T any] struct {
	Name      string
	ConfigKey string
	New       Constructor[T]
}

type Registry[T any] struct {
	kind        string
	selectorKey string
	mu          sync.RWMutex
	entries     []Entry[T]
}

func New[T any](kind, selectorKey string) *Registry[T] {
	return &Registry[T]{
		kind:        kind,
		selectorKey: selectorKey,
	}
}

func (r *Registry[T]) Register(name, configKey string, constructor Constructor[T]) {
	if name == "" {
		panic("registry entry name cannot be empty")
	}
	if configKey == "" {
		panic("registry entry config key cannot be empty")
	}
	if constructor == nil {
		panic("registry entry constructor cannot be nil")
	}

	entry := Entry[T]{
		Name:      name,
		ConfigKey: configKey,
		New:       constructor,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.entries {
		if aliasesOverlap(existing, entry) {
			panic(fmt.Sprintf("%s registry entry already registered: %s", r.kind, name))
		}
	}

	r.entries = append(r.entries, entry)
}

func (r *Registry[T]) Get(config ConfigReader) (string, T, error) {
	entries := r.snapshot()
	selector := strings.TrimSpace(config.GetString(r.selectorKey))
	if selector != "" {
		return r.getSelected(config, selector, entries)
	}

	var zero T
	for _, entry := range entries {
		value, err := construct(entry, config)
		if err == nil {
			return entry.Name, value, nil
		}
		if errors.Is(err, ErrNotConfigured) {
			continue
		}
		return "", zero, fmt.Errorf("%s %q configuration error: %w", r.kind, entry.Name, err)
	}

	return "", zero, fmt.Errorf("no %s configured; configure one of: %s", r.kind, strings.Join(configKeys(entries), ", "))
}

func (r *Registry[T]) GetOptional(config ConfigReader, fallbackName string, fallback T) (string, T, error) {
	entries := r.snapshot()
	selector := strings.TrimSpace(config.GetString(r.selectorKey))
	if selector != "" {
		return r.getSelected(config, selector, entries)
	}

	var zero T
	for _, entry := range entries {
		if !config.IsSet(entry.ConfigKey) {
			continue
		}

		value, err := construct(entry, config)
		if err == nil {
			return entry.Name, value, nil
		}
		if errors.Is(err, ErrNotConfigured) {
			continue
		}
		return "", zero, fmt.Errorf("%s %q configuration error: %w", r.kind, entry.Name, err)
	}

	return fallbackName, fallback, nil
}

func (r *Registry[T]) ConfigKey(selector string) (string, bool) {
	for _, entry := range r.snapshot() {
		if matches(entry, selector) {
			return entry.ConfigKey, true
		}
	}
	return "", false
}

func (r *Registry[T]) getSelected(config ConfigReader, selector string, entries []Entry[T]) (string, T, error) {
	var zero T
	for _, entry := range entries {
		if !matches(entry, selector) {
			continue
		}

		value, err := construct(entry, config)
		if err == nil {
			return entry.Name, value, nil
		}
		if errors.Is(err, ErrNotConfigured) {
			return "", zero, fmt.Errorf("selected %s %q requires config key %q", r.kind, selector, entry.ConfigKey)
		}
		return "", zero, fmt.Errorf("selected %s %q configuration error: %w", r.kind, selector, err)
	}

	return "", zero, fmt.Errorf("unknown %s %q; supported values: %s", r.kind, selector, strings.Join(names(entries), ", "))
}

func (r *Registry[T]) snapshot() []Entry[T] {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Entry[T](nil), r.entries...)
}

func construct[T any](entry Entry[T], config ConfigReader) (T, error) {
	value, err := entry.New(config)
	if err == nil && isNil(value) {
		var zero T
		return zero, errNilConstructorValue
	}
	return value, err
}

func isNil[T any](value T) bool {
	reflected := reflect.ValueOf(value)
	if !reflected.IsValid() {
		return true
	}

	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return reflected.IsNil()
	default:
		return false
	}
}

func configKeys[T any](entries []Entry[T]) []string {
	keys := make([]string, 0, len(entries))
	for _, entry := range entries {
		keys = append(keys, entry.ConfigKey)
	}
	return keys
}

func names[T any](entries []Entry[T]) []string {
	values := make([]string, 0, len(entries)*2)
	for _, entry := range entries {
		values = append(values, entry.Name, configName(entry.ConfigKey))
	}
	return values
}

func matches[T any](entry Entry[T], selector string) bool {
	normalized := normalize(selector)
	for _, alias := range aliases(entry) {
		if normalized == normalize(alias) {
			return true
		}
	}
	return false
}

func aliasesOverlap[T any](left, right Entry[T]) bool {
	for _, alias := range aliases(right) {
		if matches(left, alias) {
			return true
		}
	}
	return false
}

func aliases[T any](entry Entry[T]) []string {
	return []string{entry.Name, entry.ConfigKey, configName(entry.ConfigKey)}
}

func configName(configKey string) string {
	if idx := strings.LastIndex(configKey, "."); idx >= 0 {
		return configKey[idx+1:]
	}
	return configKey
}

func normalize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	replacer := strings.NewReplacer("_", "", "-", "", ".", "", " ", "")
	return replacer.Replace(value)
}
