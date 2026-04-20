package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/giantswarm/klausctl/internal/remotesurface"
	"github.com/giantswarm/klausctl/pkg/agentclient"
	"github.com/giantswarm/klausctl/pkg/config"
	"github.com/giantswarm/klausctl/pkg/remote"
)

// remoteFlagName returns the canonical CLI flag name for a key declared
// in remotesurface.Flags; panics on an unknown key so misspellings fail
// at init time. Used by all subcommands wiring --remote/--session.
func remoteFlagName(key string) string {
	return remotesurface.CLIFlagByKey(key).CLIFlag
}

// remoteFlagDesc returns the description for a remotesurface.Flag key;
// panics on an unknown key.
func remoteFlagDesc(key string) string {
	return remotesurface.CLIFlagByKey(key).Description
}

// tokenRefreshLeeway gives refresh a head start: records expiring in the
// next 60s are refreshed proactively before making a call.
const tokenRefreshLeeway = 60 * time.Second

// resolveRemoteTarget composes a remote.Target for the given URL + instance.
// It loads any cached auth record and, when present, refreshes it if it
// is within the tokenRefreshLeeway. Absence of an auth record is not an
// error — some klaus-gateway deployments run without OAuth.
func resolveRemoteTarget(ctx context.Context, remoteURL, instance, session string, paths *config.Paths) (remote.Target, *remote.AuthStore, *remote.AuthRecord, error) {
	normURL, err := remote.NormalizeBaseURL(remoteURL)
	if err != nil {
		return remote.Target{}, nil, nil, err
	}

	store := remote.NewAuthStore(paths.AuthDir)
	rec, err := store.Get(normURL)
	if err != nil {
		return remote.Target{}, nil, nil, fmt.Errorf("reading auth record for %s: %w", normURL, err)
	}

	var bearer string
	if rec != nil {
		if rec.IsExpired(tokenRefreshLeeway) {
			refreshed, refreshErr := remote.Refresh(ctx, http.DefaultClient, *rec)
			if refreshErr != nil {
				var reloginErr *remote.ErrReloginRequired
				if errors.As(refreshErr, &reloginErr) {
					return remote.Target{}, nil, nil, fmt.Errorf("stored credentials for %s are no longer valid; re-run 'klausctl auth login --remote=%s'", normURL, normURL)
				}
				return remote.Target{}, nil, nil, fmt.Errorf("refreshing token for %s: %w", normURL, refreshErr)
			}
			if err := store.Put(refreshed); err != nil {
				return remote.Target{}, nil, nil, fmt.Errorf("persisting refreshed token: %w", err)
			}
			rec = &refreshed
		}
		bearer = rec.AccessToken
	}

	target, err := remote.NewTarget(normURL, instance, session, bearer)
	if err != nil {
		return remote.Target{}, nil, nil, err
	}
	return target, store, rec, nil
}

// streamRemoteCompletion sends a prompt to a remote gateway, retrying
// exactly once on HTTP 401 by refreshing the cached token. Returns the
// (already-started) delta channel.
func streamRemoteCompletion(ctx context.Context, httpClient *http.Client, target *remote.Target, store *remote.AuthStore, rec *remote.AuthRecord, prompt string) (<-chan agentclient.CompletionDelta, error) {
	req := agentclient.CompletionRequest{
		URL:     target.CompletionsURL(),
		Prompt:  prompt,
		Bearer:  target.BearerToken,
		Headers: target.Headers(),
	}

	ch, err := agentclient.StreamCompletion(ctx, httpClient, req)
	if err == nil {
		return ch, nil
	}

	var httpErr *agentclient.HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusUnauthorized || rec == nil {
		return nil, err
	}

	// One-shot refresh + retry.
	refreshed, refreshErr := remote.Refresh(ctx, http.DefaultClient, *rec)
	if refreshErr != nil {
		var reloginErr *remote.ErrReloginRequired
		if errors.As(refreshErr, &reloginErr) {
			return nil, fmt.Errorf("remote %s rejected credentials; re-run 'klausctl auth login --remote=%s'", target.BaseURL, target.BaseURL)
		}
		return nil, fmt.Errorf("refreshing token after 401: %w", refreshErr)
	}
	if err := store.Put(refreshed); err != nil {
		return nil, fmt.Errorf("persisting refreshed token: %w", err)
	}

	target.BearerToken = refreshed.AccessToken
	req.Bearer = refreshed.AccessToken
	return agentclient.StreamCompletion(ctx, httpClient, req)
}
