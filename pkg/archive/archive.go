// Package archive persists agent transcript summaries for stopped/deleted
// instances so they can be reviewed after the container is gone.
package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/instance"
)

// Entry is the archived snapshot of an instance run.
type Entry struct {
	// Instance metadata
	UUID        string    `json:"uuid"`
	Name        string    `json:"name"`
	Image       string    `json:"image"`
	Personality string    `json:"personality,omitempty"`
	Workspace   string    `json:"workspace"`
	Port        int       `json:"port"`
	StartedAt   time.Time `json:"started_at"`
	StoppedAt   time.Time `json:"stopped_at"`

	// Agent result — flat fields from the agent's full result response.
	ResultText    string          `json:"result_text,omitempty"`
	Messages      json.RawMessage `json:"messages,omitempty"`
	MessageCount  int             `json:"message_count"`
	Status        string          `json:"status"`
	SessionID     string          `json:"session_id,omitempty"`
	TotalCostUSD  *float64        `json:"total_cost_usd,omitempty"`
	ToolCalls     map[string]int  `json:"tool_calls,omitempty"`
	ModelUsage    map[string]int  `json:"model_usage,omitempty"`
	TokenUsage    json.RawMessage `json:"token_usage,omitempty"`
	SubagentCalls json.RawMessage `json:"subagent_calls,omitempty"`
	PRURLs        []string        `json:"pr_urls,omitempty"`
	ErrorCount    int             `json:"error_count,omitempty"`
	ErrorMessage  string          `json:"error,omitempty"`
}

// Save writes an archive entry as <uuid>.json in archivesDir.
func Save(archivesDir string, entry *Entry) error {
	if err := config.EnsureDir(archivesDir); err != nil {
		return fmt.Errorf("creating archives directory: %w", err)
	}

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling archive entry: %w", err)
	}

	path := filepath.Join(archivesDir, entry.UUID+".json")
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Load reads a single archive entry by UUID.
func Load(archivesDir, uuid string) (*Entry, error) {
	path := filepath.Join(archivesDir, uuid+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("archive %q not found", uuid)
		}
		return nil, fmt.Errorf("reading archive: %w", err)
	}

	var entry Entry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("parsing archive: %w", err)
	}
	return &entry, nil
}

// LoadAll reads all archive entries, sorted by StoppedAt descending (newest first).
func LoadAll(archivesDir string) ([]*Entry, error) {
	dirEntries, err := os.ReadDir(archivesDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading archives directory: %w", err)
	}

	var entries []*Entry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".json") {
			continue
		}

		data, err := os.ReadFile(filepath.Join(archivesDir, de.Name()))
		if err != nil {
			continue
		}

		var entry Entry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entries = append(entries, &entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].StoppedAt.After(entries[j].StoppedAt)
	})

	return entries, nil
}

// Exists checks whether an archive for the given UUID already exists.
func Exists(archivesDir, uuid string) bool {
	if uuid == "" {
		return false
	}
	path := filepath.Join(archivesDir, uuid+".json")
	_, err := os.Stat(path)
	return err == nil
}

// EntryFromResult builds an Entry from instance metadata and the raw JSON
// response from the agent's full result tool. Fields that don't appear in
// the JSON are left at their zero values.
func EntryFromResult(inst *instance.Instance, resultJSON string) (*Entry, error) {
	entry := &Entry{
		UUID:        inst.UUID,
		Name:        inst.Name,
		Image:       inst.Image,
		Personality: inst.Personality,
		Workspace:   inst.Workspace,
		Port:        inst.Port,
		StartedAt:   inst.StartedAt,
		StoppedAt:   time.Now(),
	}

	if resultJSON == "" {
		entry.Status = "unknown"
		return entry, nil
	}

	// Parse the agent's result JSON to extract fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(resultJSON), &raw); err != nil {
		// Not valid JSON — store the text as result_text.
		entry.Status = "unknown"
		entry.ResultText = resultJSON
		return entry, nil
	}

	decodeString(raw, "status", &entry.Status)
	decodeString(raw, "result_text", &entry.ResultText)
	decodeString(raw, "session_id", &entry.SessionID)
	decodeString(raw, "error", &entry.ErrorMessage)
	decodeInt(raw, "message_count", &entry.MessageCount)
	decodeInt(raw, "error_count", &entry.ErrorCount)
	decodeFloat64Ptr(raw, "total_cost_usd", &entry.TotalCostUSD)
	decodeStringSlice(raw, "pr_urls", &entry.PRURLs)
	decodeStringIntMap(raw, "tool_calls", &entry.ToolCalls)
	decodeStringIntMap(raw, "model_usage", &entry.ModelUsage)

	// Preserve complex fields as raw JSON.
	if v, ok := raw["messages"]; ok {
		entry.Messages = v
	}
	if v, ok := raw["token_usage"]; ok {
		entry.TokenUsage = v
	}
	if v, ok := raw["subagent_calls"]; ok {
		entry.SubagentCalls = v
	}

	if entry.Status == "" {
		entry.Status = "unknown"
	}

	return entry, nil
}

// ListSummary is the shared list-summary type used by both the CLI and MCP
// tool for archive list responses.
type ListSummary struct {
	UUID         string   `json:"uuid"`
	Name         string   `json:"name"`
	Status       string   `json:"status"`
	StoppedAt    string   `json:"stopped_at"`
	MessageCount int      `json:"message_count"`
	TotalCostUSD *float64 `json:"total_cost_usd,omitempty"`
}

// ToListSummary converts an Entry to a ListSummary for list responses.
func (e *Entry) ToListSummary() ListSummary {
	return ListSummary{
		UUID:         e.UUID,
		Name:         e.Name,
		Status:       e.Status,
		StoppedAt:    e.StoppedAt.Format("2006-01-02T15:04:05Z07:00"),
		MessageCount: e.MessageCount,
		TotalCostUSD: e.TotalCostUSD,
	}
}

// --- JSON decode helpers ---

func decodeString(raw map[string]json.RawMessage, key string, dst *string) {
	if v, ok := raw[key]; ok {
		_ = json.Unmarshal(v, dst)
	}
}

func decodeInt(raw map[string]json.RawMessage, key string, dst *int) {
	if v, ok := raw[key]; ok {
		_ = json.Unmarshal(v, dst)
	}
}

func decodeFloat64Ptr(raw map[string]json.RawMessage, key string, dst **float64) {
	if v, ok := raw[key]; ok {
		var f float64
		if json.Unmarshal(v, &f) == nil {
			*dst = &f
		}
	}
}

func decodeStringSlice(raw map[string]json.RawMessage, key string, dst *[]string) {
	if v, ok := raw[key]; ok {
		_ = json.Unmarshal(v, dst)
	}
}

func decodeStringIntMap(raw map[string]json.RawMessage, key string, dst *map[string]int) {
	if v, ok := raw[key]; ok {
		_ = json.Unmarshal(v, dst)
	}
}
