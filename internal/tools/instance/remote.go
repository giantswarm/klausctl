package instance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/giantswarm/klausctl/internal/server"
	"github.com/giantswarm/klausctl/pkg/agentclient"
	"github.com/giantswarm/klausctl/pkg/remote"
)

const tokenRefreshLeeway = 60 * time.Second

// remoteCall bundles the per-call state for a remote klaus-gateway
// request: a composed Target (URL + routing headers + bearer), the
// AuthStore the bearer came from, and the cached AuthRecord used when we
// have to refresh on 401.
type remoteCall struct {
	Target *remote.Target
	Store  *remote.AuthStore
	Record *remote.AuthRecord
}

// remoteFromReq inspects an MCP tool request for `remote` and `session`
// inputs. When `remote` is empty, the returned *remoteCall is nil and
// callers should fall through to the local path. When set, it loads
// (and proactively refreshes) the cached auth record for the host.
func remoteFromReq(ctx context.Context, req mcp.CallToolRequest, sc *server.ServerContext, instance string) (*remoteCall, error) {
	remoteURL := req.GetString("remote", "")
	if remoteURL == "" {
		return nil, nil
	}
	session := req.GetString("session", "")

	normURL, err := remote.NormalizeBaseURL(remoteURL)
	if err != nil {
		return nil, err
	}

	store := remote.NewAuthStore(sc.Paths.AuthDir)
	rec, err := store.Get(normURL)
	if err != nil {
		return nil, fmt.Errorf("reading auth record for %s: %w", normURL, err)
	}

	var bearer string
	if rec != nil {
		if rec.IsExpired(tokenRefreshLeeway) {
			refreshed, refreshErr := remote.Refresh(ctx, http.DefaultClient, *rec)
			if refreshErr != nil {
				var reloginErr *remote.ErrReloginRequired
				if errors.As(refreshErr, &reloginErr) {
					return nil, fmt.Errorf("stored credentials for %s are no longer valid; re-run 'klausctl auth login --remote=%s'", normURL, normURL)
				}
				return nil, fmt.Errorf("refreshing token for %s: %w", normURL, refreshErr)
			}
			if err := store.Put(refreshed); err != nil {
				return nil, fmt.Errorf("persisting refreshed token: %w", err)
			}
			rec = &refreshed
		}
		bearer = rec.AccessToken
	}

	target, err := remote.NewTarget(normURL, instance, session, bearer)
	if err != nil {
		return nil, err
	}
	return &remoteCall{Target: &target, Store: store, Record: rec}, nil
}

// streamRemotePrompt sends a prompt via the remote gateway, retrying
// once on HTTP 401 after refreshing the cached token.
func (c *remoteCall) streamRemotePrompt(ctx context.Context, httpClient *http.Client, prompt string) (<-chan agentclient.CompletionDelta, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	req := agentclient.CompletionRequest{
		URL:     c.Target.CompletionsURL(),
		Prompt:  prompt,
		Bearer:  c.Target.BearerToken,
		Headers: c.Target.Headers(),
	}

	ch, err := agentclient.StreamCompletion(ctx, httpClient, req)
	if err == nil {
		return ch, nil
	}

	var httpErr *agentclient.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusUnauthorized || c.Record == nil {
		return nil, err
	}

	refreshed, refreshErr := remote.Refresh(ctx, http.DefaultClient, *c.Record)
	if refreshErr != nil {
		var reloginErr *remote.ErrReloginRequired
		if errors.As(refreshErr, &reloginErr) {
			return nil, fmt.Errorf("remote %s rejected credentials; re-run 'klausctl auth login --remote=%s'", c.Target.BaseURL, c.Target.BaseURL)
		}
		return nil, fmt.Errorf("refreshing token after 401: %w", refreshErr)
	}
	if err := c.Store.Put(refreshed); err != nil {
		return nil, fmt.Errorf("persisting refreshed token: %w", err)
	}

	c.Target.BearerToken = refreshed.AccessToken
	c.Record = &refreshed
	req.Bearer = refreshed.AccessToken
	return agentclient.StreamCompletion(ctx, httpClient, req)
}

// mcpHeaders returns the MCP-request header bundle: routing headers +
// Authorization bearer (when the target has one).
func (c *remoteCall) mcpHeaders() map[string]string {
	h := c.Target.Headers()
	if c.Target.BearerToken != "" {
		h["Authorization"] = "Bearer " + c.Target.BearerToken
	}
	return h
}
