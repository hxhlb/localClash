package rules

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListVirtualNodesCountsLabels(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	result, err := ListVirtualNodes(VirtualNodesListOptions{Selection: selection, Subscription: subscription, IncludeEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	counts := virtualNodeCounts(result.VirtualNodes)
	if counts["JP"] != 2 || counts["SG"] != 1 || counts["EMPTY"] != 0 {
		t.Fatalf("counts = %+v, want JP=2 SG=1 EMPTY=0", counts)
	}
	if result.SelectionSource != "user" {
		t.Fatalf("selection source = %q, want user", result.SelectionSource)
	}
}

func TestListVirtualNodesSkipsEmptyByDefault(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	result, err := ListVirtualNodes(VirtualNodesListOptions{Selection: selection, Subscription: subscription})
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range result.VirtualNodes {
		if node.ID == "EMPTY" {
			t.Fatalf("EMPTY should not be returned when include_empty=false: %+v", result.VirtualNodes)
		}
	}
}

func TestListVirtualNodesIncludesEmpty(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	result, err := ListVirtualNodes(VirtualNodesListOptions{Selection: selection, Subscription: subscription, IncludeEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	if virtualNodeCounts(result.VirtualNodes)["EMPTY"] != 0 {
		t.Fatalf("result = %+v, want EMPTY node_count=0", result.VirtualNodes)
	}
}

func TestListVirtualNodesSampleLimit(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	result, err := ListVirtualNodes(VirtualNodesListOptions{Selection: selection, Subscription: subscription, SampleLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range result.VirtualNodes {
		if node.ID == "JP" && len(node.Samples) != 1 {
			t.Fatalf("JP samples = %+v, want 1 sample", node.Samples)
		}
	}
}

func TestListVirtualNodesDoesNotLeakCredentials(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	result, err := ListVirtualNodes(VirtualNodesListOptions{Selection: selection, Subscription: subscription, IncludeEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	assertNoCredentialLeak(t, result)
}

func TestGetVirtualNodeReturnsCandidates(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	result, err := GetVirtualNode(VirtualNodesGetOptions{Selection: selection, Subscription: subscription, ID: "SG"})
	if err != nil {
		t.Fatal(err)
	}
	node := result.VirtualNode
	if node.ID != "SG" || node.NodeCount != 1 || len(node.Nodes) != 1 {
		t.Fatalf("node = %+v, want one SG candidate", node)
	}
	if node.Nodes[0].Name != "SG 01" || node.Nodes[0].Type != "trojan" {
		t.Fatalf("SG candidate = %+v, want safe name/type", node.Nodes[0])
	}
}

func TestGetVirtualNodeUnknownIDReturnsError(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	if _, err := GetVirtualNode(VirtualNodesGetOptions{Selection: selection, Subscription: subscription, ID: "MISSING"}); err == nil {
		t.Fatal("expected unknown virtual node error")
	}
}

func TestGetVirtualNodeLimit(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	result, err := GetVirtualNode(VirtualNodesGetOptions{Selection: selection, Subscription: subscription, ID: "JP", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.VirtualNode.NodeCount != 2 || len(result.VirtualNode.Nodes) != 1 {
		t.Fatalf("node = %+v, want count 2 and one returned node", result.VirtualNode)
	}
}

func TestGetVirtualNodeNoMatchesReturnsZero(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	result, err := GetVirtualNode(VirtualNodesGetOptions{Selection: selection, Subscription: subscription, ID: "EMPTY"})
	if err != nil {
		t.Fatal(err)
	}
	if result.VirtualNode.NodeCount != 0 || len(result.VirtualNode.Nodes) != 0 {
		t.Fatalf("node = %+v, want empty result without error", result.VirtualNode)
	}
}

func TestGetVirtualNodeDoesNotLeakCredentials(t *testing.T) {
	selection, subscription := writeVirtualNodesFixture(t)

	result, err := GetVirtualNode(VirtualNodesGetOptions{Selection: selection, Subscription: subscription, ID: "JP"})
	if err != nil {
		t.Fatal(err)
	}
	assertNoCredentialLeak(t, result)
}

func writeVirtualNodesFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	selection := filepath.Join(dir, "localclash-packs.yaml")
	subscription := filepath.Join(dir, "subscription.yaml")
	if err := os.WriteFile(selection, []byte(`
version: 1
node_labels:
  EMPTY:
    name: Empty
    match:
      - "(?i)empty"
  JP:
    name: Japan
    match:
      - "(?i)jp|japan|日本|東京|大阪"
  SG:
    name: Singapore
    match:
      - "(?i)sg|singapore|新加坡"
virtual_targets: {}
enabled_packs: []
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(subscription, []byte(`
proxies:
  - name: JP Tokyo 01
    type: ss
    server: 203.0.113.10
    password: super-secret
  - name: 日本 02
    type: vmess
    uuid: private-uuid
  - name: SG 01
    type: trojan
    server: sg.example.com
    password: private-password
  - name: HK 01
    type: ss
    server: hk.example.com
    password: should-not-leak
`), 0o644); err != nil {
		t.Fatal(err)
	}
	return selection, subscription
}

func virtualNodeCounts(nodes []VirtualNodeSummary) map[string]int {
	out := map[string]int{}
	for _, node := range nodes {
		out[node.ID] = node.NodeCount
	}
	return out
}

func assertNoCredentialLeak(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, banned := range []string{"203.0.113.10", "super-secret", "private-uuid", "private-password", "should-not-leak", "server", "password", "uuid"} {
		if strings.Contains(text, banned) {
			t.Fatalf("virtual node result leaked %q in %s", banned, text)
		}
	}
}
