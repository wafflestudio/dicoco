package githubauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0600); err != nil {
		t.Fatal(err)
	}
	app, err := New("5490993", file)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func TestJWTSigningAndClaims(t *testing.T) {
	a := testApp(t)
	now := time.Now().Truncate(time.Second)
	token, err := a.jwt(now)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatal("JWT needs three parts")
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&a.key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatal(err)
	}
	data, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		Issuer  string `json:"iss"`
		Issued  int64  `json:"iat"`
		Expires int64  `json:"exp"`
	}
	if err := json.Unmarshal(data, &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Issuer != "5490993" || claims.Issued != now.Add(-time.Minute).Unix() || claims.Expires > now.Add(10*time.Minute).Unix() {
		t.Fatal("incorrect JWT claims")
	}
}

func TestAppInstallationAndGHConfig(t *testing.T) {
	a := testApp(t)
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			t.Error("missing App JWT")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/orgs/wafflestudio/installation":
			if r.Method != "GET" {
				t.Error("expected read installation")
			}
			fmt.Fprint(w, `{"id":123,"account":{"login":"wafflestudio"}}`)
		case "/app/installations/123/access_tokens":
			var body struct {
				Permissions map[string]string `json:"permissions"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "POST" || body.Permissions["contents"] != "read" || body.Permissions["pull_requests"] != "write" || len(body.Permissions) != 3 {
				t.Error("unexpected token permissions")
			}
			json.NewEncoder(w).Encode(map[string]any{"token": "ghs_test_only_token", "expires_at": time.Now().Add(time.Hour)})
		default:
			t.Errorf("unexpected API path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	a.baseURL = srv.URL
	dir, stop, err := a.Start(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	if calls != 2 {
		t.Fatalf("want automatic installation discovery + token creation; got %d", calls)
	}
	file := filepath.Join(dir, "hosts.yml")
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("gh token file must be owner-readable only")
	}
	if _, err := exec.LookPath("gh"); err != nil {
		t.Skip("gh not installed; config creation tested")
	}
	cmd := exec.Command("gh", "auth", "token", "--hostname", "github.com")
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+dir, "GH_TOKEN=", "GITHUB_TOKEN=", "GH_PROMPT_DISABLED=1")
	output, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) != "ghs_test_only_token" {
		t.Fatal("gh did not read the generated auth config")
	}
	if err := writeConfig(dir, "ghs_rotated_test_token"); err != nil {
		t.Fatal(err)
	}
	cmd = exec.Command("gh", "auth", "token", "--hostname", "github.com")
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+dir, "GH_TOKEN=", "GITHUB_TOKEN=")
	output, err = cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) != "ghs_rotated_test_token" {
		t.Fatal("gh did not pick up a refreshed token")
	}
}

func TestAuthErrorDoesNotLogResponseBody(t *testing.T) {
	a := testApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"message":"sensitive response body"}`)
	}))
	defer srv.Close()
	a.baseURL = srv.URL
	_, _, err := a.token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("incorrect diagnostic: %v", err)
	}
}
