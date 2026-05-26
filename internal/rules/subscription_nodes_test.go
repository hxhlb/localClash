package rules

import (
	"encoding/gob"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestListSubscriptionNodesReturnsSafeSummaries(t *testing.T) {
	subscription := writeSubscriptionNodesFixture(t)

	result, err := ListSubscriptionNodes(SubscriptionNodesListOptions{Subscription: subscription, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.MatchBasis != "subscription_proxy_name" {
		t.Fatalf("match basis = %q, want subscription_proxy_name", result.MatchBasis)
	}
	if result.Total != 4 || result.Returned != 2 || len(result.Nodes) != 2 {
		t.Fatalf("result = %+v, want total 4 returned 2", result)
	}
	if result.Nodes[0].Name != "JP Tokyo 01" || result.Nodes[0].Type != "ss" {
		t.Fatalf("first node = %+v, want safe name/type", result.Nodes[0])
	}
	assertNoCredentialLeak(t, result)
}

func TestSearchSubscriptionNodesMatchesQueryByNameOnly(t *testing.T) {
	subscription := writeSubscriptionNodesFixture(t)

	result, err := SearchSubscriptionNodes(SubscriptionNodesSearchOptions{Subscription: subscription, Query: "香港"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || len(result.Nodes) != 1 {
		t.Fatalf("result = %+v, want one Hong Kong name match", result)
	}
	if result.Nodes[0].Name != "🇭🇰香港 01 | HK" || result.Nodes[0].Type != "ss" {
		t.Fatalf("node = %+v, want HK safe summary", result.Nodes[0])
	}
	if !strings.Contains(result.Note, "do not verify network egress location") {
		t.Fatalf("note = %q, want egress boundary", result.Note)
	}
	assertNoCredentialLeak(t, result)
}

func TestSearchSubscriptionNodesMatchesPatterns(t *testing.T) {
	subscription := writeSubscriptionNodesFixture(t)

	result, err := SearchSubscriptionNodes(SubscriptionNodesSearchOptions{
		Subscription: subscription,
		Patterns:     []string{`\bHK\b`},
		Limit:        1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Total != 1 || result.Returned != 1 || len(result.Nodes) != 1 {
		t.Fatalf("result = %+v, want one limited pattern match", result)
	}
	assertNoCredentialLeak(t, result)
}

func TestSearchSubscriptionNodesRejectsInvalidPattern(t *testing.T) {
	subscription := writeSubscriptionNodesFixture(t)

	if _, err := SearchSubscriptionNodes(SubscriptionNodesSearchOptions{Subscription: subscription, Patterns: []string{"["}}); err == nil {
		t.Fatal("expected invalid pattern error")
	}
}

func TestSearchSubscriptionNodesRequiresQueryOrPattern(t *testing.T) {
	subscription := writeSubscriptionNodesFixture(t)

	if _, err := SearchSubscriptionNodes(SubscriptionNodesSearchOptions{Subscription: subscription}); err == nil {
		t.Fatal("expected missing query/pattern error")
	}
}

func writeSubscriptionNodesFixture(t *testing.T) string {
	t.Helper()
	subscription := filepath.Join(t.TempDir(), "subscription.gob")
	raw := []byte(`
proxies:
  - name: JP Tokyo 01
    type: ss
    server: jp.example.com
    password: secret-jp
  - name: SG Singapore 01
    type: trojan
    server: sg.example.com
    password: secret-sg
  - name: 🇭🇰香港 01 | HK
    type: ss
    server: hk.example.com
    password: secret-hk
  - name: USA 01
    type: vmess
    server: us.example.com
    uuid: secret-us
`)
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(subscription, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	gob.Register(map[string]any{})
	gob.Register([]any{})
	encodeErr := gob.NewEncoder(file).Encode(struct {
		Version int
		Data    map[string]any
		Raw     []byte
	}{Version: 1, Data: doc, Raw: raw})
	closeErr := file.Close()
	if encodeErr != nil {
		t.Fatal(encodeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	return subscription
}

func assertNoCredentialLeak(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret", "server", "uuid", "password", "jp.example.com", "sg.example.com", "hk.example.com", "us.example.com"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("result leaked %q in %s", secret, data)
		}
	}
}
