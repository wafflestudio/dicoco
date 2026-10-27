// Package githubauth supplies short-lived GitHub App credentials to the normal
// gh CLI. Repository investigation and reviews are performed by OpenCode/gh.
package githubauth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const Organization = "wafflestudio"

type App struct {
	appID   string
	key     *rsa.PrivateKey
	baseURL string
	http    *http.Client
}

func New(appID, keyFile string) (*App, error) {
	if _, err := strconv.ParseUint(appID, 10, 64); err != nil || appID == "0" {
		return nil, errors.New("GitHub App ID must be a positive number")
	}
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read GitHub App private key: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("GitHub App private key is not PEM encoded")
	}
	var key *rsa.PrivateKey
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		var parsed any
		parsed, err = x509.ParsePKCS8PrivateKey(block.Bytes)
		if err == nil {
			var ok bool
			key, ok = parsed.(*rsa.PrivateKey)
			if !ok {
				err = errors.New("key is not RSA")
			}
		}
	default:
		err = errors.New("unsupported private key type")
	}
	if err != nil {
		return nil, fmt.Errorf("parse GitHub App private key: %w", err)
	}
	return &App{appID: appID, key: key, baseURL: "https://api.github.com", http: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (a *App) jwt(now time.Time) (string, error) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iss": a.appID, "iat": now.Add(-time.Minute).Unix(), "exp": now.Add(9 * time.Minute).Unix()})
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (a *App) request(ctx context.Context, method, path, jwt string, body, out any) error {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.baseURL+path, input)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "dicoco")
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("GitHub App request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub App %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func (a *App) token(ctx context.Context) (string, time.Time, error) {
	jwt, err := a.jwt(time.Now())
	if err != nil {
		return "", time.Time{}, err
	}
	var installation struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
	}
	if err := a.request(ctx, "GET", "/orgs/"+Organization+"/installation", jwt, nil, &installation); err != nil {
		return "", time.Time{}, err
	}
	if installation.ID <= 0 || !strings.EqualFold(installation.Account.Login, Organization) {
		return "", time.Time{}, errors.New("GitHub App is not installed on wafflestudio")
	}
	var response struct {
		Token   string    `json:"token"`
		Expires time.Time `json:"expires_at"`
	}
	// Do not inherit other permissions that may have been granted to the App.
	permissions := map[string]any{"permissions": map[string]string{"contents": "read", "pull_requests": "write", "metadata": "read"}}
	if err := a.request(ctx, "POST", fmt.Sprintf("/app/installations/%d/access_tokens", installation.ID), jwt, permissions, &response); err != nil {
		return "", time.Time{}, err
	}
	if response.Token == "" || !response.Expires.After(time.Now().Add(5*time.Minute)) {
		return "", time.Time{}, errors.New("GitHub returned an empty or nearly expired installation token")
	}
	return response.Token, response.Expires, nil
}

// Start writes an isolated gh config, then refreshes it before each token expiry.
// gh reads this file on every invocation; OpenCode need not be restarted hourly.
// The returned cleanup removes only this newly created temporary directory.
func (a *App) Start(ctx context.Context) (string, func(), error) {
	dir, err := os.MkdirTemp("", "dicoco-gh-")
	if err != nil {
		return "", nil, err
	}
	refreshCtx, cancel := context.WithCancel(ctx)
	refresh := func() (time.Time, error) {
		requestCtx, stop := context.WithTimeout(refreshCtx, 30*time.Second)
		defer stop()
		token, expires, err := a.token(requestCtx)
		if err != nil {
			return time.Time{}, err
		}
		if err := writeConfig(dir, token); err != nil {
			return time.Time{}, err
		}
		log.Printf("[github] gh authenticated as App on %s; expires=%s", Organization, expires.UTC().Format(time.RFC3339))
		return expires, nil
	}
	expires, err := refresh()
	if err != nil {
		cancel()
		os.RemoveAll(dir)
		return "", nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		delay := time.Until(expires.Add(-5 * time.Minute))
		timer := time.NewTimer(delay)
		defer timer.Stop()
		for {
			select {
			case <-refreshCtx.Done():
				return
			case <-timer.C:
				next, err := refresh()
				if err != nil {
					if ctx.Err() != nil {
						return
					}
					log.Printf("[github] refresh token: %v", err)
					if !time.Now().Before(expires) {
						os.Remove(filepath.Join(dir, "hosts.yml"))
					}
					timer.Reset(time.Minute)
				} else {
					expires = next
					timer.Reset(time.Until(expires.Add(-5 * time.Minute)))
				}
			}
		}
	}()
	return dir, func() { cancel(); <-done; os.RemoveAll(dir) }, nil
}

func writeConfig(dir, token string) error {
	quoted, _ := json.Marshal(token) // JSON double-quoted strings are also valid YAML.
	content := []byte("github.com:\n    git_protocol: https\n    oauth_token: " + string(quoted) + "\n")
	f, err := os.CreateTemp(dir, ".hosts-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(content); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(dir, "hosts.yml"))
}
