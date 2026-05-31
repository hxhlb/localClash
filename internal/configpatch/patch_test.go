package configpatch

import (
	"context"
	"encoding/gob"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"localclash/internal/configplan"
	"localclash/internal/localconfig"
	"localclash/internal/rules"
)

func TestImportPolicyTemplateWritesCanonicalPatchesAndCompiledIntent(t *testing.T) {
	dir := t.TempDir()
	templatesDir := filepath.Join(dir, "policy-templates")
	writeTestFile(t, filepath.Join(templatesDir, "minimal.json"), `{
  "id": "minimal",
  "name": "Minimal",
  "description": "Minimal template.",
  "config": {
    "version": 4,
    "policy_template": "minimal",
    "proxy_groups": {
      "DIRECT-ONLY": {"mode": "direct"}
    }
  }
}`)
	result, err := ImportPolicyTemplate(context.Background(), ImportTemplateOptions{
		RegistryDir:        filepath.Join(dir, "patches"),
		PolicyTemplatesDir: templatesDir,
		PolicyTemplate:     "minimal",
		ConfigPath:         filepath.Join(dir, "localclash-intent.json"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Imported || len(result.Patches) != 1 {
		t.Fatalf("import result = %+v, want one imported patch", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "patches", "default.minimal.v1_minimal-template.json")); err != nil {
		t.Fatalf("canonical patch file missing: %v", err)
	}
	config, err := localconfig.Load(filepath.Join(dir, "localclash-intent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if config.Generated == nil || config.Generated.Source != "patch_registry" || config.Generated.RegistryHash == "" {
		t.Fatalf("compiled metadata = %+v, want patch_registry hash", config.Generated)
	}
	if _, ok := config.ProxyGroups["DIRECT-ONLY"]; !ok {
		t.Fatalf("compiled proxy groups = %+v, want DIRECT-ONLY", config.ProxyGroups)
	}
}

func TestLoadRejectsDuplicatePatchIDAndOrderID(t *testing.T) {
	dir := t.TempDir()
	patch := Patch{
		Version: PatchVersion,
		PatchID: "user.one",
		Title:   "One",
		Source:  SourceUser,
		Status:  StatusEnabled,
		OrderID: "1000.000000",
	}
	writePatchJSON(t, filepath.Join(dir, "user.one_one.json"), patch)
	patch.Title = "Duplicate"
	writePatchJSON(t, filepath.Join(dir, "user.one_duplicate.json"), patch)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "duplicate patch_id") {
		t.Fatalf("Load duplicate id err = %v, want duplicate patch_id", err)
	}

	dir = t.TempDir()
	writePatchJSON(t, filepath.Join(dir, "user.one_one.json"), Patch{Version: PatchVersion, PatchID: "user.one", Title: "One", Source: SourceUser, Status: StatusEnabled, OrderID: "1000.000000"})
	writePatchJSON(t, filepath.Join(dir, "user.two_two.json"), Patch{Version: PatchVersion, PatchID: "user.two", Title: "Two", Source: SourceUser, Status: StatusDisabled, OrderID: "1000.000000"})
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "duplicate order_id") {
		t.Fatalf("Load duplicate order err = %v, want duplicate order_id", err)
	}
}

func TestPreviewOperationsManagePatchLifecycle(t *testing.T) {
	dir := t.TempDir()
	userPatch := Patch{Version: PatchVersion, PatchID: "user.one", Title: "One", Source: SourceUser, Status: StatusEnabled, OrderID: "1000.000000"}
	templatePatch := Patch{Version: PatchVersion, PatchID: "default.one", Title: "Default One", Source: SourcePolicyTemplate, Status: StatusEnabled, OrderID: "0200.000000"}
	writePatchJSON(t, filepath.Join(dir, "user.one_one.json"), userPatch)
	writePatchJSON(t, filepath.Join(dir, "default.one_default-one.json"), templatePatch)
	registry, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	records, _, _, impact, err := previewOperations(registry, []Operation{
		{Op: "remove_patch", PatchID: "user.one"},
		{Op: "remove_patch", PatchID: "default.one"},
	})
	if err != nil {
		t.Fatal(err)
	}
	recordsByID := byID(records)
	if _, ok := recordsByID["user.one"]; ok {
		t.Fatalf("user patch should be removed, records = %+v", records)
	}
	if got := recordsByID["default.one"].Patch.Status; got != StatusTombstoned {
		t.Fatalf("policy template patch status = %q, want tombstoned", got)
	}
	if len(impact.PatchesRemoved) != 1 || impact.PatchesRemoved[0] != "user.one" {
		t.Fatalf("impact removed = %+v, want user.one", impact.PatchesRemoved)
	}
}

func TestPreviewOperationsSetStatusAndReorder(t *testing.T) {
	dir := t.TempDir()
	writePatchJSON(t, filepath.Join(dir, "user.a_a.json"), Patch{Version: PatchVersion, PatchID: "user.a", Title: "A", Source: SourceUser, Status: StatusEnabled, OrderID: "1000.000000"})
	writePatchJSON(t, filepath.Join(dir, "user.b_b.json"), Patch{Version: PatchVersion, PatchID: "user.b", Title: "B", Source: SourceUser, Status: StatusEnabled, OrderID: "1001.000000"})
	writePatchJSON(t, filepath.Join(dir, "user.c_c.json"), Patch{Version: PatchVersion, PatchID: "user.c", Title: "C", Source: SourceUser, Status: StatusEnabled, OrderID: "1002.000000"})
	registry, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	records, operations, _, _, err := previewOperations(registry, []Operation{
		{Op: "set_patch_status", PatchID: "user.c", Status: StatusDisabled},
		{Op: "reorder_patch", PatchID: "user.c", AfterPatchID: "user.a", BeforePatchID: "user.b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	record := byID(records)["user.c"]
	if record.Patch.Status != StatusDisabled {
		t.Fatalf("status = %q, want disabled", record.Patch.Status)
	}
	if record.Patch.OrderID != "1000.500000" || operations[1].NormalizedOrderID != "1000.500000" {
		t.Fatalf("order = %q normalized = %q, want midpoint", record.Patch.OrderID, operations[1].NormalizedOrderID)
	}
}

func TestPreviewOperationsRejectsAutoOrderAllocationWithInvalidActiveOrderID(t *testing.T) {
	registry := Registry{
		Dir: t.TempDir(),
		Records: []Record{{
			Patch: Patch{
				Version: PatchVersion,
				PatchID: "user.bad-order",
				Title:   "Bad Order",
				Source:  SourceUser,
				Status:  StatusEnabled,
				OrderID: "bad",
			},
		}},
	}

	_, _, _, _, err := previewOperations(registry, []Operation{{
		Op:      "upsert_patch",
		PatchID: "user.new",
		Overlay: configplan.OverlayIntent{
			ProxyGroups: []configplan.OverlayProxyGroupIntent{{ID: "Direct", Mode: "direct"}},
		},
	}})
	if err == nil || !strings.Contains(err.Error(), `patch "user.bad-order" has invalid order_id "bad"`) ||
		!strings.Contains(err.Error(), "rebuild the affected Patch with an explicit valid order_id") {
		t.Fatalf("error = %v, want explicit invalid active order_id error", err)
	}
}

func TestDraftAndApplyCurrentDraftWritesRegistryAndArtifacts(t *testing.T) {
	dir := t.TempDir()
	writeSubscriptionGob(t, filepath.Join(dir, "subscription.gob"))
	writeTestPackIndex(t, filepath.Join(dir, ".runtime", "rules", "packs"))
	draft, err := Draft(context.Background(), DraftOptions{
		RegistryDir:        filepath.Join(dir, "patches"),
		ConfigPath:         filepath.Join(dir, "localclash-intent.json"),
		SelectionPath:      filepath.Join(dir, "localclash-packs.gob"),
		OutputPath:         filepath.Join(dir, "generated", "mihomo.yaml"),
		Subscription:       filepath.Join(dir, "subscription.gob"),
		RulesCache:         filepath.Join(dir, ".runtime", "rules", "packs"),
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
		Operations: []Operation{{
			Op:      "upsert_patch",
			PatchID: "user.direct",
			Title:   "Direct",
			OrderID: "1000.000000",
			Overlay: configplan.OverlayIntent{
				ProxyGroups: []configplan.OverlayProxyGroupIntent{{ID: "Direct", Mode: "direct"}},
			},
		}},
		Generation: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := Apply(context.Background(), ApplyOptions{
		RegistryDir:        filepath.Join(dir, "patches"),
		ConfigPath:         filepath.Join(dir, "localclash-intent.json"),
		SelectionPath:      filepath.Join(dir, "localclash-packs.gob"),
		OutputPath:         filepath.Join(dir, "generated", "mihomo.yaml"),
		Subscription:       filepath.Join(dir, "subscription.gob"),
		RulesCache:         filepath.Join(dir, ".runtime", "rules", "packs"),
		RuntimeProfilePath: filepath.Join(dir, "localclash-runtime.json"),
		BackupDir:          filepath.Join(dir, ".runtime", "backups"),
		Operations:         draft.Operations,
		BaseHashes:         draft.BaseHashes,
		BaseRegistryHash:   draft.BaseRegistryHash,
		Test:               false,
		Generation:         1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || result.RegistryHash == "" {
		t.Fatalf("apply result = %+v, want applied with hash", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "patches", "user.direct_direct.json")); err != nil {
		t.Fatalf("patch file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "generated", "mihomo.yaml")); err != nil {
		t.Fatalf("generated config missing: %v", err)
	}
}

func writePatchJSON(t *testing.T, path string, patch Patch) {
	t.Helper()
	data, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, string(data))
}

func writeTestPackIndex(t *testing.T, cacheDir string) {
	t.Helper()
	if err := rules.WritePackIndex(rules.PackIndexPath(cacheDir), map[string]rules.PackCache{
		"test": {
			Version:    1,
			Source:     "test",
			Adapter:    "test",
			Renderable: true,
			Packs: []rules.Pack{{
				ID:         "Baseline",
				Name:       "Baseline",
				Target:     "AUTO",
				Renderable: true,
				Components: []rules.Component{{
					ID:         "domain",
					Behavior:   "classical",
					Format:     "yaml",
					OrderClass: "domain",
					URL:        "https://example.com/baseline.yaml",
					Path:       "./rule-packs/test/baseline.yaml",
				}},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func writeSubscriptionGob(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	gob.Register(map[string]any{})
	gob.Register([]any{})
	err = gob.NewEncoder(file).Encode(struct {
		Version int
		Data    map[string]any
		Raw     []byte
	}{
		Version: 1,
		Data: map[string]any{
			"proxies": []any{
				map[string]any{"name": "SG 01", "type": "ss", "server": "example.com", "port": 443, "cipher": "none", "password": "test"},
			},
		},
	})
	closeErr := file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}

func writeTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
