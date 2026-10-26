package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	"github.com/ronigooja/Cylo/internal/ops"
)

func init() {
	_ = RegisterOperation("setup", setupOperation)
	_ = RegisterOperation("provider configure", providerConfigureOperation)
	_ = RegisterOperation("provider test", providerTestOperation)
	_ = RegisterOperation("key init", keyInitOperation)
	_ = RegisterOperation("key rotate", keyRotateOperation)
	_ = RegisterOperation("status", statusOperation)
}

func setupOperation(args []string, in io.Reader, out, errOut io.Writer) error {
	return providerConfigureOperation(args, in, out, errOut)
}

func providerConfigureOperation(args []string, in io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("provider configure", flag.ContinueOnError)
	fs.SetOutput(errOut)
	base := fs.String("url", os.Getenv("CYLO_CODEX_EXTERNAL_BASE_URL"), "CLIProxyAPI base URL")
	model := fs.String("model", os.Getenv("CYLO_CODEX_EXTERNAL_MODEL"), "model name")
	secretPath := fs.String("secret-file", ops.DefaultProviderSecretPath, "private token file")
	aoEnv := fs.String("ao-env", "/etc/cylo/ao.env", "AO environment file")
	token := fs.String("token", "", "API token (shell history may retain this value)")
	if err := fs.Parse(args); err != nil { return err }
	reader := bufio.NewReader(in)
	if strings.TrimSpace(*base) == "" {
		fmt.Fprint(out, "CLIProxyAPI URL: ")
		*base = readLine(reader)
	}
	if strings.TrimSpace(*model) == "" {
		fmt.Fprint(out, "Model: ")
		*model = readLine(reader)
	}
	secret := []byte(*token)
	var err error
	if len(secret) == 0 {
		secret, err = readHiddenToken(in, out)
		if err != nil { return err }
	}
	cfg, err := (ops.ProviderConfig{BaseURL: *base, Model: *model, SecretPath: *secretPath, Secret: string(secret)}).Normalized()
	if err != nil { return err }
	oldSecret, oldSecretErr := os.ReadFile(cfg.SecretPath)
	oldEnv, oldEnvErr := os.ReadFile(*aoEnv)
	if err = ops.WriteSecretAtomic(cfg.SecretPath, secret); err != nil { return err }
	if err = ops.WriteAOEnvAtomic(*aoEnv, cfg); err != nil {
		rollbackSecret(cfg.SecretPath, oldSecret, oldSecretErr)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	probe := ops.Probe(ctx, cfg, nil)
	if !probe.OK() {
		rollbackSecret(cfg.SecretPath, oldSecret, oldSecretErr)
		rollbackFile(*aoEnv, oldEnv, oldEnvErr)
		return fmt.Errorf("provider check failed: %s (HTTP %d)", probe.ErrorCategory, probe.HTTPStatus)
	}
	manager := ops.NewManager(nil)
	if err = manager.Reload(ctx, ops.DetectMode(), nil, "", ""); err != nil {
		rollbackSecret(cfg.SecretPath, oldSecret, oldSecretErr)
		rollbackFile(*aoEnv, oldEnv, oldEnvErr)
		return err
	}
	fmt.Fprintln(out, "Provider configured and services reloaded.")
	return nil
}

func providerTestOperation(args []string, in io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("provider test", flag.ContinueOnError)
	fs.SetOutput(errOut)
	base := fs.String("url", os.Getenv("CYLO_CODEX_EXTERNAL_BASE_URL"), "provider URL")
	model := fs.String("model", os.Getenv("CYLO_CODEX_EXTERNAL_MODEL"), "model")
	secretPath := fs.String("secret-file", ops.DefaultProviderSecretPath, "private token file")
	if err := fs.Parse(args); err != nil { return err }
	result := ops.Probe(context.Background(), ops.ProviderConfig{BaseURL: *base, Model: *model, SecretPath: *secretPath}, nil)
	fmt.Fprintf(out, "HTTP status: %d\nModel: %s\nAvailable: %t\nResult: %s\n", result.HTTPStatus, result.Model, result.ModelAvailable, result.ErrorCategory)
	if !result.OK() { return errors.New("provider check failed") }
	return nil
}

func keyInitOperation(args []string, in io.Reader, out, errOut io.Writer) error { return keyOperation(false, args, in, out, errOut) }
func keyRotateOperation(args []string, in io.Reader, out, errOut io.Writer) error { return keyOperation(true, args, in, out, errOut) }

func keyOperation(rotate bool, args []string, in io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("key", flag.ContinueOnError)
	fs.SetOutput(errOut)
	db := fs.String("database", os.Getenv("CYLO_DATABASE_PATH"), "control database path")
	server := fs.String("server", "", "server URL for installation link")
	output := fs.String("output", "", "private JSON export path")
	if err := fs.Parse(args); err != nil { return err }
	if *db == "" { *db = "/var/lib/cylo/server/data/cylo.sqlite" }
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var result ops.KeyResult
	var err error
	options := ops.KeyOptions{DatabasePath: *db, OutputPath: *output, ServerURL: *server}
	if rotate { result, err = ops.RotateKey(ctx, options) } else { result, err = ops.InitKey(ctx, options) }
	if err != nil { return err }
	if err := result.Display(out); err != nil { return err }
	if result.InstallationURL != "" { renderQR(ctx, result.InstallationURL, out) }
	return nil
}

// renderQR keeps the one-time key presentation useful on a terminal while
// retaining a complete installation link fallback for systems without
// qrencode. It is called only by explicit key init/rotate actions.
func renderQR(ctx context.Context, link string, out io.Writer) {
	cmd := exec.CommandContext(ctx, "qrencode", "-t", "ANSIUTF8", "-o", "-", link)
	data, err := cmd.Output()
	if err == nil && len(data) > 0 {
		_, _ = out.Write(data)
		return
	}
	fmt.Fprintln(out, "二维码工具不可用；请手动导入完整安装链接：")
	fmt.Fprintln(out, link)
}

func statusOperation(args []string, _ io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(errOut)
	secret := fs.String("secret-file", ops.DefaultProviderSecretPath, "private token file path")
	urlValue := fs.String("url", os.Getenv("CYLO_CODEX_EXTERNAL_BASE_URL"), "provider URL")
	model := fs.String("model", os.Getenv("CYLO_CODEX_EXTERNAL_MODEL"), "provider model")
	if err := fs.Parse(args); err != nil { return err }
	status := ops.NewManager(nil).Inspect(context.Background(), ops.InspectConfig{SecretPath: *secret, ProviderBaseURL: *urlValue, ProviderModel: *model, ProviderConfigSource: "environment"})
	fmt.Fprintf(out, "Deployment: %s\n", status.Mode)
	for _, service := range status.Services { fmt.Fprintf(out, "%s: %s (%s)\n", service.Name, service.State, service.Source) }
	fmt.Fprintf(out, "Provider: %s model=%s source=%s last-check=%s\n", status.Provider.BaseURL, status.Provider.Model, status.Provider.ConfigSource, status.Provider.LastCheck)
	fmt.Fprintf(out, "Secret: %s exists=%t mode=%s owner=%s\n", status.Secret.Path, status.Secret.Exists, status.Secret.Mode, status.Secret.Owner)
	return nil
}

func readLine(r *bufio.Reader) string { value, _ := r.ReadString('\n'); return strings.TrimSpace(value) }

func readHiddenToken(in io.Reader, out io.Writer) ([]byte, error) {
	if file, ok := in.(*os.File); ok {
		if term, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS); err == nil {
			old := *term
			term.Lflag &^= unix.ECHO
			if err = unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, term); err == nil {
				fmt.Fprint(out, "API token: ")
				value, readErr := bufio.NewReader(file).ReadBytes('\n')
				_ = unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, &old)
				fmt.Fprintln(out)
				return []byte(strings.TrimSpace(string(value))), readErr
			}
		}
	}
	fmt.Fprint(out, "API token (input hidden by terminal): ")
	value, err := bufio.NewReader(in).ReadBytes('\n')
	return []byte(strings.TrimSpace(string(value))), err
}

func rollbackSecret(path string, data []byte, err error) { if err == nil { _ = ops.WriteSecretAtomic(path, data) } else { _ = os.Remove(path) } }
func rollbackFile(path string, data []byte, err error) { if err == nil { _ = os.WriteFile(path, data, 0o600) } else { _ = os.Remove(path) } }

