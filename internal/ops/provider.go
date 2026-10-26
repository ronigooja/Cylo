// Package ops contains deployment-facing, secret-safe operational helpers.
package ops

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultProviderSecretPath = "/etc/cylo/runner-secrets/cliproxyapi-api-key"

// ProviderConfig is the deployment configuration consumed by the AO daemon.
// Secret contains the credential only while configuring; it is never rendered
// into an environment file or included in ProbeResult.
type ProviderConfig struct {
	BaseURL    string
	Model      string
	SecretPath string
	Secret     string
}

func (c ProviderConfig) Normalized() (ProviderConfig, error) {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	c.Model = strings.TrimSpace(c.Model)
	c.SecretPath = strings.TrimSpace(c.SecretPath)
	if c.SecretPath == "" {
		c.SecretPath = DefaultProviderSecretPath
	}
	if err := ValidateProviderBaseURL(c.BaseURL); err != nil {
		return ProviderConfig{}, err
	}
	if c.Model == "" {
		return ProviderConfig{}, errors.New("provider model is required")
	}
	if !filepath.IsAbs(c.SecretPath) {
		return ProviderConfig{}, errors.New("provider secret path must be absolute")
	}
	if strings.ContainsAny(c.Model, "\r\n") {
		return ProviderConfig{}, errors.New("provider model contains a newline")
	}
	return c, nil
}

func ValidateProviderBaseURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("provider URL must be an absolute HTTP(S) URL without credentials, query, or fragment")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return errors.New("provider URL must use HTTP or HTTPS")
	}
	return nil
}

// WriteSecretAtomic writes a private credential without exposing it in logs or
// command arguments. Existing credentials are replaced only after the complete
// new file has been synced.
func WriteSecretAtomic(path string, secret []byte) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("secret path must be absolute")
	}
	if len(secret) == 0 {
		return errors.New("provider token is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".provider-secret-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0o600); err == nil {
		_, err = tmp.Write(secret)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDir(dir)
}

// WriteAOEnvAtomic renders only the AO external-provider variables. The token
// itself stays in SecretPath and is never written to this file.
func WriteAOEnvAtomic(path string, c ProviderConfig) error {
	c, err := c.Normalized()
	if err != nil {
		return err
	}
	contents := "AO_CODEX_EXTERNAL_BASE_URL=" + strconv.Quote(c.BaseURL) + "\n" +
		"AO_CODEX_EXTERNAL_API_KEY_FILE=" + strconv.Quote(c.SecretPath) + "\n" +
		"AO_CODEX_EXTERNAL_MODEL=" + strconv.Quote(c.Model) + "\n"
	return writeFileAtomic(path, []byte(contents), 0o600)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("configuration path must be absolute")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cylo-config-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(mode); err == nil {
		_, err = tmp.Write(data)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(tmpName, path); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	err = d.Sync()
	closeErr := d.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// ProbeResult deliberately contains no response body or credential.
type ProbeResult struct {
	HTTPStatus    int
	Model         string
	ModelAvailable bool
	ErrorCategory string
}

// Probe checks the provider's models endpoint and reports coarse outcomes.
func Probe(ctx context.Context, c ProviderConfig, client *http.Client) ProbeResult {
	normalized, err := c.Normalized()
	if err != nil {
		return ProbeResult{Model: c.Model, ErrorCategory: "invalid_configuration"}
	}
	secret, err := os.ReadFile(normalized.SecretPath)
	if err != nil || len(secret) == 0 {
		return ProbeResult{Model: normalized.Model, ErrorCategory: "credential_unavailable"}
	}
	if client == nil {
		client = http.DefaultClient
	}
	endpoint := normalized.BaseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ProbeResult{Model: normalized.Model, ErrorCategory: "request_error"}
	}
	req.Header.Set("Authorization", "Bearer "+string(secret))
	res, err := client.Do(req)
	if err != nil {
		category := "network_error"
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			category = "timeout"
		}
		return ProbeResult{Model: normalized.Model, ErrorCategory: category}
	}
	defer res.Body.Close()
	result := ProbeResult{HTTPStatus: res.StatusCode, Model: normalized.Model}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		result.ErrorCategory = "authentication_failed"
		return result
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		result.ErrorCategory = "provider_http_error"
		return result
	}
	var catalog struct{ Data []struct{ ID string `json:"id"` } `json:"data"` }
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&catalog); err != nil {
		result.ErrorCategory = "invalid_response"
		return result
	}
	for _, model := range catalog.Data {
		if subtle.ConstantTimeCompare([]byte(model.ID), []byte(normalized.Model)) == 1 {
			result.ModelAvailable = true
			return result
		}
	}
	result.ErrorCategory = "model_unavailable"
	return result
}

func (r ProbeResult) OK() bool { return r.HTTPStatus >= 200 && r.HTTPStatus < 300 && r.ModelAvailable && r.ErrorCategory == "" }

// ReadSecret prompts through the supplied reader. Callers should provide a
// terminal implementation that disables echo; this helper never prints the
// entered bytes or stores them globally.
func ReadSecret(prompt io.Writer, read func() ([]byte, error)) ([]byte, error) {
	if prompt != nil {
		_, _ = io.WriteString(prompt, "API token: ")
	}
	secret, err := read()
	if err != nil {
		return nil, err
	}
	secret = []byte(strings.TrimSpace(string(secret)))
	if len(secret) == 0 {
		return nil, fmt.Errorf("provider token is empty")
	}
	return secret, nil
}
