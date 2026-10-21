package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestServerSubprocessBootstrapAndRestart(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "cylo-server")
	build := exec.Command("/root/environment/go1.26.4/bin/go", "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build server: %v\n%s", err, output)
	}
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()
	db := filepath.Join(t.TempDir(), "state", "cylo.db")
	code, token := subprocessSecret(t), subprocessSecret(t)
	start := func() *exec.Cmd {
		cmd := exec.Command(binary)
		cmd.Env = append(os.Environ(), "CYLO_LISTEN_ADDR="+addr, "CYLO_DATABASE_PATH="+db, "CYLO_BOOTSTRAP_CODE="+code, "CYLO_VOICE_GATEWAY_URL=", "CYLO_VOICE_GATEWAY_API_KEY=")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	client := &http.Client{Timeout: time.Second}
	call := func(method, path, bearer string, body any) (*http.Response, []byte, error) {
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req, err := http.NewRequest(method, "http://"+addr+path, bytes.NewReader(data))
		if err != nil {
			return nil, nil, err
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		res, err := client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer res.Body.Close()
		var buf bytes.Buffer
		_, err = buf.ReadFrom(res.Body)
		return res, buf.Bytes(), err
	}
	callCommand := func(bearer, key string, body any) (*http.Response, []byte, error) {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/v1/commands", bytes.NewReader(data))
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		req.Header.Set("Idempotency-Key", key)
		res, err := client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		defer res.Body.Close()
		var buf bytes.Buffer
		_, err = buf.ReadFrom(res.Body)
		return res, buf.Bytes(), err
	}
	cmd := start()
	t.Cleanup(func() { stopServer(t, cmd) })
	deadline := time.Now().Add(8 * time.Second)
	for {
		res, _, err := call(http.MethodGet, "/healthz", "", nil)
		if err == nil && res.StatusCode == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server did not become ready: %v", err)
		}
		time.Sleep(30 * time.Millisecond)
	}
	regBody := map[string]string{"request_id": "subprocess-bootstrap", "code": code, "device_name": "subprocess-test", "device_token": token}
	res, _, err := call(http.MethodPost, "/v1/bootstrap", "", regBody)
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("bootstrap status=%v err=%v", responseStatus(res), err)
	}
	stopServer(t, cmd)
	recovery := exec.Command(binary, "recover-bootstrap", "--database", db, "--confirm-revoke-all-devices")
	recoveryOutput, err := recovery.Output()
	if err != nil {
		t.Fatalf("offline administrator recovery failed: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(recoveryOutput), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("recovery should print one code only after commit")
	}
	code = string(lines[1])
	newToken := subprocessSecret(t)
	cmd = start()
	t.Cleanup(func() { stopServer(t, cmd) })
	deadline = time.Now().Add(8 * time.Second)
	for {
		res, _, err = call(http.MethodGet, "/healthz", "", nil)
		if err == nil && res.StatusCode == http.StatusOK {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restarted server did not become ready: %v", err)
		}
		time.Sleep(30 * time.Millisecond)
	}
	var body []byte
	res, body, err = call(http.MethodGet, "/v1/snapshot", token, nil)
	if err != nil || res.StatusCode < http.StatusBadRequest {
		t.Fatalf("recovery left old bearer valid: status=%v err=%v", responseStatus(res), err)
	}
	res, body, err = call(http.MethodPost, "/v1/bootstrap", "", map[string]string{"request_id": "recovery-bootstrap", "code": code, "device_name": "recovered", "device_token": newToken})
	if err != nil || res.StatusCode != http.StatusCreated {
		t.Fatalf("recovered bootstrap status=%v err=%v", responseStatus(res), err)
	}
	res, body, err = call(http.MethodGet, "/v1/snapshot", newToken, nil)
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("snapshot after restart status=%v err=%v body=%s", responseStatus(res), err, body)
	}
	res, body, err = callCommand(newToken, "memory-smoke", map[string]any{"kind": "memory.remember", "payload": map[string]any{"scope": map[string]string{"kind": "personal"}, "subject": "server restart", "key": "memory readiness", "content": "memory command is available after restart", "source_kind": "user_note"}})
	if err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("memory write after restart status=%v err=%v body=%s", responseStatus(res), err, body)
	}
	res, body, err = call(http.MethodGet, "/v1/memory?q=available+after+restart", newToken, nil)
	if err != nil || res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("memory command is available after restart")) {
		t.Fatalf("memory search after restart status=%v err=%v body=%s", responseStatus(res), err, body)
	}
	res, body, err = call(http.MethodGet, "/v1/voice/config", newToken, nil)
	if err != nil || res.StatusCode != http.StatusServiceUnavailable || !bytes.Contains(body, []byte("voice_unavailable")) {
		t.Fatalf("voice candidate without gateway should fail closed after restart: status=%v err=%v body=%s", responseStatus(res), err, body)
	}
}

func subprocessSecret(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func responseStatus(r *http.Response) any {
	if r == nil {
		return "no response"
	}
	return fmt.Sprint(r.StatusCode)
}

func stopServer(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd == nil || cmd.Process == nil || cmd.ProcessState != nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Logf("server exit: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		t.Error("server did not stop within grace period")
	}
}
