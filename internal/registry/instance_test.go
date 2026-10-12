package registry

import (
	"strings"
	"testing"
)

// sectionConfig serves one raw section to Instances.
type sectionConfig map[string]any

func (c sectionConfig) GetString(string) string {
	return ""
}

func (c sectionConfig) IsSet(key string) bool {
	return key == "things"
}

func (c sectionConfig) UnmarshalKey(_ string, rawVal any) error {
	*rawVal.(*map[string]any) = c
	return nil
}

func newInstanceRegistry() *Registry[string] {
	r := New[string]("thing", "things.use")
	r.Register("FirstThing", "things.first_thing", func(ConfigReader) (string, error) {
		return "first", nil
	})
	r.Register("Second", "things.second", func(ConfigReader) (string, error) {
		return "", ErrNotConfigured
	})
	return r
}

func TestInstancesResolveLegacyAndNamedEntries(t *testing.T) {
	t.Parallel()
	r := newInstanceRegistry()

	instances, err := r.Instances(sectionConfig{
		"use":         "named",
		"empty":       nil,
		"second":      map[string]any{"token": "legacy"},
		"zeta":        map[string]any{"type": "first-thing", "token": "z"},
		"alpha":       map[string]any{"type": "FirstThing", "token": "a"},
		"first_thing": []any{"list"},
	})
	if err != nil {
		t.Fatalf("Instances returned error: %v", err)
	}

	got := make([]string, 0, len(instances))
	for _, instance := range instances {
		got = append(got, instance.Name+"="+instance.Type)
	}
	want := "alpha=FirstThing,first_thing=FirstThing,zeta=FirstThing,second=Second"
	if strings.Join(got, ",") != want {
		t.Fatalf("unexpected instances: %v", got)
	}
	if settings := instances[0].Settings.(map[string]any); settings["type"] != nil || settings["token"] != "a" {
		t.Fatalf("expected type to be removed from settings, got %#v", settings)
	}
	if instances[0].Label() != "alpha (FirstThing)" || instances[1].Label() != "FirstThing" {
		t.Fatalf("unexpected labels: %q, %q", instances[0].Label(), instances[1].Label())
	}
	if instances[0].ConfigKey != "things.first_thing" {
		t.Fatalf("unexpected config key: %q", instances[0].ConfigKey)
	}
}

func TestInstancesRejectInvalidEntries(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		section sectionConfig
		wantErr string
	}{
		{"missing type", sectionConfig{"home": map[string]any{}}, `thing "home" needs a "type"`},
		{"unknown type", sectionConfig{"home": map[string]any{"type": "missing"}}, `thing "home" has unknown type "missing"`},
		{"invalid type", sectionConfig{"home": map[string]any{"type": 3}}, `thing "home" has an invalid "type" value`},
		{"reserved name", sectionConfig{"second": map[string]any{"type": "first_thing"}}, `thing name "second" is reserved for type Second`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := newInstanceRegistry().Instances(tt.section)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestFindResolvesNamesAndUniqueTypes(t *testing.T) {
	t.Parallel()
	r := newInstanceRegistry()
	instances, err := r.Instances(sectionConfig{
		"a":      map[string]any{"type": "first_thing"},
		"b":      map[string]any{"type": "first_thing"},
		"second": map[string]any{},
	})
	if err != nil {
		t.Fatalf("Instances returned error: %v", err)
	}

	if instance, err := r.Find(instances, " B "); err != nil || instance.Name != "b" {
		t.Fatalf("expected name lookup, got %+v, %v", instance, err)
	}
	if instance, err := r.Find(instances, "things.second"); err != nil || instance.Name != "second" {
		t.Fatalf("expected type alias lookup, got %+v, %v", instance, err)
	}
	if _, err := r.Find(instances, "FirstThing"); err == nil || !strings.Contains(err.Error(), `thing "FirstThing" is ambiguous; select one of: a, b`) {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
	if _, err := r.Find(nil, "second"); err == nil || !strings.Contains(err.Error(), `selected thing "second" requires config key "things.second"`) {
		t.Fatalf("expected unconfigured type error, got %v", err)
	}
	if _, err := r.Find(instances, "missing"); err == nil || !strings.Contains(err.Error(), `unknown thing "missing"; configured: a, b, second`) {
		t.Fatalf("expected unknown selector error, got %v", err)
	}
}

func TestSelectResolvesSelectorsInOrder(t *testing.T) {
	t.Parallel()
	r := newInstanceRegistry()
	instances, err := r.Instances(sectionConfig{
		"a":      map[string]any{"type": "first_thing"},
		"second": map[string]any{},
	})
	if err != nil {
		t.Fatalf("Instances returned error: %v", err)
	}

	selected, err := r.Select(instances, []string{"second", "a"})
	if err != nil || len(selected) != 2 || selected[0].Name != "second" || selected[1].Name != "a" {
		t.Fatalf("unexpected selection: %+v, %v", selected, err)
	}
	selected, err = r.Select(instances, nil)
	if err != nil || len(selected) != 1 || selected[0].Name != "a" {
		t.Fatalf("expected first instance by default, got %+v, %v", selected, err)
	}
	if _, err := r.Select(instances, []string{"a", "A"}); err == nil || !strings.Contains(err.Error(), `thing "a" is selected more than once`) {
		t.Fatalf("expected duplicate selection error, got %v", err)
	}
	if _, err := r.Select(nil, nil); err == nil || !strings.Contains(err.Error(), "no thing configured; configure one of: things.first_thing, things.second") {
		t.Fatalf("expected no instance error, got %v", err)
	}
}

func TestBuildReportsConstructorErrors(t *testing.T) {
	t.Parallel()
	r := newInstanceRegistry()

	value, err := r.Build(Instance{Name: "a", Type: "FirstThing"}, testConfig{})
	if err != nil || value != "first" {
		t.Fatalf("expected build to succeed, got %q, %v", value, err)
	}
	if _, err := r.Build(Instance{Name: "second", Type: "Second", legacy: true}, testConfig{}); err == nil || !strings.Contains(err.Error(), `thing "second" has no settings`) {
		t.Fatalf("expected missing settings error, got %v", err)
	}
	if _, err := r.Build(Instance{Name: "x", Type: "Missing"}, testConfig{}); err == nil || !strings.Contains(err.Error(), `unknown thing type "Missing"`) {
		t.Fatalf("expected unknown type error, got %v", err)
	}
}
