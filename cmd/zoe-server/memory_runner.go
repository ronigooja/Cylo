package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ronigooja/Zoe/internal/control"
)

var memoryWorkerScopes = []string{"memory:jobs:claim", "memory:jobs:complete", "memory:propose"}

type memoryRunnerCredential struct {
	Enabled      bool
	Token        string
	RunnerID     string
	WorkspaceIDs []string
	Scopes       []string
}

func registerMemoryRunnerCredential(ctx context.Context, store interface{ DB() *sql.DB }, credential memoryRunnerCredential) error {
	if !credential.Enabled || store == nil || store.DB() == nil {
		return errors.New("enabled memory runner credential and open store are required")
	}
	if err := control.RegisterRunnerCredentialIfMissing(ctx, store, credential.Token, credential.RunnerID, credential.WorkspaceIDs, credential.Scopes); err != nil {
		return err
	}
	tokenBytes, err := base64.RawURLEncoding.DecodeString(credential.Token)
	if err != nil {
		return errors.New("memory runner credential is invalid")
	}
	hash := sha256.Sum256(tokenBytes)
	var runnerID string
	var workspacesRaw, scopesRaw []byte
	var revoked sql.NullString
	if err := store.DB().QueryRowContext(ctx, `SELECT runner_id,workspace_ids,scopes,revoked_at FROM runner_credentials WHERE token_hash=?`, hash[:]).Scan(&runnerID, &workspacesRaw, &scopesRaw, &revoked); err != nil {
		return err
	}
	var workspaces, scopes []string
	if err := json.Unmarshal(workspacesRaw, &workspaces); err != nil {
		return err
	}
	if err := json.Unmarshal(scopesRaw, &scopes); err != nil {
		return err
	}
	if revoked.Valid || runnerID != credential.RunnerID || !sameStringSet(workspaces, credential.WorkspaceIDs) || !sameStringSet(scopes, credential.Scopes) {
		return errors.New("stored memory runner credential does not match its configured ID, exact workspace allowlist, and exact scopes")
	}
	return nil
}

func loadMemoryRunnerCredentialFromEnv() (memoryRunnerCredential, error) {
	path := strings.TrimSpace(os.Getenv("ZOE_MEMORY_RUNNER_TOKEN_FILE"))
	configured := path != "" || hasEnv("ZOE_MEMORY_RUNNER_ID") || hasEnv("ZOE_MEMORY_RUNNER_WORKSPACES") || hasEnv("ZOE_MEMORY_RUNNER_SCOPES")
	if path == "" {
		if configured {
			return memoryRunnerCredential{}, errors.New("ZOE_MEMORY_RUNNER_TOKEN_FILE is required when memory runner settings are configured")
		}
		return memoryRunnerCredential{}, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return memoryRunnerCredential{}, fmt.Errorf("read memory runner token file: %w", err)
	}
	token := strings.TrimSpace(string(contents))
	if !validBearer(token) {
		return memoryRunnerCredential{}, errors.New("memory runner token file must contain a 32-byte base64url credential")
	}
	if token == strings.TrimSpace(os.Getenv("ZOE_RUNNER_TOKEN")) {
		return memoryRunnerCredential{}, errors.New("memory runner credential must be distinct from ZOE_RUNNER_TOKEN")
	}
	workspaces := optionalSplitEnv("ZOE_MEMORY_RUNNER_WORKSPACES")
	if len(workspaces) == 0 {
		return memoryRunnerCredential{}, errors.New("ZOE_MEMORY_RUNNER_WORKSPACES must list one or more exact workspace IDs")
	}
	if !validExactWorkspaceIDs(workspaces) {
		return memoryRunnerCredential{}, errors.New("ZOE_MEMORY_RUNNER_WORKSPACES must contain unique workspace IDs without wildcards")
	}
	scopes := append([]string(nil), memoryWorkerScopes...)
	if hasEnv("ZOE_MEMORY_RUNNER_SCOPES") {
		scopes = optionalSplitEnv("ZOE_MEMORY_RUNNER_SCOPES")
		if !sameStringSet(scopes, memoryWorkerScopes) {
			return memoryRunnerCredential{}, errors.New("ZOE_MEMORY_RUNNER_SCOPES must be exactly memory:jobs:claim,memory:jobs:complete,memory:propose")
		}
	}
	runnerID := env("ZOE_MEMORY_RUNNER_ID", "memory-worker")
	if runnerID == env("ZOE_RUNNER_ID", "configured-runner") || runnerID == "" || strings.ContainsAny(runnerID, ", \t\r\n") {
		return memoryRunnerCredential{}, errors.New("ZOE_MEMORY_RUNNER_ID must be a distinct non-empty runner ID")
	}
	return memoryRunnerCredential{Enabled: true, Token: token, RunnerID: runnerID, WorkspaceIDs: workspaces, Scopes: scopes}, nil
}

func hasEnv(name string) bool {
	_, ok := os.LookupEnv(name)
	return ok
}

func validExactWorkspaceIDs(ids []string) bool {
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" || id == "*" || strings.ContainsAny(id, ", \t\r\n") {
			return false
		}
		if _, exists := seen[id]; exists {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func sameStringSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := make(map[string]struct{}, len(got))
	for _, value := range got {
		if value == "" {
			return false
		}
		if _, exists := seen[value]; exists {
			return false
		}
		seen[value] = struct{}{}
	}
	for _, value := range want {
		if _, exists := seen[value]; !exists {
			return false
		}
	}
	return true
}
