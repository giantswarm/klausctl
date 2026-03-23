package agentclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// CompletionDelta is a single streaming delta from the completions endpoint.
// When the channel is closed the stream has ended. If the stream was
// interrupted by an I/O error (as opposed to a clean [DONE] or context
// cancellation), Err is set on the final delta before the channel closes.
type CompletionDelta struct {
	Content string
	Err     error
}

// chatCompletionRequest is the POST body for /v1/chat/completions.
type chatCompletionRequest struct {
	Model    string           `json:"model"`
	Messages []chatReqMessage `json:"messages"`
	Stream   bool             `json:"stream"`
}

type chatReqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// StreamCompletion sends a prompt via POST /v1/chat/completions with
// stream=true and returns a channel of deltas. The channel is closed when
// the stream ends (either cleanly via [DONE] or due to an error).
func StreamCompletion(ctx context.Context, client *http.Client, baseURL, prompt string) (<-chan CompletionDelta, error) {
	url := baseURL + "/v1/chat/completions"

	body := chatCompletionRequest{
		Model: "default",
		Messages: []chatReqMessage{
			{Role: "user", Content: prompt},
		},
		Stream: true,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to completions stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		resp.Body.Close()
		return nil, fmt.Errorf("completions returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	ch := make(chan CompletionDelta, 16)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.HasPrefix(line, "data: ") {
				continue
			}
			payload := line[6:]
			if payload == "[DONE]" {
				return
			}

			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				ch <- CompletionDelta{Content: chunk.Choices[0].Delta.Content}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- CompletionDelta{Err: fmt.Errorf("reading completions stream: %w", err)}
		}
	}()

	return ch, nil
}
