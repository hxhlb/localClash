package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestRunResetDoesNotBootstrapRuntimeFirst(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := run([]string{"reset", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(".runtime"); !os.IsNotExist(err) {
		t.Fatalf("reset should run before bootstrap creates .runtime, err=%v", err)
	}
}

func TestRunRuntimeStatusPrintsJSONEnvelope(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	workDir := filepath.Join(".runtime", "mihomo")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join("generated", "mihomo.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join("bin", "linux-"+runtime.GOARCH, "mihomo-meta")
	cmd := startFakeRuntime(t, core, workDir, config)
	if err := os.WriteFile(filepath.Join(workDir, "mihomo.pid"), []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return run([]string{"runtime", "status", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			Running       bool   `json:"running"`
			PID           int    `json:"pid"`
			ExternalUIURL string `json:"external_ui_url"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("status JSON = %q, error = %v", output, err)
	}
	if !result.OK || !result.Status.Running || result.Status.PID != cmd.Process.Pid || result.Status.ExternalUIURL != "http://127.0.0.1:9090/ui" {
		t.Fatalf("status result = %+v, want current pid and external UI", result)
	}
}

func TestRunProductStatusPrintsJSONEnvelope(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	output := captureStdout(t, func() error {
		return run([]string{"status", "--json"})
	})
	var result struct {
		OK      bool           `json:"ok"`
		Changed bool           `json:"changed"`
		Status  map[string]any `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("product status JSON = %q, error = %v", output, err)
	}
	if !result.OK || result.Changed || result.Status["runtime"] == nil || result.Status["components"] == nil {
		t.Fatalf("product status result = %+v, want product status envelope", result)
	}
}

func TestRunProductConfigRenderUsesDurableLocalClashIntent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeMainTestFile(t, filepath.Join("policies", "loyalsoldier.yaml"), `rule_source:
  base_url: https://example.com/rules
  update_interval: 86400
groups:
  direct: DIRECT
  reject: REJECT
  proxy: PROXY
  auto: AUTO
  manual: MANUAL
  apple: Apple
provider_mapping:
  applications:
    path: applications.txt
    behavior: classical
    target: direct
  proxy:
    path: proxy.txt
    behavior: domain
    target: proxy
modes:
  default: whitelist
  whitelist:
    rules:
      - provider: applications
        target: direct
      - provider: proxy
        target: proxy
      - match: true
        target: proxy
`)
	writeMainTestFile(t, "subscription.yaml", `proxies:
  - name: "HK 01"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)
	writeMainTestFile(t, "localclash.yaml", `version: 1
policy_template: localclash-default
proxy_groups:
  AI:
    mode: auto
    match:
      type: name_regex
      pattern: ".*"
      min: 1
custom_rules:
  - id: ai_test
    target: AI
    rules:
      - type: DOMAIN
        value: example.ai
`)

	output := captureStdout(t, func() error {
		return run([]string{"config", "render", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			Source    string `json:"source"`
			Selection string `json:"selection"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("config render JSON = %q, error = %v", output, err)
	}
	if !result.OK || result.Status.Source != "durable_state" || result.Status.Selection != "localclash-packs.yaml" {
		t.Fatalf("config render result = %+v, want durable state with derived selection", result)
	}
	generated, err := os.ReadFile(filepath.Join("generated", "mihomo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	if !strings.Contains(text, "name: AI") || !strings.Contains(text, "DOMAIN,example.ai,AI") {
		t.Fatalf("generated config did not consume localclash.yaml intent:\n%s", text)
	}
	if _, err := os.Stat("localclash-packs.yaml"); err != nil {
		t.Fatalf("derived localclash-packs.yaml missing: %v", err)
	}
}

func startFakeRuntime(t *testing.T, core, workDir, config string) *exec.Cmd {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(core), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(core, "-d", workDir, "-f", config)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	return cmd
}

func TestRunStopRemovesStalePIDFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	workDir := filepath.Join(dir, "runtime")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(workDir, "mihomo.pid")
	if err := os.WriteFile(pidFile, []byte("not-a-pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return run([]string{"stop", "--workdir", workDir, "--json"})
	})
	var result struct {
		Stopped        bool `json:"stopped"`
		StalePIDFile   bool `json:"stale_pid_file"`
		RemovedPIDFile bool `json:"removed_pid_file"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("stop JSON = %q, error = %v", output, err)
	}
	if result.Stopped || !result.StalePIDFile || !result.RemovedPIDFile {
		t.Fatalf("stop result = %+v, want stale pid file removed", result)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("pid file should be removed, err=%v", err)
	}
}

func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	err = fn()
	if closeErr := writer.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	os.Stdout = original
	t.Cleanup(func() {
		os.Stdout = original
		_ = reader.Close()
	})
	data, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeMainTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
