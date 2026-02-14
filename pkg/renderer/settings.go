package renderer

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/giantswarm/klausctl/pkg/config"
)

// renderSettings writes the settings.json file containing hooks configuration.
// This mirrors the Helm chart's settings.json rendering.
func (r *Renderer) renderSettings(hooks map[string][]config.HookMatcher) error {
	data := map[string]any{
		"hooks": hooks,
	}

	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}

	path := filepath.Join(r.paths.RenderedDir, "settings.json")
	return writeFile(path, append(content, '\n'), 0o644)
}

// renderHookScripts writes hook script files that are referenced by hooks.
// Scripts are rendered at: <rendered>/hooks/<name>
// They are mounted to /etc/klaus/hooks/<name> in the container.
func (r *Renderer) renderHookScripts(scripts map[string]string) error {
	for name, content := range scripts {
		path := filepath.Join(r.paths.RenderedDir, "hooks", name)
		// Hook scripts need to be executable.
		if err := writeFile(path, []byte(content), 0o755); err != nil {
			return fmt.Errorf("writing hook script %q: %w", name, err)
		}
	}
	return nil
}
