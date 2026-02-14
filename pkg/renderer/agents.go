package renderer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/giantswarm/klausctl/pkg/config"
)

// renderAgentFiles writes markdown agent files.
// Agent files are rendered at: <extensions>/.claude/agents/<name>.md
// This mirrors the Helm chart's agentFiles rendering.
func (r *Renderer) renderAgentFiles(agentFiles map[string]config.AgentFile) error {
	for name, agent := range agentFiles {
		content := agent.Content
		// Ensure trailing newline.
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}

		path := filepath.Join(r.paths.ExtensionsDir, ".claude", "agents", name+".md")
		if err := writeFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing agent file %q: %w", name, err)
		}
	}
	return nil
}
