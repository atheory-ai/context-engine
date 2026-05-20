package acceptance

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectInitAcceptance(t *testing.T) {
	project := newFixtureProject(t)
	initProject(t, project)

	if _, err := os.Stat(filepath.Join(project.Dir, "ce.yaml")); err != nil {
		t.Fatalf("ce.yaml not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project.DataDir, "meta.db")); err != nil {
		t.Fatalf("meta.db not created: %v", err)
	}

	show := project.run(t, "config", "show")
	if show.ExitCode != 0 {
		t.Fatalf("config show failed: %s", show.Stderr)
	}
	if !strings.Contains(show.Stdout, "context-engine-fixture") {
		t.Fatalf("config show missing fixture git URL/name:\n%s", show.Stdout)
	}
}

func TestIndexAcceptance(t *testing.T) {
	project := newFixtureProject(t)
	initProject(t, project)

	res := project.run(t, "index", "--full", ".")
	if res.ExitCode != 0 {
		t.Fatalf("index failed (exit %d)\nstdout:\n%s\nstderr:\n%s", res.ExitCode, res.Stdout, res.Stderr)
	}
	if !strings.Contains(res.Stdout, "Index complete:") {
		t.Fatalf("index summary missing:\nstdout:\n%s\nstderr:\n%s", res.Stdout, res.Stderr)
	}
	if _, err := os.Stat(filepath.Join(project.DataDir, "graphs", "local.db")); err != nil {
		t.Fatalf("local graph DB not created: %v", err)
	}
}

func TestQueryAcceptancePreflight(t *testing.T) {
	project := newFixtureProject(t)
	initProject(t, project)

	res := project.run(t, "query", "How does ProcessPayment work?")
	if res.ExitCode == 0 {
		t.Fatalf("query unexpectedly succeeded before indexing\nstdout:\n%s", res.Stdout)
	}
	combined := res.Stdout + res.Stderr
	if !strings.Contains(combined, "project not yet indexed") {
		t.Fatalf("query should surface deterministic preflight failure, got:\n%s", combined)
	}
}

func TestServerAcceptanceStartsAndStops(t *testing.T) {
	project := newFixtureProject(t)
	initProject(t, project)
	enableAPIServerOnly(t, project)
	port := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, ceBin,
		"--data-dir", project.DataDir,
		"server", "start",
		"--host", "127.0.0.1",
		"--port", fmt.Sprint(port),
	)
	cmd.Dir = project.Dir
	cmd.Env = testEnv(project.DataDir)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})
	go io.Copy(io.Discard, stdout) //nolint:errcheck
	go io.Copy(io.Discard, stderr) //nolint:errcheck

	waitForHTTP(t, fmt.Sprintf("http://127.0.0.1:%d/health", port))
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
	if err != nil {
		t.Fatalf("health request: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("health status = %q, want ok", body["status"])
	}
}

func TestMCPStdioAcceptance(t *testing.T) {
	project := newFixtureProject(t)
	initProject(t, project)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ceBin, "--data-dir", project.DataDir, "mcp-stdio")
	cmd.Dir = project.Dir
	cmd.Env = testEnv(project.DataDir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("mcp-stdio start: %v", err)
	}
	requests := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"acceptance","version":"test"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	}
	for _, req := range requests {
		if _, err := fmt.Fprintln(stdin, req); err != nil {
			t.Fatalf("write mcp request: %v", err)
		}
	}
	_ = stdin.Close()

	lines := readMCPResponses(t, stdout, 2)
	if err := cmd.Wait(); err != nil {
		t.Fatalf("mcp-stdio failed: %v\nstderr:\n%s", err, stderr.String())
	}

	if len(lines) != 2 {
		t.Fatalf("mcp response count = %d, want 2", len(lines))
	}
	if result, ok := lines[0]["result"].(map[string]any); !ok || result["protocolVersion"] == "" {
		t.Fatalf("initialize response missing protocolVersion: %#v", lines[0])
	}
	result, ok := lines[1]["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list result missing: %#v", lines[1])
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools/list returned no tools: %#v", result)
	}
}

func TestPluginValidateAcceptance(t *testing.T) {
	project := newFixtureProject(t)
	wasmPath := filepath.Join(project.Dir, "missing-manifest.wasm")
	if err := os.WriteFile(wasmPath, invalidWASMWithoutManifest(), 0644); err != nil {
		t.Fatal(err)
	}

	res := project.run(t, "plugin", "validate", wasmPath, "--json")
	if res.ExitCode != 0 {
		t.Fatalf("plugin validate --json should report invalid plugin without failing command\nstdout:\n%s\nstderr:\n%s", res.Stdout, res.Stderr)
	}
	var payload struct {
		Passed bool     `json:"passed"`
		Errors []string `json:"errors"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &payload); err != nil {
		t.Fatalf("invalid JSON output:\n%s\nerr: %v", res.Stdout, err)
	}
	if payload.Passed {
		t.Fatalf("invalid plugin reported as passed:\n%s", res.Stdout)
	}
	if len(payload.Errors) == 0 || !strings.Contains(payload.Errors[0], "missing required export") {
		t.Fatalf("expected missing export error, got: %#v", payload.Errors)
	}
}

func TestTokenAcceptance(t *testing.T) {
	project := newFixtureProject(t)
	res := project.run(t, "token", "create", "--name", "acceptance", "--scope", "read", "--expires-days", "7")
	if res.ExitCode != 0 {
		t.Fatalf("token create failed:\nstdout:\n%s\nstderr:\n%s", res.Stdout, res.Stderr)
	}
	for _, want := range []string{"Token created:", "ID:    tok_acceptance", "Scope: read", "CE_TOKEN=tok_acceptance"} {
		if !strings.Contains(res.Stdout, want) {
			t.Fatalf("token output missing %q:\n%s", want, res.Stdout)
		}
	}

	invalid := project.run(t, "token", "create", "--name", "bad", "--scope", "write-only")
	if invalid.ExitCode == 0 {
		t.Fatalf("invalid token scope unexpectedly succeeded:\n%s", invalid.Stdout)
	}
	if !strings.Contains(invalid.Stderr, "invalid scope") {
		t.Fatalf("invalid token scope error missing:\nstdout:\n%s\nstderr:\n%s", invalid.Stdout, invalid.Stderr)
	}
}

func readMCPResponses(t *testing.T, stdout io.Reader, want int) []map[string]any {
	t.Helper()
	scanner := bufio.NewScanner(stdout)
	var responses []map[string]any
	for scanner.Scan() {
		var obj map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &obj); err != nil {
			t.Fatalf("decode MCP response %q: %v", scanner.Text(), err)
		}
		responses = append(responses, obj)
		if len(responses) == want {
			return responses
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read MCP stdout: %v", err)
	}
	return responses
}
