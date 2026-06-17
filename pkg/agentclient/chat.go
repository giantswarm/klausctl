package agentclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// finishReasonError is the OpenAI-compatible finish_reason emitted by klaus
// when an agent run terminates in an error state.
const finishReasonError = "error"

// ErrAgentRunFailed is set on the final CompletionDelta when the agent run
// ended in error (finish_reason "error"). Blocking callers return it so the
// process exits non-zero, preventing silent success on a failed run.
var ErrAgentRunFailed = errors.New("agent run ended in error")

// CompletionDelta is a single streaming delta from the completions endpoint.
// When the channel is closed the stream has ended. If the stream was
// interrupted by an I/O error (as opposed to a clean [DONE] or context
// cancellation), Err is set on the final delta before the channel closes.
type CompletionDelta struct {
	Content string
	Err     error
}

// CompletionRequest describes a single /v1/chat/completions call. URL is
// the full endpoint (the caller is responsible for composing the
// instance-scoped path). Bearer and Headers are optional — Bearer becomes
// an `Authorization: Bearer <token>` header and Headers are applied on
// top of the default Content-Type/Accept pair.
type CompletionRequest struct {
	URL     string
	Prompt  string
	Bearer  string
	Headers map[string]string
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

// StreamCompletion sends a prompt via POST to the completions URL with
// stream=true and returns a channel of deltas. The channel is closed when
// the stream ends (either cleanly via [DONE] or due to an error).
//
// The wire body is identical regardless of whether req.Bearer/Headers are
// set — local (direct-to-agent) and remote (gateway) callers share the
// same request shape so the server side sees no difference.
func StreamCompletion(ctx context.Context, client *http.Client, req CompletionRequest) (<-chan CompletionDelta, error) {
	body := chatCompletionRequest{
		Model: "default",
		Messages: []chatReqMessage{
			{Role: "user", Content: req.Prompt},
		},
		Stream: true,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, req.URL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	if req.Bearer != "" {
		httpReq.Header.Set("Authorization", "Bearer "+req.Bearer)
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("connecting to completions stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       strings.TrimSpace(string(errBody)),
		}
	}

	ch := make(chan CompletionDelta, 16)
	go func() {
		defer func() { _ = resp.Body.Close() }()
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
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
				continue
			}
			if len(chunk.Choices) > 0 {
				if content := chunk.Choices[0].Delta.Content; content != "" {
					ch <- CompletionDelta{Content: content}
				}
				// A terminal finish_reason of "error" means the agent run
				// failed (e.g. the model was unavailable and the underlying
				// subprocess exited non-zero). Surface it as a delta error so
				// blocking callers exit non-zero instead of reporting success
				// for a run that produced nothing.
				if fr := chunk.Choices[0].FinishReason; fr != nil && *fr == finishReasonError {
					ch <- CompletionDelta{Err: ErrAgentRunFailed}
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			ch <- CompletionDelta{Err: fmt.Errorf("reading completions stream: %w", err)}
		}
	}()

	return ch, nil
}

// HTTPError is returned by StreamCompletion when the completions endpoint
// responds with a non-200 status. Callers (notably the remote path) can
// type-assert on this to trigger refresh-on-401.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("completions returned status %d: %s", e.StatusCode, e.Body)
}
