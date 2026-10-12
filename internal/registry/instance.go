package registry

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// TypeKey selects the implementation of a named instance.
const TypeKey = "type"

// Instance is one configured entry below a registry section, for example
// updaters.cf-work. Entries keyed by an implementation name, such as
// updaters.cloudflare, are legacy instances whose name doubles as their type.
type Instance struct {
	// Name is the configuration key below the section.
	Name string
	// Type is the registered implementation name.
	Type string
	// ConfigKey is the registered configuration root of Type. Constructors
	// read Settings from this key in a config scoped to the instance.
	ConfigKey string
	// Settings holds the instance configuration without the type selector.
	Settings any

	legacy bool
	order  int
}

// Label identifies the instance in logs and errors.
func (i Instance) Label() string {
	if i.legacy || i.Name == "" {
		return i.Type
	}
	return fmt.Sprintf("%s (%s)", i.Name, i.Type)
}

// Instances returns the configured instances below the registry section in
// registration order of their types, then by name. Empty entries are treated
// as unconfigured.
func (r *Registry[T]) Instances(config ConfigReader) ([]Instance, error) {
	section := r.section()
	if !config.IsSet(section) {
		return nil, nil
	}

	var raw map[string]any
	if err := config.UnmarshalKey(section, &raw); err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", section, err)
	}

	entries := r.snapshot()
	selectorName := configName(r.selectorKey)
	instances := make([]Instance, 0, len(raw))
	for name, settings := range raw {
		if name == selectorName || settings == nil {
			continue
		}
		instance, err := r.instance(entries, name, settings)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}

	slices.SortFunc(instances, func(a, b Instance) int {
		return cmp.Or(cmp.Compare(a.order, b.order), strings.Compare(a.Name, b.Name))
	})
	return instances, nil
}

func (r *Registry[T]) instance(entries []Entry[T], name string, settings any) (Instance, error) {
	named, namedOrder, hasNamed := find(entries, name)

	typeName := ""
	if mapping, ok := settings.(map[string]any); ok {
		if raw, ok := mapping[TypeKey]; ok {
			value, ok := raw.(string)
			if !ok || strings.TrimSpace(value) == "" {
				return Instance{}, fmt.Errorf("%s %q has an invalid %q value", r.kind, name, TypeKey)
			}
			typeName = value
			settings = withoutKey(mapping, TypeKey)
		}
	}

	if typeName == "" {
		if !hasNamed {
			return Instance{}, fmt.Errorf("%s %q needs a %q; supported types: %s", r.kind, name, TypeKey, strings.Join(names(entries), ", "))
		}
		return Instance{
			Name:      name,
			Type:      named.Name,
			ConfigKey: named.ConfigKey,
			Settings:  settings,
			legacy:    true,
			order:     namedOrder,
		}, nil
	}

	typed, typedOrder, ok := find(entries, typeName)
	if !ok {
		return Instance{}, fmt.Errorf("%s %q has unknown type %q; supported types: %s", r.kind, name, typeName, strings.Join(names(entries), ", "))
	}
	if hasNamed && named.Name != typed.Name {
		return Instance{}, fmt.Errorf("%s name %q is reserved for type %s", r.kind, name, named.Name)
	}
	return Instance{
		Name:      name,
		Type:      typed.Name,
		ConfigKey: typed.ConfigKey,
		Settings:  settings,
		legacy:    hasNamed,
		order:     typedOrder,
	}, nil
}

// Find resolves a selector to a configured instance. Instance names match
// first; implementation aliases such as "IpService" match the only instance of
// that type so existing selectors keep working.
func (r *Registry[T]) Find(instances []Instance, selector string) (Instance, error) {
	selector = strings.TrimSpace(selector)
	for _, instance := range instances {
		if strings.EqualFold(instance.Name, selector) {
			return instance, nil
		}
	}

	entry, _, ok := find(r.snapshot(), selector)
	if !ok {
		return Instance{}, fmt.Errorf("unknown %s %q; configured: %s", r.kind, selector, instanceNames(instances))
	}

	var matches []Instance
	for _, instance := range instances {
		if instance.Type == entry.Name {
			matches = append(matches, instance)
		}
	}
	switch len(matches) {
	case 0:
		return Instance{}, fmt.Errorf("selected %s %q requires config key %q", r.kind, selector, entry.ConfigKey)
	case 1:
		return matches[0], nil
	default:
		return Instance{}, fmt.Errorf("%s %q is ambiguous; select one of: %s", r.kind, selector, instanceNames(matches))
	}
}

// Select resolves selectors in order. Without selectors it returns the first
// configured instance, matching the historical single-implementation behavior.
func (r *Registry[T]) Select(instances []Instance, selectors []string) ([]Instance, error) {
	if len(selectors) == 0 {
		if len(instances) == 0 {
			return nil, fmt.Errorf("no %s configured; configure one of: %s", r.kind, strings.Join(configKeys(r.snapshot()), ", "))
		}
		return instances[:1], nil
	}

	selected := make([]Instance, 0, len(selectors))
	for _, selector := range selectors {
		instance, err := r.Find(instances, selector)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(selected, func(existing Instance) bool { return existing.Name == instance.Name }) {
			return nil, fmt.Errorf("%s %q is selected more than once", r.kind, instance.Name)
		}
		selected = append(selected, instance)
	}
	return selected, nil
}

// Build constructs an instance from a config that exposes its settings below
// instance.ConfigKey.
func (r *Registry[T]) Build(instance Instance, config ConfigReader) (T, error) {
	var zero T
	entry, _, ok := find(r.snapshot(), instance.Type)
	if !ok {
		return zero, fmt.Errorf("unknown %s type %q", r.kind, instance.Type)
	}

	value, err := construct(entry, config)
	if errors.Is(err, ErrNotConfigured) {
		return zero, fmt.Errorf("%s %q has no settings", r.kind, instance.Name)
	}
	if err != nil {
		return zero, fmt.Errorf("%s %q configuration error: %w", r.kind, instance.Label(), err)
	}
	return value, nil
}

func (r *Registry[T]) section() string {
	if idx := strings.LastIndex(r.selectorKey, "."); idx >= 0 {
		return r.selectorKey[:idx]
	}
	return r.selectorKey
}

func find[T any](entries []Entry[T], selector string) (Entry[T], int, bool) {
	for i, entry := range entries {
		if matches(entry, selector) {
			return entry, i, true
		}
	}
	return Entry[T]{}, 0, false
}

func withoutKey(mapping map[string]any, key string) map[string]any {
	result := maps.Clone(mapping)
	delete(result, key)
	return result
}

func instanceNames(instances []Instance) string {
	if len(instances) == 0 {
		return "none"
	}
	values := make([]string, 0, len(instances))
	for _, instance := range instances {
		values = append(values, instance.Name)
	}
	return strings.Join(values, ", ")
}
