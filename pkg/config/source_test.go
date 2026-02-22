package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceRegistryMethods(t *testing.T) {
	s := Source{
		Name:     "test",
		Registry: "myregistry.example.com/team",
	}

	if got := s.ToolchainRegistry(); got != "myregistry.example.com/team/klaus-toolchains" {
		t.Errorf("ToolchainRegistry() = %q, want convention-based path", got)
	}
	if got := s.PersonalityRegistry(); got != "myregistry.example.com/team/klaus-personalities" {
		t.Errorf("PersonalityRegistry() = %q, want convention-based path", got)
	}
	if got := s.PluginRegistry(); got != "myregistry.example.com/team/klaus-plugins" {
		t.Errorf("PluginRegistry() = %q, want convention-based path", got)
	}
}

func TestSourceRegistryOverrides(t *testing.T) {
	s := Source{
		Name:          "custom",
		Registry:      "myregistry.example.com/custom",
		Toolchains:    "myregistry.example.com/custom/tools",
		Personalities: "myregistry.example.com/custom/personas",
		Plugins:       "myregistry.example.com/custom/addons",
	}

	if got := s.ToolchainRegistry(); got != "myregistry.example.com/custom/tools" {
		t.Errorf("ToolchainRegistry() = %q, want override value", got)
	}
	if got := s.PersonalityRegistry(); got != "myregistry.example.com/custom/personas" {
		t.Errorf("PersonalityRegistry() = %q, want override value", got)
	}
	if got := s.PluginRegistry(); got != "myregistry.example.com/custom/addons" {
		t.Errorf("PluginRegistry() = %q, want override value", got)
	}
}

func TestDefaultSourceConfig(t *testing.T) {
	sc := DefaultSourceConfig()
	if len(sc.Sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sc.Sources))
	}
	if sc.Sources[0].Name != DefaultSourceName {
		t.Errorf("expected default source name %q, got %q", DefaultSourceName, sc.Sources[0].Name)
	}
	if sc.Sources[0].Registry != DefaultSourceRegistry {
		t.Errorf("expected default registry %q, got %q", DefaultSourceRegistry, sc.Sources[0].Registry)
	}
	if !sc.Sources[0].Default {
		t.Error("default source should have Default=true")
	}
}

func TestLoadSourceConfig_FileNotFound(t *testing.T) {
	sc, err := LoadSourceConfig(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	if err != nil {
		t.Fatalf("LoadSourceConfig() returned error for missing file: %v", err)
	}
	if len(sc.Sources) != 1 {
		t.Fatalf("expected 1 built-in source, got %d", len(sc.Sources))
	}
	if sc.Sources[0].Name != DefaultSourceName {
		t.Errorf("expected built-in source, got %q", sc.Sources[0].Name)
	}
}

func TestLoadSourceConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.yaml")

	content := `sources:
  - name: giantswarm
    registry: gsoci.azurecr.io/giantswarm
    default: true
  - name: my-team
    registry: myregistry.example.com/my-team
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	sc, err := LoadSourceConfig(path)
	if err != nil {
		t.Fatalf("LoadSourceConfig() returned error: %v", err)
	}

	if len(sc.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sc.Sources))
	}
	if sc.Sources[1].Name != "my-team" {
		t.Errorf("second source name = %q, want %q", sc.Sources[1].Name, "my-team")
	}
}

func TestLoadSourceConfig_BuiltinInjected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.yaml")

	content := `sources:
  - name: my-team
    registry: myregistry.example.com/my-team
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	sc, err := LoadSourceConfig(path)
	if err != nil {
		t.Fatalf("LoadSourceConfig() returned error: %v", err)
	}

	if len(sc.Sources) != 2 {
		t.Fatalf("expected 2 sources (built-in + user), got %d", len(sc.Sources))
	}
	if sc.Sources[0].Name != DefaultSourceName {
		t.Errorf("first source should be built-in, got %q", sc.Sources[0].Name)
	}
}

func TestSourceConfigSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.yaml")

	sc := DefaultSourceConfig()
	if err := sc.Add(Source{Name: "team-a", Registry: "reg.example.com/a"}); err != nil {
		t.Fatal(err)
	}
	if err := sc.SaveTo(path); err != nil {
		t.Fatalf("SaveTo() returned error: %v", err)
	}

	loaded, err := LoadSourceConfig(path)
	if err != nil {
		t.Fatalf("LoadSourceConfig() returned error: %v", err)
	}
	if len(loaded.Sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(loaded.Sources))
	}
	if loaded.Sources[1].Name != "team-a" {
		t.Errorf("expected team-a, got %q", loaded.Sources[1].Name)
	}
}

func TestSourceConfigAdd_Duplicate(t *testing.T) {
	sc := DefaultSourceConfig()
	err := sc.Add(Source{Name: DefaultSourceName, Registry: "whatever"})
	if err == nil {
		t.Fatal("expected error when adding duplicate source")
	}
}

func TestSourceConfigAdd_InvalidName(t *testing.T) {
	sc := DefaultSourceConfig()
	err := sc.Add(Source{Name: "123invalid", Registry: "whatever"})
	if err == nil {
		t.Fatal("expected error for invalid name")
	}
}

func TestSourceConfigAdd_EmptyRegistry(t *testing.T) {
	sc := DefaultSourceConfig()
	err := sc.Add(Source{Name: "valid-name", Registry: ""})
	if err == nil {
		t.Fatal("expected error for empty registry")
	}
}

func TestSourceConfigRemove(t *testing.T) {
	sc := DefaultSourceConfig()
	_ = sc.Add(Source{Name: "removable", Registry: "reg.example.com/x"})

	if err := sc.Remove("removable"); err != nil {
		t.Fatalf("Remove() returned error: %v", err)
	}
	if len(sc.Sources) != 1 {
		t.Fatalf("expected 1 source after remove, got %d", len(sc.Sources))
	}
}

func TestSourceConfigRemove_Builtin(t *testing.T) {
	sc := DefaultSourceConfig()
	err := sc.Remove(DefaultSourceName)
	if err == nil {
		t.Fatal("expected error when removing built-in source")
	}
}

func TestSourceConfigRemove_NotFound(t *testing.T) {
	sc := DefaultSourceConfig()
	err := sc.Remove("nonexistent")
	if err == nil {
		t.Fatal("expected error when removing nonexistent source")
	}
}

func TestSourceConfigSetDefault(t *testing.T) {
	sc := DefaultSourceConfig()
	_ = sc.Add(Source{Name: "team-b", Registry: "reg.example.com/b"})

	if err := sc.SetDefault("team-b"); err != nil {
		t.Fatalf("SetDefault() returned error: %v", err)
	}

	for _, s := range sc.Sources {
		if s.Name == "team-b" && !s.Default {
			t.Error("team-b should be default")
		}
		if s.Name == DefaultSourceName && s.Default {
			t.Error("giantswarm should no longer be default")
		}
	}
}

func TestSourceConfigSetDefault_NotFound(t *testing.T) {
	sc := DefaultSourceConfig()
	err := sc.SetDefault("nonexistent")
	if err == nil {
		t.Fatal("expected error when setting default to nonexistent source")
	}
}

func TestSourceConfigGet(t *testing.T) {
	sc := DefaultSourceConfig()
	s := sc.Get(DefaultSourceName)
	if s == nil {
		t.Fatal("expected to find built-in source")
	}
	if s.Name != DefaultSourceName {
		t.Errorf("expected %q, got %q", DefaultSourceName, s.Name)
	}

	if sc.Get("nonexistent") != nil {
		t.Error("expected nil for nonexistent source")
	}
}

func TestSourceConfigValidate_DuplicateNames(t *testing.T) {
	sc := &SourceConfig{
		Sources: []Source{
			{Name: "a", Registry: "reg1"},
			{Name: "a", Registry: "reg2"},
		},
	}
	if err := sc.Validate(); err == nil {
		t.Fatal("expected validation error for duplicate names")
	}
}

func TestSourceConfigValidate_EmptyRegistry(t *testing.T) {
	sc := &SourceConfig{
		Sources: []Source{
			{Name: "valid", Registry: ""},
		},
	}
	if err := sc.Validate(); err == nil {
		t.Fatal("expected validation error for empty registry")
	}
}

func TestSourceConfigValidate_MultipleDefaults(t *testing.T) {
	sc := &SourceConfig{
		Sources: []Source{
			{Name: "a", Registry: "reg1", Default: true},
			{Name: "b", Registry: "reg2", Default: true},
		},
	}
	if err := sc.Validate(); err == nil {
		t.Fatal("expected validation error for multiple defaults")
	}
}

func TestEnsureBuiltin_RespectsExistingDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.yaml")

	content := `sources:
  - name: my-team
    registry: myregistry.example.com/my-team
    default: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	sc, err := LoadSourceConfig(path)
	if err != nil {
		t.Fatalf("LoadSourceConfig() returned error: %v", err)
	}

	defaultCount := 0
	for _, s := range sc.Sources {
		if s.Default {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Errorf("expected exactly 1 default, got %d", defaultCount)
	}
}

func TestNewSourceResolver_Default(t *testing.T) {
	r := DefaultSourceResolver()
	got := r.ResolvePluginRef("gs-base")
	want := "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base"
	if got != want {
		t.Errorf("ResolvePluginRef(%q) = %q, want %q", "gs-base", got, want)
	}
}

func TestSourceResolverResolvePluginRef(t *testing.T) {
	r := NewSourceResolver([]Source{
		{Name: "custom", Registry: "custom.io/org"},
		{Name: "giantswarm", Registry: "gsoci.azurecr.io/giantswarm"},
	})

	tests := []struct {
		name string
		ref  string
		want string
	}{
		{
			name: "short name resolves to first source",
			ref:  "my-plugin",
			want: "custom.io/org/klaus-plugins/my-plugin",
		},
		{
			name: "short name with tag",
			ref:  "my-plugin:v1.0.0",
			want: "custom.io/org/klaus-plugins/my-plugin:v1.0.0",
		},
		{
			name: "full ref unchanged",
			ref:  "other.io/repo/plugin:v2.0.0",
			want: "other.io/repo/plugin:v2.0.0",
		},
		{
			name: "empty ref",
			ref:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.ResolvePluginRef(tt.ref)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSourceResolverResolvePersonalityRef(t *testing.T) {
	r := NewSourceResolver([]Source{
		{Name: "team", Registry: "team.io/x"},
	})

	got := r.ResolvePersonalityRef("sre")
	want := "team.io/x/klaus-personalities/sre"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSourceResolverResolveToolchainRef(t *testing.T) {
	r := NewSourceResolver([]Source{
		{Name: "team", Registry: "team.io/x"},
	})

	got := r.ResolveToolchainRef("go")
	want := "team.io/x/klaus-toolchains/go"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSourceResolverForSource(t *testing.T) {
	r := NewSourceResolver([]Source{
		{Name: "giantswarm", Registry: "gsoci.azurecr.io/giantswarm"},
		{Name: "team", Registry: "team.io/x"},
	})

	filtered, err := r.ForSource("team")
	if err != nil {
		t.Fatalf("ForSource() returned error: %v", err)
	}

	got := filtered.ResolvePluginRef("my-plugin")
	want := "team.io/x/klaus-plugins/my-plugin"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSourceResolverForSource_NotFound(t *testing.T) {
	r := DefaultSourceResolver()
	_, err := r.ForSource("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent source")
	}
}

func TestSourceResolverRegistries(t *testing.T) {
	r := NewSourceResolver([]Source{
		{Name: "a", Registry: "reg-a.io/x", Default: true},
		{Name: "b", Registry: "reg-b.io/y"},
	})

	plugins := r.PluginRegistries()
	if len(plugins) != 2 {
		t.Fatalf("expected 2 plugin registries, got %d", len(plugins))
	}
	if plugins[0].Source != "a" || plugins[0].Registry != "reg-a.io/x/klaus-plugins" {
		t.Errorf("unexpected first plugin registry: %+v", plugins[0])
	}
	if plugins[1].Source != "b" || plugins[1].Registry != "reg-b.io/y/klaus-plugins" {
		t.Errorf("unexpected second plugin registry: %+v", plugins[1])
	}

	personalities := r.PersonalityRegistries()
	if len(personalities) != 2 {
		t.Fatalf("expected 2 personality registries, got %d", len(personalities))
	}
	if personalities[0].Registry != "reg-a.io/x/klaus-personalities" {
		t.Errorf("unexpected personality registry: %q", personalities[0].Registry)
	}

	toolchains := r.ToolchainRegistries()
	if len(toolchains) != 2 {
		t.Fatalf("expected 2 toolchain registries, got %d", len(toolchains))
	}
	if toolchains[1].Registry != "reg-b.io/y/klaus-toolchains" {
		t.Errorf("unexpected toolchain registry: %q", toolchains[1].Registry)
	}
}

func TestSourceResolverDefaultFirst(t *testing.T) {
	r := NewSourceResolver([]Source{
		{Name: "first", Registry: "first.io/x"},
		{Name: "default-src", Registry: "default.io/x", Default: true},
		{Name: "last", Registry: "last.io/x"},
	})

	got := r.ResolvePluginRef("my-plugin")
	want := "default.io/x/klaus-plugins/my-plugin"
	if got != want {
		t.Errorf("ResolvePluginRef() = %q, want default source %q", got, want)
	}

	sources := r.Sources()
	if sources[0].Name != "default-src" {
		t.Errorf("expected default source first, got %q", sources[0].Name)
	}
}

func TestSourceResolverDefaultOnly(t *testing.T) {
	r := NewSourceResolver([]Source{
		{Name: "first", Registry: "first.io/x"},
		{Name: "default-src", Registry: "default.io/x", Default: true},
		{Name: "last", Registry: "last.io/x"},
	})

	d := r.DefaultOnly()
	sources := d.Sources()
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Name != "default-src" {
		t.Errorf("expected default source, got %q", sources[0].Name)
	}

	got := d.ResolvePluginRef("my-plugin")
	want := "default.io/x/klaus-plugins/my-plugin"
	if got != want {
		t.Errorf("DefaultOnly().ResolvePluginRef() = %q, want %q", got, want)
	}

	registries := d.PluginRegistries()
	if len(registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(registries))
	}
}

func TestSourceResolverDefaultOnly_SingleSource(t *testing.T) {
	r := NewSourceResolver([]Source{
		{Name: "only", Registry: "only.io/x", Default: true},
	})

	d := r.DefaultOnly()
	if len(d.Sources()) != 1 {
		t.Fatal("expected 1 source")
	}
	if d.Sources()[0].Name != "only" {
		t.Errorf("expected 'only', got %q", d.Sources()[0].Name)
	}
}

func TestSourceResolverSourcesReturnsCopy(t *testing.T) {
	r := NewSourceResolver([]Source{
		{Name: "a", Registry: "a.io/x", Default: true},
	})
	sources := r.Sources()
	sources[0].Name = "mutated"
	if r.Sources()[0].Name == "mutated" {
		t.Error("Sources() should return a copy, not the internal slice")
	}
}

func TestBackwardCompatible_ResolveRefs(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) string
		ref  string
		want string
	}{
		{
			name: "plugin short name still works",
			fn:   ResolvePluginRef,
			ref:  "gs-base",
			want: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base",
		},
		{
			name: "personality short name still works",
			fn:   ResolvePersonalityRef,
			ref:  "sre",
			want: "gsoci.azurecr.io/giantswarm/klaus-personalities/sre",
		},
		{
			name: "toolchain short name still works",
			fn:   ResolveToolchainRef,
			ref:  "go",
			want: "gsoci.azurecr.io/giantswarm/klaus-toolchains/go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn(tt.ref)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateSourceName(t *testing.T) {
	valid := []string{"giantswarm", "my-team", "A1", "x"}
	for _, name := range valid {
		if err := ValidateSourceName(name); err != nil {
			t.Errorf("ValidateSourceName(%q) returned error: %v", name, err)
		}
	}

	invalid := []string{"", "1starts-with-digit", "-starts-with-dash", "has_underscore"}
	for _, name := range invalid {
		if err := ValidateSourceName(name); err == nil {
			t.Errorf("ValidateSourceName(%q) expected error", name)
		}
	}
}

func TestValidateSourceName_ErrorMentionsUnderscores(t *testing.T) {
	err := ValidateSourceName("has_underscore")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "underscore") {
		t.Errorf("error message should mention underscores: %v", err)
	}
}

func TestSourceConfigUpdate(t *testing.T) {
	sc := DefaultSourceConfig()
	_ = sc.Add(Source{Name: "team-a", Registry: "reg.example.com/a"})

	err := sc.Update("team-a", Source{Registry: "new-reg.example.com/a"})
	if err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}

	s := sc.Get("team-a")
	if s.Registry != "new-reg.example.com/a" {
		t.Errorf("registry not updated: got %q", s.Registry)
	}
}

func TestSourceConfigUpdate_PartialPatch(t *testing.T) {
	sc := DefaultSourceConfig()
	_ = sc.Add(Source{
		Name:          "team-a",
		Registry:      "reg.example.com/a",
		Toolchains:    "reg.example.com/a/my-toolchains",
		Personalities: "reg.example.com/a/my-personalities",
	})

	err := sc.Update("team-a", Source{Toolchains: "reg.example.com/a/new-toolchains"})
	if err != nil {
		t.Fatalf("Update() returned error: %v", err)
	}

	s := sc.Get("team-a")
	if s.Registry != "reg.example.com/a" {
		t.Errorf("registry changed unexpectedly: got %q", s.Registry)
	}
	if s.Toolchains != "reg.example.com/a/new-toolchains" {
		t.Errorf("toolchains not updated: got %q", s.Toolchains)
	}
	if s.Personalities != "reg.example.com/a/my-personalities" {
		t.Errorf("personalities changed unexpectedly: got %q", s.Personalities)
	}
}

func TestSourceConfigUpdate_NotFound(t *testing.T) {
	sc := DefaultSourceConfig()
	err := sc.Update("nonexistent", Source{Registry: "whatever"})
	if err == nil {
		t.Fatal("expected error when updating nonexistent source")
	}
}

func TestSourceConfigUpdate_SaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sources.yaml")

	sc := DefaultSourceConfig()
	_ = sc.Add(Source{Name: "team-a", Registry: "reg.example.com/a"})
	_ = sc.SaveTo(path)

	loaded, _ := LoadSourceConfig(path)
	_ = loaded.Update("team-a", Source{Registry: "new-reg.example.com/a"})
	_ = loaded.Save()

	reloaded, err := LoadSourceConfig(path)
	if err != nil {
		t.Fatalf("LoadSourceConfig() returned error: %v", err)
	}

	s := reloaded.Get("team-a")
	if s == nil {
		t.Fatal("expected team-a source")
	}
	if s.Registry != "new-reg.example.com/a" {
		t.Errorf("registry not persisted: got %q", s.Registry)
	}
}
