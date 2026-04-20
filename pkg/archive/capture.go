package archive

import (
	"context"
	"fmt"

	"github.com/giantswarm/klausctl/pkg/instance"
	"github.com/giantswarm/klausctl/pkg/mcpclient"
)

// Capture fetches the full result from a running instance via its MCP
// endpoint and saves it as an archive entry. This is best-effort: the
// caller should log and continue on error.
func Capture(ctx context.Context, client *mcpclient.Client, inst *instance.Instance, archivesDir string) error {
	if inst.UUID == "" {
		return fmt.Errorf("instance %q has no UUID; skipping archive", inst.Name)
	}

	if Exists(archivesDir, inst.UUID) {
		return nil // already archived
	}

	baseURL := fmt.Sprintf("http://localhost:%d/mcp", inst.Port)

	toolResult, err := client.Result(ctx, inst.Name, baseURL, true)
	if err != nil {
		// Fall back: archive with instance metadata only.
		entry, _ := EntryFromResult(inst, "")
		return Save(archivesDir, entry)
	}

	resultJSON := mcpclient.ExtractText(toolResult)

	entry, err := EntryFromResult(inst, resultJSON)
	if err != nil {
		return fmt.Errorf("building archive entry: %w", err)
	}

	return Save(archivesDir, entry)
}
