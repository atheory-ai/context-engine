package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var ceBin string

type cmdResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type fixtureProject struct {
	Dir     string
	DataDir string
}

func TestMain(m *testing.M) {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binDir, err := os.MkdirTemp("", "ce-acceptance-bin-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(binDir)

	ceBin = filepath.Join(binDir, "ce")
	build := exec.Command("go", "build", "-o", ceBin, "./cmd/ce")
	build.Dir = root
	build.Env = testEnv("")
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build ce: %v\n%s", err, out)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func newFixtureProject(t *testing.T) fixtureProject {
	t.Helper()
	dir := t.TempDir()
	dataDir := filepath.Join(dir, ".ce-data")

	writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/fixture\n\ngo 1.24\n")
	writeFile(t, filepath.Join(dir, "main.go"), `package main

func ProcessPayment() string {
	return InvoiceWriter()
}

func InvoiceWriter() string {
	return "invoice"
}
`)
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "fixture@example.invalid")
	runGit(t, dir, "config", "user.name", "CE Fixture")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	runGit(t, dir, "config", "tag.gpgsign", "false")
	runGit(t, dir, "remote", "add", "origin", "https://example.invalid/context-engine-fixture.git")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "fixture")

	return fixtureProject{Dir: dir, DataDir: dataDir}
}

func (p fixtureProject) run(t *testing.T, args ...string) cmdResult {
	t.Helper()
	return runCE(t, p.Dir, p.DataDir, "", args...)
}

func (p fixtureProject) runWithInput(t *testing.T, input string, args ...string) cmdResult {
	t.Helper()
	return runCE(t, p.Dir, p.DataDir, input, args...)
}

func runCE(t *testing.T, dir, dataDir, stdin string, args ...string) cmdResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	fullArgs := append([]string{"--data-dir", dataDir}, args...)
	cmd := exec.CommandContext(ctx, ceBin, fullArgs...)
	cmd.Dir = dir
	cmd.Env = testEnv(dataDir)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("ce %s timed out\nstdout:\n%s\nstderr:\n%s", strings.Join(args, " "), stdout.String(), stderr.String())
	}
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("ce %s: %v", strings.Join(args, " "), err)
		}
	}
	return cmdResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func initProject(t *testing.T, p fixtureProject) {
	t.Helper()
	input := "Fixture project for CE acceptance tests.\n\n\n\n3\n"
	res := p.runWithInput(t, input, "project", "init", ".")
	if res.ExitCode != 0 {
		t.Fatalf("project init failed (exit %d)\nstdout:\n%s\nstderr:\n%s", res.ExitCode, res.Stdout, res.Stderr)
	}
}

func enableAPIServerOnly(t *testing.T, p fixtureProject) {
	t.Helper()
	configPath := filepath.Join(p.Dir, "ce.yaml")
	f, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open ce.yaml: %v", err)
	}
	defer f.Close()
	_, err = f.WriteString(`
server:
  api_enabled: true
  mcp_enabled: false
  ws_enabled: false
`)
	if err != nil {
		t.Fatalf("write server config: %v", err)
	}
}

func writeLocalProviderConfig(t *testing.T, p fixtureProject) {
	t.Helper()
	writeFile(t, filepath.Join(p.Dir, "ce.yaml"), `project:
  git_url: https://example.invalid/context-engine-fixture.git
  base_prompt: Fixture project for CE acceptance tests.
  arch_prompt: Minimal Go project.
llm:
  provider: local
engine:
  max_loops: 1
  k_limit: 10
`)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func testEnv(dataDir string) []string {
	env := os.Environ()
	env = append(env,
		"CE_LLM_API_KEY=",
		"ANTHROPIC_API_KEY=",
		"OPENAI_API_KEY=",
		"NO_COLOR=1",
	)
	if dataDir != "" {
		env = append(env, "CE_DATA_DIR="+dataDir)
	}
	return env
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("TCP listeners are unavailable in this environment: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHTTP(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", url)
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find repo root from %s", dir)
		}
		dir = parent
	}
}

func readJSONLines(stdout string) []map[string]any {
	var out []map[string]any
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err == nil {
			out = append(out, obj)
		}
	}
	return out
}

func invalidWASMWithoutManifest() []byte {
	return []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
}
