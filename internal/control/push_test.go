package control

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAPNSSenderSignsJWTAndSendsSilentWakeup(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	var got bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = true
		if r.URL.String() != "https://api.sandbox.push.apple.com/3/device/"+strings.Repeat("a", 64) {
			t.Errorf("unexpected APNs URL: %s", r.URL)
		}
		for name, want := range map[string]string{"apns-topic": "com.example.zoe", "apns-push-type": "background", "apns-priority": "5", "apns-id": "apns-test-id", "apns-collapse-id": "zoe-test"} {
			if r.Header.Get(name) != want {
				t.Errorf("%s=%q, want %q", name, r.Header.Get(name), want)
			}
		}
		parts := strings.Split(strings.TrimPrefix(r.Header.Get("authorization"), "bearer "), ".")
		if len(parts) != 3 {
			t.Fatalf("invalid bearer JWT")
		}
		var header, claims map[string]any
		hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
		cb, _ := base64.RawURLEncoding.DecodeString(parts[1])
		if json.Unmarshal(hb, &header) != nil || json.Unmarshal(cb, &claims) != nil {
			t.Fatalf("invalid JWT JSON")
		}
		if header["alg"] != "ES256" || header["kid"] != "key-id" || claims["iss"] != "team-id" {
			t.Fatalf("unexpected JWT header/claims: %#v %#v", header, claims)
		}
		sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if len(sig) != 64 || !ecdsa.Verify(&key.PublicKey, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			t.Fatalf("JWT signature is invalid")
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if json.Unmarshal(body, &payload) != nil {
			t.Fatalf("invalid APNs payload: %s", body)
		}
		aps := payload["aps"].(map[string]any)
		if aps["content-available"] != float64(1) || len(aps) != 1 {
			t.Fatalf("payload must be a silent wakeup: %#v", payload)
		}
		if payload["zoe"].(map[string]any)["inbox_cursor"] != "42" {
			t.Fatalf("inbox cursor missing: %#v", payload)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Apns-Id": []string{"apns-test-id"}}, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	sender, err := NewAPNSSender(APNSConfig{Environment: "sandbox", TeamID: "team-id", KeyID: "key-id", Topic: "com.example.zoe", PrivateKey: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), HTTPClient: client})
	if err != nil {
		t.Fatal(err)
	}
	issuedAt := time.Now()
	firstJWT, err := sender.jwt(issuedAt)
	if err != nil {
		t.Fatal(err)
	}
	reusedJWT, err := sender.jwt(issuedAt.Add(49 * time.Minute))
	if err != nil || reusedJWT != firstJWT {
		t.Fatalf("provider JWT was not reused within its cache window: %v", err)
	}
	rotatedJWT, err := sender.jwt(issuedAt.Add(51 * time.Minute))
	if err != nil || rotatedJWT == firstJWT {
		t.Fatalf("provider JWT was not rotated before its one-hour lifetime: %v", err)
	}
	sender.mu.Lock()
	sender.providerJWT = ""
	sender.jwtExpires = time.Time{}
	sender.mu.Unlock()
	response := sender.Send(context.Background(), PushRequest{Token: strings.Repeat("a", 64), APNSID: "apns-test-id", CollapseID: "zoe-test", Cursor: "42"})
	if !got || response.StatusCode != http.StatusOK || response.APNSID != "apns-test-id" {
		t.Fatalf("unexpected send result: got=%v response=%+v", got, response)
	}
}

func TestAPNSSenderRequiresFixedEnvironmentAndTopic(t *testing.T) {
	if _, err := NewAPNSSender(APNSConfig{Environment: "https://attacker.invalid", TeamID: "team", KeyID: "key", Topic: "com.example.app", PrivateKey: []byte("x")}); err == nil {
		t.Fatal("arbitrary APNs host was accepted")
	}
	if _, err := NewAPNSSender(APNSConfig{Environment: "production", TeamID: "team", KeyID: "key", Topic: "com.example.app/../other", PrivateKey: []byte("x")}); err == nil {
		t.Fatal("invalid topic was accepted")
	}
	id, err := randomUUID()
	if err != nil || len(id) != 36 {
		t.Fatalf("invalid generated APNs UUID: %q (%v)", id, err)
	}
}
