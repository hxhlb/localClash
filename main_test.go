package main

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"localclash/internal/appinit"
	"localclash/internal/coredownload"
	"localclash/internal/rules"
	"localclash/internal/runtimeprofile"

	"gopkg.in/yaml.v3"
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

func TestRunRuntimeStatusUsesDetectedWorkDir(t *testing.T) {
	installDir := t.TempDir()
	wrongDir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", installDir)
	t.Chdir(wrongDir)

	workDir := filepath.Join(installDir, ".runtime", "mihomo")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(installDir, "generated", "mihomo.yaml")
	if err := os.MkdirAll(filepath.Dir(config), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte("external-controller: 127.0.0.1:9090\nexternal-ui: ui/zashboard\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	core := filepath.Join(installDir, "bin", "linux-"+runtime.GOARCH, "mihomo-meta")
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
			Running    bool   `json:"running"`
			PID        int    `json:"pid"`
			RuntimeDir string `json:"runtime_dir"`
			Config     string `json:"config"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("status JSON = %q, error = %v", output, err)
	}
	if !result.OK || !result.Status.Running || result.Status.PID != cmd.Process.Pid {
		t.Fatalf("status result = %+v, want detected runtime", result)
	}
	if result.Status.RuntimeDir != workDir || result.Status.Config != config {
		t.Fatalf("status paths = runtime %q config %q, want detected workdir", result.Status.RuntimeDir, result.Status.Config)
	}
	if _, err := os.Stat(filepath.Join(wrongDir, ".runtime")); !os.IsNotExist(err) {
		t.Fatalf("runtime status should not create state under wrong cwd, err=%v", err)
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

func TestRunProductResetFullDryRunPrintsJSONEnvelope(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "localclash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	writeMainTestFile(t, "localclash.json", "version: 1\n")
	expected, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() error {
		return run([]string{"reset", "--full", "--dry-run", "--json"})
	})
	var result struct {
		OK      bool `json:"ok"`
		Changed bool `json:"changed"`
		Status  struct {
			Full    bool `json:"full"`
			DryRun  bool `json:"dry_run"`
			Deleted []struct {
				Path string `json:"path"`
			} `json:"deleted"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("product reset JSON = %q, error = %v", output, err)
	}
	if !result.OK || result.Changed || !result.Status.Full || !result.Status.DryRun || len(result.Status.Deleted) != 1 || result.Status.Deleted[0].Path != expected {
		t.Fatalf("product reset result = %+v, want full dry-run envelope for %s", result, expected)
	}
	if _, err := os.Stat(filepath.Join(dir, "localclash.json")); err != nil {
		t.Fatalf("dry-run should keep localclash.json: %v", err)
	}
}

func TestRunProductConfigRenderUsesDurableLocalClashIntent(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	writeMainTestFile(t, "subscription.gob", `proxies:
  - name: "HK 01"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)
	writeMainTestFile(t, "localclash.json", `version: 1
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
	writeMainTestPackIndex(t, filepath.Join(".runtime", "rules", "packs"))

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
	if !result.OK || result.Status.Source != "durable_state" || result.Status.Selection != "localclash-packs.gob" {
		t.Fatalf("config render result = %+v, want durable state with derived selection", result)
	}
	generated, err := os.ReadFile(filepath.Join("generated", "mihomo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(generated)
	if !strings.Contains(text, "name: AI") || !strings.Contains(text, "DOMAIN,example.ai,AI") {
		t.Fatalf("generated config did not consume localclash.json intent:\n%s", text)
	}
	if _, err := os.Stat("localclash-packs.gob"); err != nil {
		t.Fatalf("derived localclash-packs.gob missing: %v", err)
	}
}

func TestRunProductConfigRenderUsesEnvWorkspaceFromNeutralCwd(t *testing.T) {
	installDir := t.TempDir()
	wrongDir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", installDir)
	t.Chdir(wrongDir)

	writeMainTestFile(t, filepath.Join(installDir, "subscription.gob"), `proxies:
  - name: "HK 01"
    type: ss
    server: example.com
    port: 443
    cipher: none
    password: test
`)
	writeMainTestFile(t, filepath.Join(installDir, "localclash.json"), `version: 1
policy_template: localclash-default
proxy_groups:
  AI:
    mode: auto
    match:
      type: name_regex
      pattern: ".*"
      min: 1
`)
	writeMainTestPackIndex(t, filepath.Join(installDir, ".runtime", "rules", "packs"))

	output := captureStdout(t, func() error {
		return run([]string{"config", "render", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			Selection string `json:"selection"`
			Output    string `json:"output"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("config render JSON = %q, error = %v", output, err)
	}
	if !result.OK || result.Status.Selection != filepath.Join(installDir, "localclash-packs.gob") || result.Status.Output != filepath.Join(installDir, "generated", "mihomo.yaml") {
		t.Fatalf("config render result = %+v, want paths under %s", result, installDir)
	}
	if _, err := os.Stat(filepath.Join(installDir, "generated", "mihomo.yaml")); err != nil {
		t.Fatalf("generated config should be written under workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wrongDir, "generated", "mihomo.yaml")); !os.IsNotExist(err) {
		t.Fatalf("generated config should not be written under cwd, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(wrongDir, "localclash-packs.gob")); !os.IsNotExist(err) {
		t.Fatalf("selection should not be written under cwd, err=%v", err)
	}
}

func TestRunProductComponentUpdateMihomoRefreshesCoreVersionCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	core := filepath.Join(dir, runtimeprofile.MetaCorePath)
	oldDownloadCore := downloadCore
	downloadCore = func(ctx context.Context, opts coredownload.Options) ([]coredownload.Result, error) {
		writeMainExecutableCore(t, core, "Mihomo component update")
		return []coredownload.Result{{OutputPath: core, Flavor: coredownload.FlavorMeta, Target: opts.Target}}, nil
	}
	t.Cleanup(func() {
		downloadCore = oldDownloadCore
	})

	output := captureStdout(t, func() error {
		return run([]string{"component", "update", "mihomo", "--json"})
	})
	var result struct {
		OK       bool     `json:"ok"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("component update JSON = %q, error = %v", output, err)
	}
	if !result.OK || len(result.Warnings) != 0 {
		t.Fatalf("component update result = %+v, want ok without cache warning", result)
	}
	cache := readMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")))
	if cache.CorePath != core || cache.Version != "Mihomo component update" {
		t.Fatalf("cache = %+v, want refreshed component update core %s", cache, core)
	}
}

func TestExecuteDesiredConfigRefreshesCoreVersionCacheAfterRuntimeProfileSwitch(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	meta := filepath.Join(dir, runtimeprofile.MetaCorePath)
	smart := filepath.Join(dir, runtimeprofile.SmartCorePath)
	writeMainExecutableCore(t, meta, "Mihomo meta")
	writeMainExecutableCore(t, smart, "Mihomo smart")
	state := appinit.Bootstrap(context.Background(), appinit.Options{})

	changed, warnings, err := executeDesiredConfig(context.Background(), &desiredConfig{
		RuntimeProfile: runtimeprofile.ModeRouter,
		Core:           runtimeprofile.CoreSmart,
	}, state)
	if err != nil {
		t.Fatal(err)
	}
	if !changed || len(warnings) != 0 {
		t.Fatalf("changed=%v warnings=%v, want profile switch without warnings", changed, warnings)
	}
	cache := readMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")))
	if cache.CorePath != smart || cache.Version != "Mihomo smart" || !cache.SmartSupported {
		t.Fatalf("cache = %+v, want smart core refresh", cache)
	}
}

func TestRunProductRuntimeStartRefreshesCoreVersionCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	core := filepath.Join(dir, runtimeprofile.MetaCorePath)
	writeMainExecutableCore(t, core, "Mihomo runtime start")
	writeMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")), core, "Mihomo stale")
	config := filepath.Join(dir, "generated", "mihomo.yaml")
	writeMainTestFile(t, config, "mixed-port: 7890\n")

	output := captureStdout(t, func() error {
		return run([]string{"runtime", "start", "--json"})
	})
	var result struct {
		OK     bool `json:"ok"`
		Status struct {
			PID int `json:"pid"`
		} `json:"status"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("runtime start JSON = %q, error = %v", output, err)
	}
	if result.Status.PID > 0 {
		t.Cleanup(func() {
			if process, err := os.FindProcess(result.Status.PID); err == nil {
				_ = process.Kill()
				_, _ = process.Wait()
			}
		})
	}
	if !result.OK || result.Status.PID == 0 {
		t.Fatalf("runtime start result = %+v, want started runtime", result)
	}
	cache := readMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")))
	if cache.CorePath != core || cache.Version != "Mihomo runtime start" {
		t.Fatalf("cache = %+v, want runtime start refresh", cache)
	}
}

func TestRunDoctorUsesLiveCoreProbeWhenBootstrapUsesCachedVersion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALCLASH_WORKDIR", dir)
	t.Chdir(dir)
	core := filepath.Join(dir, runtimeprofile.MetaCorePath)
	countPath := filepath.Join(dir, "version-count")
	writeMainCountingCore(t, core, countPath, "Mihomo doctor live")
	writeMainCoreCache(t, appinit.CoreVersionCachePath(filepath.Join(dir, ".runtime")), core, "Mihomo cached")

	_ = captureStdout(t, func() error {
		return run([]string{"doctor", "--json"})
	})
	if got := readMainCount(t, countPath); got != 1 {
		t.Fatalf("doctor core -v count = %d, want 1 live probe", got)
	}
}

func startFakeRuntime(t *testing.T, core, workDir, config string) *exec.Cmd {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(core), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core, []byte("#!/bin/sh\nif [ \"$1\" = \"-v\" ]; then echo \"mihomo fake\"; exit 0; fi\nsleep 300\n"), 0o755); err != nil {
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

type mainCoreCache struct {
	CorePath       string `json:"core_path"`
	Version        string `json:"version"`
	SmartSupported bool   `json:"smart_supported"`
	UpdatedAt      string `json:"updated_at"`
}

func writeMainExecutableCore(t *testing.T, path, version string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-v\" ]; then echo " + strconv.Quote(version) + "; exit 0; fi\n" +
		"for arg in \"$@\"; do if [ \"$arg\" = \"-t\" ]; then echo ok; exit 0; fi; done\n" +
		"sleep 300\n"
	writeMainTestFile(t, path, script)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeMainCountingCore(t *testing.T, path, countPath, version string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"-v\" ]; then\n" +
		"  count=0\n" +
		"  [ -f " + strconv.Quote(countPath) + " ] && count=$(cat " + strconv.Quote(countPath) + ")\n" +
		"  count=$((count + 1))\n" +
		"  echo \"$count\" > " + strconv.Quote(countPath) + "\n" +
		"  echo " + strconv.Quote(version) + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"for arg in \"$@\"; do if [ \"$arg\" = \"-t\" ]; then echo ok; exit 0; fi; done\n" +
		"sleep 300\n"
	writeMainTestFile(t, path, script)
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeMainCoreCache(t *testing.T, path, corePath, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	cache := mainCoreCache{
		CorePath:       corePath,
		Version:        version,
		SmartSupported: strings.Contains(strings.ToLower(version), "smart"),
		UpdatedAt:      "2026-05-28T09:00:00Z",
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMainCoreCache(t *testing.T, path string) mainCoreCache {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cache mainCoreCache
	if err := json.Unmarshal(data, &cache); err != nil {
		t.Fatal(err)
	}
	return cache
}

func readMainCount(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	return count
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
	var data []byte
	var err error
	switch filepath.Ext(path) {
	case ".json":
		var doc any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		data, err = json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
	case ".gob":
		gob.Register(map[string]any{})
		gob.Register([]any{})
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		encodeErr := gob.NewEncoder(file).Encode(struct {
			Version int
			Data    map[string]any
			Raw     []byte
		}{Version: 1, Data: doc, Raw: []byte(content)})
		closeErr := file.Close()
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
		return
	default:
		data = []byte(content)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeMainTestPackIndex(t *testing.T, cacheDir string) {
	t.Helper()
	if err := rules.WritePackIndex(rules.PackIndexPath(cacheDir), map[string]rules.PackCache{
		"blackmatrix7": {
			Version:    1,
			Source:     "blackmatrix7",
			Adapter:    "blackmatrix7",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "OpenAI",
				Name:       "OpenAI",
				Target:     "AI",
				Renderable: true,
				Components: []rules.Component{{
					ID:         "OpenAI",
					Behavior:   "classical",
					Format:     "yaml",
					OrderClass: "mixed",
					URL:        "https://example.com/OpenAI.yaml",
					Path:       "./rule-packs/blackmatrix7/OpenAI.yaml",
				}},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
}
