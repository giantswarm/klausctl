//go:build integration

package oci

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests require a local OCI registry running at localhost:5099.
// Start one with: docker run -d -p 5099:5000 --name klausctl-test-registry registry:2

const testRegistry = "localhost:5099"

func testRef(name, tag string) string {
	return fmt.Sprintf("%s/klausctl-test/%s:%s", testRegistry, name, tag)
}

func setupPluginDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	writeTestFile(t, filepath.Join(dir, "skills", "k8s", "SKILL.md"), "# Kubernetes Skill\n\nThis skill helps with Kubernetes operations.")
	writeTestFile(t, filepath.Join(dir, "agents", "helper.md"), "# Helper Agent\n\nProvides platform assistance.")
	writeTestFile(t, filepath.Join(dir, "hooks", "hooks.json"), `{"PreToolUse":[{"matcher":"Bash","approval":"auto-approve"}]}`)
	writeTestFile(t, filepath.Join(dir, ".mcp.json"), `{"mcpServers":{"test":{"command":"echo","args":["hello"]}}}`)

	return dir
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIntegrationPushAndPull(t *testing.T) {
	ctx := context.Background()
	client := NewClient(WithPlainHTTP(true))

	srcDir := setupPluginDir(t)
	ref := testRef("gs-platform", "v1.0.0")

	meta := PluginMeta{
		Name:        "gs-platform",
		Version:     "1.0.0",
		Description: "Giant Swarm platform plugin",
		Skills:      []string{"k8s"},
	}

	configJSON, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshaling metadata: %v", err)
	}

	// --- Push ---
	t.Log("Pushing plugin to registry...")
	pushResult, err := client.Push(ctx, srcDir, ref, configJSON, PluginArtifact)
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if pushResult.Digest == "" {
		t.Fatal("Push() returned empty digest")
	}
	t.Logf("Pushed: digest=%s", pushResult.Digest)

	// --- Pull into a fresh directory ---
	destDir := t.TempDir()
	t.Log("Pulling plugin from registry...")
	pullResult, err := client.Pull(ctx, ref, destDir, PluginArtifact)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if pullResult.Cached {
		t.Error("First pull should not be cached")
	}
	if pullResult.Digest == "" {
		t.Fatal("Pull() returned empty digest")
	}
	t.Logf("Pulled: digest=%s, cached=%v", pullResult.Digest, pullResult.Cached)

	// --- Verify extracted content matches source ---
	assertFileExists(t, filepath.Join(destDir, "skills", "k8s", "SKILL.md"))
	assertFileContains(t, filepath.Join(destDir, "skills", "k8s", "SKILL.md"), "Kubernetes Skill")
	assertFileExists(t, filepath.Join(destDir, "agents", "helper.md"))
	assertFileContains(t, filepath.Join(destDir, "agents", "helper.md"), "Helper Agent")
	assertFileExists(t, filepath.Join(destDir, "hooks", "hooks.json"))
	assertFileContains(t, filepath.Join(destDir, "hooks", "hooks.json"), "PreToolUse")
	assertFileExists(t, filepath.Join(destDir, ".mcp.json"))
	assertFileContains(t, filepath.Join(destDir, ".mcp.json"), "mcpServers")

	// --- Verify cache metadata was written ---
	cacheEntry, err := ReadCacheEntry(destDir)
	if err != nil {
		t.Fatalf("ReadCacheEntry() error = %v", err)
	}
	if cacheEntry.Digest != pullResult.Digest {
		t.Errorf("cache digest = %q, want %q", cacheEntry.Digest, pullResult.Digest)
	}
	if cacheEntry.PulledAt.IsZero() {
		t.Error("cache PulledAt should not be zero")
	}
	t.Logf("Cache entry: digest=%s, ref=%s, pulledAt=%s", cacheEntry.Digest, cacheEntry.Ref, cacheEntry.PulledAt)
}

func TestIntegrationCacheHit(t *testing.T) {
	ctx := context.Background()
	client := NewClient(WithPlainHTTP(true))

	srcDir := setupPluginDir(t)
	ref := testRef("cache-test", "v1.0.0")

	meta := PluginMeta{Name: "cache-test", Version: "1.0.0"}
	configJSON, _ := json.Marshal(meta)

	if _, err := client.Push(ctx, srcDir, ref, configJSON, PluginArtifact); err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	destDir := t.TempDir()
	first, err := client.Pull(ctx, ref, destDir, PluginArtifact)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if first.Cached {
		t.Error("First pull should not be cached")
	}
	t.Logf("First pull: digest=%s, cached=%v", first.Digest, first.Cached)

	second, err := client.Pull(ctx, ref, destDir, PluginArtifact)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if !second.Cached {
		t.Error("Second pull should be cached")
	}
	if second.Digest != first.Digest {
		t.Errorf("Digest mismatch: first=%q, second=%q", first.Digest, second.Digest)
	}
	t.Logf("Second pull: digest=%s, cached=%v", second.Digest, second.Cached)
}

func TestIntegrationResolve(t *testing.T) {
	ctx := context.Background()
	client := NewClient(WithPlainHTTP(true))

	srcDir := setupPluginDir(t)
	ref := testRef("resolve-test", "v2.0.0")

	meta := PluginMeta{Name: "resolve-test", Version: "2.0.0"}
	configJSON, _ := json.Marshal(meta)

	pushResult, err := client.Push(ctx, srcDir, ref, configJSON, PluginArtifact)
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}

	digest, err := client.Resolve(ctx, ref)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if digest != pushResult.Digest {
		t.Errorf("Resolve() = %q, want %q", digest, pushResult.Digest)
	}
	t.Logf("Resolved %s -> %s", ref, digest)

	bareRepo := fmt.Sprintf("%s/klausctl-test/resolve-test", testRegistry)
	_, err = client.Resolve(ctx, bareRepo)
	if err == nil {
		t.Error("Resolve() with bare repo should fail")
	}
	t.Logf("Resolve bare repo correctly failed: %v", err)
}

func TestIntegrationList(t *testing.T) {
	ctx := context.Background()
	client := NewClient(WithPlainHTTP(true))

	srcDir := setupPluginDir(t)
	repoName := "klausctl-test/list-test"
	fullRepo := fmt.Sprintf("%s/%s", testRegistry, repoName)

	meta := PluginMeta{Name: "list-test", Version: "1.0.0"}
	configJSON, _ := json.Marshal(meta)

	if _, err := client.Push(ctx, srcDir, fullRepo+":v1.0.0", configJSON, PluginArtifact); err != nil {
		t.Fatalf("Push v1.0.0 error = %v", err)
	}
	meta.Version = "2.0.0"
	configJSON, _ = json.Marshal(meta)
	if _, err := client.Push(ctx, srcDir, fullRepo+":v2.0.0", configJSON, PluginArtifact); err != nil {
		t.Fatalf("Push v2.0.0 error = %v", err)
	}

	tags, err := client.List(ctx, fullRepo)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	t.Logf("Tags: %v", tags)

	if len(tags) < 2 {
		t.Fatalf("List() returned %d tags, want >= 2", len(tags))
	}

	found := map[string]bool{}
	for _, tag := range tags {
		found[tag] = true
	}
	if !found["v1.0.0"] {
		t.Error("List() missing tag v1.0.0")
	}
	if !found["v2.0.0"] {
		t.Error("List() missing tag v2.0.0")
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected file to exist: %s", path)
	}
}

func assertFileContains(t *testing.T, path, substr string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !strings.Contains(string(data), substr) {
		t.Errorf("file %s does not contain %q, got: %q", path, substr, string(data))
	}
}
