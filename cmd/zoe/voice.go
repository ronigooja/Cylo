package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	_ = RegisterOperation("voice configure", voiceConfigureOperation)
	_ = RegisterOperation("voice status", voiceStatusOperation)
}

// voiceDeploymentConfig is deliberately limited to non-secret deployment
// inputs. Account cookies, management passwords and downstream API keys stay
// in the gateway's own private files and are never handled by this CLI.
type voiceDeploymentConfig struct {
	Repository string
	Revision   string
	LANBind    string
}

func voiceConfigPath(root string) string { return filepath.Join(root, "voice-deploy.env") }

func voiceConfigureOperation(args []string, _ io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("voice configure", flag.ContinueOnError)
	fs.SetOutput(errOut)
	root := fs.String("root", "/etc/zoe", "deployment configuration root")
	repository := fs.String("repository", "", "chatgpt-web-voice fork URL")
	revision := fs.String("revision", "", "immutable chatgpt-web-voice commit or tag")
	lanBind := fs.String("lan-bind", "", "private IPv4 address for the optional management LAN bind")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	if !filepath.IsAbs(*root) {
		return errors.New("deployment root must be absolute")
	}
	if *repository == "" {
		*repository = "https://github.com/ronigooja/chatgpt-web-voice.git"
	}
	if err := validateVoiceRepository(*repository); err != nil {
		return err
	}
	if *revision == "" {
		*revision = "main"
	}
	if !validVoiceRevision(*revision) {
		return errors.New("revision must contain only letters, digits, '.', '/', '_' or '-'")
	}
	if *lanBind != "" {
		ip := net.ParseIP(*lanBind)
		if ip == nil || ip.To4() == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || !ip.IsPrivate() {
			return errors.New("lan-bind must be a private IPv4 address")
		}
	}
	if err := os.MkdirAll(*root, 0750); err != nil {
		return err
	}
	data := fmt.Sprintf("# Managed by zoe voice configure\nVOICE_GATEWAY_REPOSITORY=%s\nVOICE_GATEWAY_REVISION=%s\nVOICE_LAN_BIND=%s\n", *repository, *revision, *lanBind)
	path := voiceConfigPath(*root)
	tmp, err := os.CreateTemp(*root, ".voice-deploy.env.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err = tmp.Chmod(0600); err == nil {
		_, err = tmp.WriteString(data)
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
	fmt.Fprintf(out, "Voice deployment configuration saved: %s\nRepository: %s\nRevision: %s\nLAN bind: %s\nRun deploy/voice/deploy.sh to apply it.\n", path, *repository, *revision, displayUnset(*lanBind))
	return nil
}

func voiceStatusOperation(args []string, _ io.Reader, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("voice status", flag.ContinueOnError)
	fs.SetOutput(errOut)
	root := fs.String("root", "/etc/zoe", "deployment configuration root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments")
	}
	b, err := os.ReadFile(voiceConfigPath(*root))
	if err != nil {
		if os.IsNotExist(err) {
			return errors.New("voice gateway is not configured; run: zoe voice configure")
		}
		return err
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "VOICE_") {
			k, v, ok := strings.Cut(line, "=")
			if ok {
				values[k] = v
			}
		}
	}
	fmt.Fprintf(out, "Voice config: %s\nRepository: %s\nRevision: %s\nLAN bind: %s\n", voiceConfigPath(*root), values["VOICE_GATEWAY_REPOSITORY"], values["VOICE_GATEWAY_REVISION"], displayUnset(values["VOICE_LAN_BIND"]))
	return nil
}

func validateVoiceRepository(value string) error {
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.ContainsAny(value, "\r\n") {
		return errors.New("repository must be an HTTPS URL")
	}
	return nil
}
func validVoiceRevision(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._/-", r) {
			continue
		}
		return false
	}
	return true
}
func displayUnset(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}
