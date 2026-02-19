package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// execRuntime implements the Runtime interface using os/exec to call
// docker or podman CLI commands. Both CLIs share compatible interfaces.
type execRuntime struct {
	binary string
}

func (r *execRuntime) Name() string {
	return r.binary
}

func (r *execRuntime) Run(ctx context.Context, opts RunOptions) (string, error) {
	args := []string{"run"}

	if opts.Detach {
		args = append(args, "-d")
	}

	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}

	if opts.User != "" {
		args = append(args, "--user", opts.User)
	}

	// Environment variables (sorted for deterministic output).
	envKeys := make([]string, 0, len(opts.EnvVars))
	for k := range opts.EnvVars {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, opts.EnvVars[k]))
	}

	// Port mappings (sorted for deterministic output).
	portKeys := make([]int, 0, len(opts.Ports))
	for k := range opts.Ports {
		portKeys = append(portKeys, k)
	}
	sort.Ints(portKeys)
	for _, hostPort := range portKeys {
		containerPort := opts.Ports[hostPort]
		args = append(args, "-p", fmt.Sprintf("%d:%d", hostPort, containerPort))
	}

	// Volume mounts.
	for _, v := range opts.Volumes {
		mount := fmt.Sprintf("%s:%s", v.HostPath, v.ContainerPath)
		if v.ReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}

	args = append(args, opts.Image)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s run failed: %s\n%s", r.binary, err, stderr.String())
	}

	return strings.TrimSpace(stdout.String()), nil
}

func (r *execRuntime) Stop(ctx context.Context, name string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.binary, "stop", name)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s stop failed: %s\n%s", r.binary, err, stderr.String())
	}
	return nil
}

func (r *execRuntime) Remove(ctx context.Context, name string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.binary, "rm", "-f", name)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s rm failed: %s\n%s", r.binary, err, stderr.String())
	}
	return nil
}

func (r *execRuntime) Status(ctx context.Context, name string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.binary, "inspect", "--format", "{{.State.Status}}", name)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Docker prints "No such object", Podman prints "no such container".
		if strings.Contains(strings.ToLower(stderr.String()), "no such") {
			return "", nil
		}
		return "", fmt.Errorf("%s inspect failed: %w\n%s", r.binary, err, stderr.String())
	}
	return strings.TrimSpace(stdout.String()), nil
}

func (r *execRuntime) Inspect(ctx context.Context, name string) (*ContainerInfo, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.binary, "inspect", name)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s inspect failed: %s\n%s", r.binary, err, stderr.String())
	}

	var results []inspectResult
	if err := json.Unmarshal(stdout.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("parsing inspect output: %w", err)
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no container found with name %q", name)
	}

	result := results[0]
	return &ContainerInfo{
		ID:        result.ID,
		Name:      strings.TrimPrefix(result.Name, "/"),
		Image:     result.Image,
		Status:    result.State.Status,
		StartedAt: result.State.StartedAt,
	}, nil
}

func (r *execRuntime) Images(ctx context.Context, filter string) ([]ImageInfo, error) {
	args := []string{"images"}
	if filter != "" {
		args = append(args, "--filter", "reference="+filter)
	}
	args = append(args, "--format", "{{json .}}")

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s images failed: %s\n%s", r.binary, err, stderr.String())
	}

	output := strings.TrimSpace(stdout.String())
	if output == "" {
		return nil, nil
	}

	var images []ImageInfo
	for _, line := range strings.Split(output, "\n") {
		if line == "" {
			continue
		}
		var raw struct {
			Repository   string `json:"Repository"`
			Tag          string `json:"Tag"`
			ID           string `json:"ID"`
			CreatedSince string `json:"CreatedSince"`
			Size         string `json:"Size"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		// Skip untagged images.
		if raw.Tag == "<none>" || raw.Repository == "<none>" {
			continue
		}
		images = append(images, ImageInfo{
			Repository:   raw.Repository,
			Tag:          raw.Tag,
			ID:           raw.ID,
			CreatedSince: raw.CreatedSince,
			Size:         raw.Size,
		})
	}

	return images, nil
}

func (r *execRuntime) Pull(ctx context.Context, image string, w io.Writer) error {
	cmd := exec.CommandContext(ctx, r.binary, "pull", image)
	cmd.Stdout = w
	cmd.Stderr = w

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s pull failed: %w", r.binary, err)
	}
	return nil
}

func (r *execRuntime) Logs(ctx context.Context, name string, follow bool, tail int) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}
	args = append(args, name)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	// Swallow context-cancellation errors -- the user interrupted with Ctrl+C,
	// which is the normal way to stop "logs -f".
	if ctx.Err() != nil {
		return nil
	}
	return err
}

func (r *execRuntime) LogsCapture(ctx context.Context, name string, tail int) (string, error) {
	args := []string{"logs"}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}
	args = append(args, name)

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, r.binary, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s logs failed: %s\n%s", r.binary, err, stderr.String())
	}
	return stdout.String(), nil
}
