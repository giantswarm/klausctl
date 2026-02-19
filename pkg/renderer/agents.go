package renderer

import (
	"fmt"
	"path/filepath"
	"sort"

	"github.com/giantswarm/klausctl/pkg/config"
)

// renderAgentFiles writes markdown agent files.
// Agent files are rendered at: <extensions>/.claude/agents/<name>.md
// This mirrors the Helm chart's agentFiles rendering.
func (r *Renderer) renderAgentFiles(agentFiles map[string]config.AgentFile) error {
	// Sort agent names for deterministic output.
	names := make([]string, 0, len(agentFiles))
	for name := range agentFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if err := validateName(name); err != nil {
			return fmt.Errorf("invalid agent file name: %w", err)
		}
		agent := agentFiles[name]
		content := ensureTrailingNewline(agent.Content)

		path := filepath.Join(r.paths.ExtensionsDir, ".claude", "agents", name+".md")
		if err := writeFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing agent file %q: %w", name, err)
		}
	}
	return nil
}
