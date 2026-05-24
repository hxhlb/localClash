package policytemplate

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBuildMinimalTemplate(t *testing.T) {
	dir := writeTemplateFixture(t)
	config, summary, err := Build(dir, TemplateMinimal)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != TemplateMinimal || config.PolicyTemplate != TemplateMinimal {
		t.Fatalf("template = %+v config = %+v, want minimal", summary, config)
	}
	if len(config.Packs) != 0 || len(config.ProxyGroups) != 0 {
		t.Fatalf("minimal config = %+v, want no packs or proxy groups", config)
	}
}

func TestBuildLocalClashDefaultTemplate(t *testing.T) {
	dir := writeTemplateFixture(t)
	config, summary, err := Build(dir, TemplateLocalClashDefault)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != TemplateLocalClashDefault || config.PolicyTemplate != TemplateLocalClashDefault {
		t.Fatalf("template = %+v config = %+v, want localclash default", summary, config)
	}
	if len(config.ProxyGroups) == 0 || len(config.Packs) == 0 {
		t.Fatalf("default config = %+v, want proxy groups and packs", config)
	}
	if config.Packs[0].ID != "v2fly_dlc_category_ads_all" || config.Packs[0].Target != "REJECT" {
		t.Fatalf("first pack = %+v, want ads reject first", config.Packs[0])
	}
	group := config.ProxyGroups["AI"]
	if group.Mode != "auto" || group.Match == nil || group.Match.Pattern != ".*" {
		t.Fatalf("AI group = %+v, want auto all-nodes selector", group)
	}
}

func TestRealLocalClashDefaultTemplateIsLayered(t *testing.T) {
	config, summary, err := Build(filepath.Join("..", "..", DefaultDir), TemplateLocalClashDefault)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ID != TemplateLocalClashDefault || config.Version != 2 {
		t.Fatalf("template = %+v config version = %d, want v2 localclash default", summary, config.Version)
	}
	if _, exists := config.ProxyGroups["STEAM"]; exists {
		t.Fatalf("default template still has flat STEAM proxy group: %+v", config.ProxyGroups["STEAM"])
	}
	if !config.ProxyGroups["🇭🇰 香港节点"].Optional {
		t.Fatalf("香港节点 group = %+v, want optional region selector", config.ProxyGroups["🇭🇰 香港节点"])
	}
	steam := config.PolicyGroups["🎮 Steam"]
	if steam.Mode != "manual" || len(steam.Exits) == 0 {
		t.Fatalf("Steam policy group = %+v, want business-to-exit selector", steam)
	}
	wantExits := []string{"🎯 手动选择", "⚡ 自动选择", "🇭🇰 香港节点", "🇺🇸 美国节点", "🇯🇵 日本节点", "🇸🇬 新加坡节点", "🇹🇼 台湾节点", "🇰🇷 韩国节点", "🌐 全球直连"}
	for id, group := range config.PolicyGroups {
		if !reflect.DeepEqual(group.Exits, wantExits) {
			t.Fatalf("policy group %q exits = %#v, want %#v", id, group.Exits, wantExits)
		}
	}
	if config.Packs[0].ID != "v2fly_dlc_category_ads_all" || config.Packs[0].Target != "REJECT" {
		t.Fatalf("first pack = %+v, want ads reject first", config.Packs[0])
	}
	if got := config.Packs[len(config.Packs)-2].Target; got != "🧭 漏网之鱼" {
		t.Fatalf("geolocation fallback target = %q, want 🧭 漏网之鱼", got)
	}
}

func TestListTemplatesReadsDiskFiles(t *testing.T) {
	dir := writeTemplateFixture(t)
	templates, err := List(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(templates) != 2 {
		t.Fatalf("templates = %+v, want two", templates)
	}
	if templates[0].Path == "" || templates[1].Path == "" {
		t.Fatalf("templates = %+v, want disk paths", templates)
	}
}

func writeTemplateFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writePolicyTemplateTestFile(t, filepath.Join(dir, "minimal.yaml"), `id: minimal
name: Minimal
description: Minimal policy.
config:
  version: 1
  policy_template: minimal
  proxy_groups: {}
  packs: []
`)
	writePolicyTemplateTestFile(t, filepath.Join(dir, "localclash-default.yaml"), `id: localclash-default
name: localClash Default
description: ACL4SSR-like default policy.
default: true
config:
  version: 1
  policy_template: localclash-default
  proxy_groups:
    AI:
      mode: auto
      match:
        type: name_regex
        pattern: .*
        min: 1
  packs:
    - id: v2fly_dlc_category_ads_all
      type: geosite
      target: REJECT
    - id: v2fly_dlc_openai
      type: geosite
      target: AI
`)
	return dir
}

func writePolicyTemplateTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
