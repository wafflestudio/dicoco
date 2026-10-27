package opencode

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Start owns a foreground OpenCode service. Its generated password is captured
// in memory, never logged or saved in the bot's conversation state.
func Start(ctx context.Context, binary, directory string, environment ...string) (*Client, func(), error) {
	processCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(processCtx, binary, "serve", "--hostname", "127.0.0.1", "--port", "0")
	cmd.Dir = directory
	cmd.Env = append(os.Environ(), environment...)
	if len(environment) > 0 {
		// Environment tokens override gh's config. Avoid accidentally using a
		// personal credential inherited from the bot's execution environment.
		var filtered []string
		for _, entry := range cmd.Env {
			key, _, _ := strings.Cut(entry, "=")
			if key != "GH_TOKEN" && key != "GITHUB_TOKEN" && key != "GH_ENTERPRISE_TOKEN" && key != "GITHUB_ENTERPRISE_TOKEN" {
				filtered = append(filtered, entry)
			}
		}
		cmd.Env = filtered
	}
	cmd.Stderr = io.Discard
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, fmt.Errorf("start OpenCode: %w", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stop := func() { cancel(); <-done }
	type endpoint struct{ url, password string }
	ready := make(chan endpoint, 1)
	go func() {
		var e endpoint
		sent := false
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "server listening on ") {
				e.url = strings.TrimPrefix(line, "server listening on ")
			}
			if strings.HasPrefix(line, "server password ") {
				e.password = strings.TrimPrefix(line, "server password ")
			}
			if !sent && e.url != "" && e.password != "" {
				ready <- e
				sent = true
			}
		}
	}()
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case e := <-ready:
		client, err := New(e.url, e.password, directory)
		if err != nil {
			stop()
			return nil, nil, err
		}
		return client, stop, nil
	case err := <-done:
		cancel()
		return nil, nil, fmt.Errorf("OpenCode exited before startup: %v", err)
	case <-timer.C:
		stop()
		return nil, nil, errors.New("OpenCode startup timed out")
	case <-ctx.Done():
		stop()
		return nil, nil, ctx.Err()
	}
}
