package rules

import (
	"encoding/gob"
	"fmt"
	"os"
	"regexp"
	"strings"
)

type SubscriptionNodesListOptions struct {
	Subscription string
	Limit        int
}

type SubscriptionNodesSearchOptions struct {
	Subscription  string
	Query         string
	Patterns      []string
	CaseSensitive bool
	Limit         int
}

type SubscriptionNodeSample struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type SelectorSuggestion struct {
	Type     string `json:"type"`
	Pattern  string `json:"pattern"`
	Boundary string `json:"boundary"`
	Note     string `json:"note"`
}

type SubscriptionNodesResult struct {
	Subscription       string                   `json:"subscription"`
	MatchBasis         string                   `json:"match_basis"`
	Total              int                      `json:"total"`
	Returned           int                      `json:"returned"`
	Nodes              []SubscriptionNodeSample `json:"nodes"`
	SelectorSuggestion *SelectorSuggestion      `json:"selector_suggestion,omitempty"`
	Note               string                   `json:"note"`
}

type safeSubscriptionNode struct {
	Name string
	Type string
}

func ListSubscriptionNodes(opts SubscriptionNodesListOptions) (SubscriptionNodesResult, error) {
	subscription := defaultString(opts.Subscription, "subscription.gob")
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	nodes, err := loadSafeSubscriptionNodes(subscription)
	if err != nil {
		return SubscriptionNodesResult{}, err
	}
	samples := subscriptionNodeSamples(nodes)
	limited := limitSubscriptionNodeSamples(samples, limit)
	return SubscriptionNodesResult{
		Subscription: subscription,
		MatchBasis:   "subscription_proxy_name",
		Total:        len(samples),
		Returned:     len(limited),
		Nodes:        limited,
		Note:         subscriptionNodesNote(),
	}, nil
}

func SearchSubscriptionNodes(opts SubscriptionNodesSearchOptions) (SubscriptionNodesResult, error) {
	subscription := defaultString(opts.Subscription, "subscription.gob")
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if strings.TrimSpace(opts.Query) == "" && len(opts.Patterns) == 0 {
		return SubscriptionNodesResult{}, fmt.Errorf("query or patterns is required")
	}
	nodes, err := loadSafeSubscriptionNodes(subscription)
	if err != nil {
		return SubscriptionNodesResult{}, err
	}
	matchers, err := buildSubscriptionNodeMatchers(opts)
	if err != nil {
		return SubscriptionNodesResult{}, err
	}
	var matched []SubscriptionNodeSample
	seen := map[string]bool{}
	for _, node := range nodes {
		for _, matcher := range matchers {
			if matcher(node.Name) {
				if !seen[node.Name] {
					matched = append(matched, SubscriptionNodeSample{Name: node.Name, Type: node.Type})
					seen[node.Name] = true
				}
				break
			}
		}
	}
	limited := limitSubscriptionNodeSamples(matched, limit)
	return SubscriptionNodesResult{
		Subscription:       subscription,
		MatchBasis:         "subscription_proxy_name",
		Total:              len(matched),
		Returned:           len(limited),
		Nodes:              limited,
		SelectorSuggestion: selectorSuggestion(opts),
		Note:               subscriptionNodesNote(),
	}, nil
}

func loadSafeSubscriptionNodes(path string) ([]safeSubscriptionNode, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	gob.Register(map[string]any{})
	gob.Register([]any{})
	var artifact struct {
		Version int
		Data    map[string]any
		Raw     []byte
	}
	if err := gob.NewDecoder(file).Decode(&artifact); err != nil {
		return nil, err
	}
	if artifact.Version != 1 {
		return nil, fmt.Errorf("subscription artifact schema version mismatch: expected 1, got %d; run localclash subscriptions refresh", artifact.Version)
	}
	source := artifact.Data
	raw, ok := source["proxies"].([]any)
	if !ok {
		return nil, fmt.Errorf("subscription %q has no proxies", path)
	}
	nodes := make([]safeSubscriptionNode, 0, len(raw))
	for _, item := range raw {
		proxy, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("subscription %q contains an invalid proxy entry", path)
		}
		name, ok := proxy["name"].(string)
		if !ok || name == "" {
			return nil, fmt.Errorf("subscription %q contains a proxy without name", path)
		}
		nodeType, _ := proxy["type"].(string)
		nodes = append(nodes, safeSubscriptionNode{Name: name, Type: nodeType})
	}
	return nodes, nil
}

func subscriptionNodeSamples(nodes []safeSubscriptionNode) []SubscriptionNodeSample {
	out := make([]SubscriptionNodeSample, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, SubscriptionNodeSample{Name: node.Name, Type: node.Type})
	}
	return out
}

func buildSubscriptionNodeMatchers(opts SubscriptionNodesSearchOptions) ([]func(string) bool, error) {
	var matchers []func(string) bool
	if strings.TrimSpace(opts.Query) != "" {
		query := strings.TrimSpace(opts.Query)
		if !opts.CaseSensitive {
			query = strings.ToLower(query)
		}
		matchers = append(matchers, func(name string) bool {
			if !opts.CaseSensitive {
				name = strings.ToLower(name)
			}
			return strings.Contains(name, query)
		})
	}
	for _, pattern := range opts.Patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if !opts.CaseSensitive {
			pattern = "(?i)" + pattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("pattern %q is invalid: %w", pattern, err)
		}
		matchers = append(matchers, re.MatchString)
	}
	if len(matchers) == 0 {
		return nil, fmt.Errorf("query or patterns is required")
	}
	return matchers, nil
}

func subscriptionNodesNote() string {
	return "Matches are based only on subscription proxy names and do not verify network egress location."
}

func selectorSuggestion(opts SubscriptionNodesSearchOptions) *SelectorSuggestion {
	patterns := cleanPatterns(opts.Patterns)
	pattern := ""
	if len(patterns) > 0 {
		pattern = strings.Join(patterns, "|")
	} else if strings.TrimSpace(opts.Query) != "" {
		pattern = regexp.QuoteMeta(strings.TrimSpace(opts.Query))
	}
	if pattern == "" {
		return nil
	}
	return &SelectorSuggestion{
		Type:     "name_regex",
		Pattern:  pattern,
		Boundary: "name_based_hint_only",
		Note:     subscriptionNodesNote(),
	}
}

func cleanPatterns(patterns []string) []string {
	var out []string
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			out = append(out, pattern)
		}
	}
	return out
}

func limitSubscriptionNodeSamples(samples []SubscriptionNodeSample, limit int) []SubscriptionNodeSample {
	if limit < 0 {
		limit = 0
	}
	if len(samples) > limit {
		samples = samples[:limit]
	}
	out := make([]SubscriptionNodeSample, 0, len(samples))
	return append(out, samples...)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
