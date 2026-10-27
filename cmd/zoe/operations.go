package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ronigooja/Zoe/internal/ops"
	"github.com/skip2/go-qrcode"
	"golang.org/x/sys/unix"
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
	fmt.Fprintln(out, "Setup checks the installed deployment and configures its provider.")
	return providerConfigureOperation(args, in, out, errOut)
}

// providerFlags are shared by setup, configure, test and status so the same
// deployment is used by interactive and scripted operations.
type providerFlags struct {
	root, base, model, secret, token *string
	aoEnv, aoService                 *string
}

func addProviderFlags(fs *flag.FlagSet, token bool) providerFlags {
	f := providerFlags{root: fs.String("root", "", "deployment configuration root (default /etc/zoe)"), base: fs.String("url", "", "override provider URL"), model: fs.String("model", "", "override model"), secret: fs.String("secret-file", "", "override private token file"), aoEnv: fs.String("ao-env", "", "override AO environment file")}
	f.aoService = fs.String("ao-service", "", "AO service name when discovery cannot determine it")
	if token {
		f.token = fs.String("token", "", "compatibility token input; shell history and process arguments may retain it (prefer prompt)")
	}
	return f
}

var opsManager = func() ops.Manager { return ops.NewManager(nil) }

func providerDeployment(ctx context.Context, f providerFlags) (ops.Deployment, ops.ProviderConfig, error) {
	d, err := ops.DiscoverDeployment(ctx, opsManager(), *f.root)
	if err != nil {
		return d, ops.ProviderConfig{}, err
	}
	if *f.aoEnv != "" {
		d.AOEnv = *f.aoEnv
	}
	if *f.aoService != "" {
		d.AOService = *f.aoService
	}
	cfg, err := d.ProviderConfig()
	if err != nil {
		return d, cfg, err
	}
	if *f.base != "" {
		cfg.BaseURL = *f.base
	}
	if *f.model != "" {
		cfg.Model = *f.model
	}
	if *f.secret != "" {
		cfg.SecretPath = *f.secret
	}
	return d, cfg, nil
}
func providerConfigureOperation(args []string, in io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("provider configure", flag.ContinueOnError)
	fs.SetOutput(errOut)
	f := addProviderFlags(fs, true)
	nonInteractive := fs.Bool("non-interactive", false, "require all provider values without prompts")
	tokenFile := fs.String("token-file", "", "read a private token file (preferred for scripts)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	d, cfg, err := providerDeployment(ctx, f)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Deployment: %s\nDatabase: %s\nAO config: %s\nAO service: %s\n", d.Mode, d.DatabasePath, d.AOEnv, d.AOService)
	in = sharedInput(in)
	prompt := func(label string, value *string) error {
		if strings.TrimSpace(*value) != "" {
			return nil
		}
		if *nonInteractive {
			return fmt.Errorf("%s is required", label)
		}
		fmt.Fprintf(out, "%s: ", label)
		v, err := inputLine(in)
		if err != nil {
			return err
		}
		*value = v
		return nil
	}
	if err := prompt("CLIProxyAPI URL", &cfg.BaseURL); err != nil {
		return err
	}
	if err := prompt("Model", &cfg.Model); err != nil {
		return err
	}
	if *f.token != "" && *tokenFile != "" {
		return errors.New("use only one of --token and --token-file")
	}
	secret := []byte(*f.token)
	if *tokenFile != "" {
		secret, err = os.ReadFile(*tokenFile)
		if err != nil {
			return errors.New("cannot read token file")
		}
	}
	if len(secret) == 0 {
		if *nonInteractive {
			return errors.New("--token-file or --token is required for non-interactive configure")
		}
		secret, err = readHiddenToken(in, out)
		if err != nil {
			return err
		}
	}
	cfg.Secret = strings.TrimSpace(string(secret))
	if _, err := opsManager().ConfigureProvider(ctx, d, cfg, nil); err != nil {
		return err
	}
	fmt.Fprintln(out, "Provider configured, checked and services reloaded.")
	return nil
}
func providerTestOperation(args []string, _ io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("provider test", flag.ContinueOnError)
	fs.SetOutput(errOut)
	f := addProviderFlags(fs, false)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	d, cfg, err := providerDeployment(ctx, f)
	if err != nil {
		return err
	}
	result, err := opsManager().TestProvider(ctx, d, cfg, nil)
	fmt.Fprintf(out, "HTTP status: %d\nModel available: %t\nResult: %s\n", result.HTTPStatus, result.ModelAvailable, safeLabel(result.ErrorCategory))
	return err
}

func keyInitOperation(args []string, in io.Reader, out, errOut io.Writer) error {
	return keyOperation(false, args, in, out, errOut)
}
func keyRotateOperation(args []string, in io.Reader, out, errOut io.Writer) error {
	return keyOperation(true, args, in, out, errOut)
}

// secretTerminal is separate from stdout so shell redirection never captures
// a newly generated credential or QR. Tests can replace this boundary.
var secretTerminal = func() (io.WriteCloser, error) { return os.OpenFile("/dev/tty", os.O_WRONLY, 0) }

func keyOperation(rotate bool, args []string, in io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("key", flag.ContinueOnError)
	fs.SetOutput(errOut)
	root := fs.String("root", "/etc/zoe", "deployment configuration root")
	db := fs.String("database", os.Getenv("ZOE_DATABASE_PATH"), "control database path")
	server := fs.String("server", os.Getenv("ZOE_SERVER_URL"), "required server URL for app connection")
	output := fs.String("output", "", "private 0600 JSON export (default beside database)")
	nonInteractive := fs.Bool("non-interactive", false, "require server URL without a prompt")
	confirm := fs.Bool("yes", false, "confirm immediate invalidation of the old key on rotation")
	offline := fs.Bool("offline", false, "skip managed service pause; require exclusive database lock")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if *db == "" {
		d, err := ops.DiscoverDeployment(context.Background(), opsManager(), *root)
		if err != nil {
			return err
		}
		*db = d.DatabasePath
	}
	in = sharedInput(in)
	if strings.TrimSpace(*server) == "" {
		if *nonInteractive {
			return errors.New("--server is required")
		}
		fmt.Fprint(out, "Zoe server URL: ")
		value, err := inputLine(in)
		if err != nil {
			return err
		}
		*server = value
	}
	if err := ops.ValidateServerURL(*server); err != nil {
		return err
	}
	if rotate {
		fmt.Fprintln(out, "Rotating immediately invalidates the old instance key on every connected app.")
		if !*confirm {
			if *nonInteractive {
				return errors.New("rotation requires --yes")
			}
			fmt.Fprint(out, "Rotate key now? [y/N]: ")
			answer, err := inputLine(in)
			if err != nil {
				return err
			}
			if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
				return errors.New("rotation cancelled")
			}
		}
	}
	if err := checkDeploymentKeySources(*root); err != nil {
		return err
	}
	if *output == "" {
		*output = filepath.Join(filepath.Dir(*db), "instance-key-"+time.Now().UTC().Format("20060102T150405.000000000")+".json")
	}
	fmt.Fprintln(out, "Checking exclusive database ownership.")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Serialize key maintenance with provider configuration and other operator
	// actions. The database lock protects SQLite state; this deployment lock
	// also prevents a concurrent service reconfiguration from racing the stop
	// and restart window below.
	releaseDeployment, err := ops.LockDeployment(ctx, ops.Deployment{Root: *root})
	if err != nil {
		return fmt.Errorf("acquire deployment operation lock: %w", err)
	}
	defer releaseDeployment()
	resume := func() error { return nil }
	if !*offline {
		var pauseErr error
		resume, pauseErr = pauseControl(ctx, *root)
		if pauseErr != nil {
			return pauseErr
		}
	}
	resumed := false
	defer func() {
		if !resumed {
			if err := resume(); err != nil {
				fmt.Fprintln(errOut, "Service recovery failed; restart the control service manually.")
			}
		}
	}()
	options := ops.KeyOptions{DatabasePath: *db, OutputPath: *output, ServerURL: *server}
	var result ops.KeyResult
	if rotate {
		result, err = ops.RotateKey(ctx, options)
	} else {
		result, err = ops.InitKey(ctx, options)
	}
	if err != nil {
		return err
	}
	if err := resume(); err != nil {
		return fmt.Errorf("key committed to private export %s; service restart failed: %w", *output, err)
	}
	resumed = true
	fmt.Fprintf(out, "Private key export: %s (0600)\n", *output)
	terminal, err := secretTerminal()
	if err != nil {
		fmt.Fprintln(out, "No controlling terminal; connection details are saved only in the private export.")
		return nil
	}
	defer terminal.Close()
	fmt.Fprintf(terminal, "Server URL: %s\nInstance key and app connection link (shown once):\n", *server)
	if err := result.Display(terminal); err != nil {
		return err
	}
	return renderQR(ctx, result.InstallationURL, terminal)
}

// pauseControl stops only a currently running managed control service and
// returns a recovery action. Lock acquisition in ops is still authoritative.
func pauseControl(ctx context.Context, root string) (func() error, error) {
	m := opsManager()
	d, err := ops.DiscoverDeployment(ctx, m, root)
	if err != nil {
		return nil, fmt.Errorf("discover control service (use --offline only after stopping it manually): %w", err)
	}
	recovery := func(name string, args ...string) func() error {
		return func() error {
			recoveryCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_, err := m.Run(recoveryCtx, name, args...)
			return err
		}
	}
	if d.Mode == ops.ModeSystemd {
		state, err := m.Run(ctx, "systemctl", "is-active", "zoe-compose.service")
		if strings.TrimSpace(string(state)) == "inactive" {
			return func() error { return nil }, nil
		}
		if err != nil || strings.TrimSpace(string(state)) != "active" {
			return nil, errors.New("cannot establish control service state; stop it manually and use --offline")
		}
		if _, err := m.Run(ctx, "systemctl", "stop", "zoe-compose.service"); err != nil {
			return nil, errors.New("cannot stop control service")
		}
		return recovery("systemctl", "start", "zoe-compose.service"), nil
	}
	if d.Mode != ops.ModeCompose || d.ComposeFile == "" {
		return nil, errors.New("unknown control deployment; stop it manually and use --offline")
	}
	base := []string{"compose", "--env-file", d.StackEnv, "--file", d.ComposeFile}
	if d.ComposeProject != "" {
		base = append(base, "--project-name", d.ComposeProject)
	}
	running, err := m.Run(ctx, "docker", append(append([]string{}, base...), "ps", "--status", "running", "--services")...)
	if err != nil {
		return nil, errors.New("cannot inspect control container")
	}
	active := false
	for _, service := range strings.Fields(string(running)) {
		if service == "zoe-server" {
			active = true
		}
	}
	if !active {
		return func() error { return nil }, nil
	}
	if _, err := m.Run(ctx, "docker", append(append([]string{}, base...), "stop", "zoe-server")...); err != nil {
		return nil, errors.New("cannot stop control container")
	}
	return recovery("docker", append(base, "start", "zoe-server")...), nil
}

// Startup credentials supplied by the deployment can overwrite or reject a
// rotated database key. Refuse mutation rather than leave a stale deployment.
func checkDeploymentKeySources(root string) error {
	for _, name := range []string{"ZOE_INSTANCE_KEY", "ZOE_INSTANCE_KEY_FILE"} {
		if os.Getenv(name) != "" {
			return fmt.Errorf("%s is set; remove the startup credential source before offline key maintenance, then configure the new private key before restarting", name)
		}
	}
	for _, file := range []string{"zoe-stack.env", "zoe-server.env", "zoe-runner.env", "ao.env", ".env"} {
		data, err := os.ReadFile(filepath.Join(root, file))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot inspect deployment credential sources: %w", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "export "))
			name, value, ok := strings.Cut(line, "=")
			if ok && (strings.TrimSpace(name) == "ZOE_INSTANCE_KEY" || strings.TrimSpace(name) == "ZOE_INSTANCE_KEY_FILE") && strings.Trim(strings.TrimSpace(value), "\"'") != "" {
				return fmt.Errorf("%s configures a startup instance credential; remove or migrate that source before offline key maintenance to avoid stale credentials on restart", file)
			}
		}
	}
	return nil
}

// renderQR is self-contained; sensitive links never become process arguments.
func renderQR(_ context.Context, link string, out io.Writer) error {
	code, err := qrcode.New(link, qrcode.Medium)
	if err != nil {
		return fmt.Errorf("encode installation QR: %w", err)
	}
	matrix := code.Bitmap()
	for y := 0; y < len(matrix); y += 2 {
		if _, err := io.WriteString(out, "\x1b[30;47m"); err != nil {
			return err
		}
		for x := range matrix[y] {
			top := matrix[y][x]
			bottom := y+1 < len(matrix) && matrix[y+1][x]
			glyph := " "
			switch {
			case top && bottom:
				glyph = "█"
			case top:
				glyph = "▀"
			case bottom:
				glyph = "▄"
			}
			if _, err := io.WriteString(out, glyph); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(out, "\x1b[0m\n"); err != nil {
			return err
		}
	}
	return nil
}

func statusOperation(args []string, _ io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(errOut)
	f := addProviderFlags(fs, false)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	d, cfg, err := providerDeployment(ctx, f)
	if err != nil {
		return err
	}
	inspect := d.InspectConfig()
	inspect.SecretPath = cfg.SecretPath
	inspect.ProviderBaseURL = cfg.BaseURL
	inspect.ProviderModel = cfg.Model
	status := opsManager().Inspect(ctx, inspect)
	fmt.Fprintf(out, "Deployment: %s\n", status.Mode)
	for _, service := range status.Services {
		fmt.Fprintf(out, "%s: %s (%s)\n", safeLabel(service.Name), safeLabel(service.State), safeLabel(service.Source))
	}
	fmt.Fprintf(out, "Provider: %s model=%s source=%s last-check=%s\n", safeLabel(status.Provider.BaseURL), safeLabel(status.Provider.Model), safeLabel(status.Provider.ConfigSource), safeLabel(ops.LastProviderCheck(d)))
	fmt.Fprintf(out, "Secret: %s exists=%t mode=%s owner=%s\n", safeLabel(status.Secret.Path), status.Secret.Exists, safeLabel(status.Secret.Mode), safeLabel(status.Secret.Owner))
	return nil
}

func safeLabel(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 80 {
		value = value[:80]
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return "invalid"
		}
	}
	return value
}

func readHiddenToken(in io.Reader, out io.Writer) ([]byte, error) {
	in = sharedInput(in)
	hidden := false
	if file := terminalFile(in); file != nil {
		if term, err := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS); err == nil {
			old := *term
			term.Lflag &^= unix.ECHO
			if err := unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, term); err != nil {
				return nil, errors.New("cannot disable terminal echo")
			}
			defer unix.IoctlSetTermios(int(file.Fd()), unix.TCSETS, &old)
			hidden = true
		}
	}
	if hidden {
		fmt.Fprint(out, "API token (hidden): ")
	} else {
		fmt.Fprint(out, "API token (non-terminal input): ")
	}
	value, err := inputLine(in)
	if hidden {
		fmt.Fprintln(out)
	}
	if err != nil {
		return nil, err
	}
	if value == "" {
		return nil, errors.New("API token is required")
	}
	return []byte(value), nil
}
