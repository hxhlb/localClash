package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type VirtualNodesListOptions struct {
	Subscription string
	Selection    string
	IncludeEmpty bool
	SampleLimit  int
}

type VirtualNodesGetOptions struct {
	ID           string
	Subscription string
	Selection    string
	Limit        int
}

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

type VirtualNodesListResult struct {
	Subscription    string               `json:"subscription"`
	Selection       string               `json:"selection"`
	SelectionSource string               `json:"selection_source"`
	Total           int                  `json:"total"`
	VirtualNodes    []VirtualNodeSummary `json:"virtual_nodes"`
}

type VirtualNodeSummary struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Match     []string            `json:"match"`
	NodeCount int                 `json:"node_count"`
	Samples   []VirtualNodeSample `json:"samples"`
}

type VirtualNodesGetResult struct {
	VirtualNode VirtualNodeDetail `json:"virtual_node"`
}

type VirtualNodeDetail struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Match     []string            `json:"match"`
	NodeCount int                 `json:"node_count"`
	Nodes     []VirtualNodeSample `json:"nodes"`
}

type VirtualNodeSample struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

type SubscriptionNodesResult struct {
	Subscription string              `json:"subscription"`
	MatchBasis   string              `json:"match_basis"`
	Total        int                 `json:"total"`
	Returned     int                 `json:"returned"`
	Nodes        []VirtualNodeSample `json:"nodes"`
	Note         string              `json:"note"`
}

type safeSubscriptionNode struct {
	Name string
	Type string
}

type resolvedSelection struct {
	Path   string
	Source string
	Value  Selection
}

func ListVirtualNodes(opts VirtualNodesListOptions) (VirtualNodesListResult, error) {
	subscription := defaultString(opts.Subscription, "subscription.yaml")
	sampleLimit := opts.SampleLimit
	if sampleLimit <= 0 {
		sampleLimit = 5
	}
	selection, err := loadSelectionForVirtualNodes(opts.Selection)
	if err != nil {
		return VirtualNodesListResult{}, err
	}
	nodes, err := loadSafeSubscriptionNodes(subscription)
	if err != nil {
		return VirtualNodesListResult{}, err
	}

	labelIDs := sortedNodeLabelIDs(selection.Value.NodeLabels)
	out := VirtualNodesListResult{
		Subscription:    subscription,
		Selection:       selection.Path,
		SelectionSource: selection.Source,
		VirtualNodes:    []VirtualNodeSummary{},
	}
	for _, id := range labelIDs {
		label := selection.Value.NodeLabels[id]
		matched, err := matchVirtualNodeCandidates(nodes, label)
		if err != nil {
			return VirtualNodesListResult{}, fmt.Errorf("virtual node %q: %w", id, err)
		}
		if len(matched) == 0 && !opts.IncludeEmpty {
			continue
		}
		out.VirtualNodes = append(out.VirtualNodes, VirtualNodeSummary{
			ID:        id,
			Name:      nodeLabelName(id, label),
			Match:     append([]string(nil), label.Match...),
			NodeCount: len(matched),
			Samples:   limitVirtualNodeSamples(matched, sampleLimit),
		})
	}
	out.Total = len(out.VirtualNodes)
	return out, nil
}

func GetVirtualNode(opts VirtualNodesGetOptions) (VirtualNodesGetResult, error) {
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		return VirtualNodesGetResult{}, fmt.Errorf("virtual node id is required")
	}
	subscription := defaultString(opts.Subscription, "subscription.yaml")
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	selection, err := loadSelectionForVirtualNodes(opts.Selection)
	if err != nil {
		return VirtualNodesGetResult{}, err
	}
	label, ok := selection.Value.NodeLabels[id]
	if !ok {
		return VirtualNodesGetResult{}, fmt.Errorf("virtual node %q not found in selection %q", id, selection.Path)
	}
	nodes, err := loadSafeSubscriptionNodes(subscription)
	if err != nil {
		return VirtualNodesGetResult{}, err
	}
	matched, err := matchVirtualNodeCandidates(nodes, label)
	if err != nil {
		return VirtualNodesGetResult{}, fmt.Errorf("virtual node %q: %w", id, err)
	}
	return VirtualNodesGetResult{
		VirtualNode: VirtualNodeDetail{
			ID:        id,
			Name:      nodeLabelName(id, label),
			Match:     append([]string(nil), label.Match...),
			NodeCount: len(matched),
			Nodes:     limitVirtualNodeSamples(matched, limit),
		},
	}, nil
}

func ListSubscriptionNodes(opts SubscriptionNodesListOptions) (SubscriptionNodesResult, error) {
	subscription := defaultString(opts.Subscription, "subscription.yaml")
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	nodes, err := loadSafeSubscriptionNodes(subscription)
	if err != nil {
		return SubscriptionNodesResult{}, err
	}
	samples := subscriptionNodeSamples(nodes)
	limited := limitVirtualNodeSamples(samples, limit)
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
	subscription := defaultString(opts.Subscription, "subscription.yaml")
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
	var matched []VirtualNodeSample
	seen := map[string]bool{}
	for _, node := range nodes {
		for _, matcher := range matchers {
			if matcher(node.Name) {
				if !seen[node.Name] {
					matched = append(matched, VirtualNodeSample{Name: node.Name, Type: node.Type})
					seen[node.Name] = true
				}
				break
			}
		}
	}
	limited := limitVirtualNodeSamples(matched, limit)
	return SubscriptionNodesResult{
		Subscription: subscription,
		MatchBasis:   "subscription_proxy_name",
		Total:        len(matched),
		Returned:     len(limited),
		Nodes:        limited,
		Note:         subscriptionNodesNote(),
	}, nil
}

func loadSelectionForVirtualNodes(path string) (resolvedSelection, error) {
	if strings.TrimSpace(path) != "" {
		selection, err := LoadSelection(path)
		if err != nil {
			return resolvedSelection{}, err
		}
		return resolvedSelection{Path: path, Source: selectionSource(path), Value: selection}, nil
	}

	userPath := "localclash-packs.yaml"
	selection, err := LoadSelection(userPath)
	if err == nil {
		return resolvedSelection{Path: userPath, Source: "user", Value: selection}, nil
	}
	if !os.IsNotExist(err) {
		return resolvedSelection{}, err
	}

	examplePath := "localclash-packs.yaml.example"
	selection, err = LoadSelection(examplePath)
	if err != nil {
		return resolvedSelection{}, err
	}
	return resolvedSelection{Path: examplePath, Source: "example", Value: selection}, nil
}

func selectionSource(path string) string {
	if filepath.Base(path) == "localclash-packs.yaml.example" {
		return "example"
	}
	return "user"
}

func loadSafeSubscriptionNodes(path string) ([]safeSubscriptionNode, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var source map[string]any
	if err := yaml.Unmarshal(data, &source); err != nil {
		return nil, err
	}
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

func subscriptionNodeSamples(nodes []safeSubscriptionNode) []VirtualNodeSample {
	out := make([]VirtualNodeSample, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, VirtualNodeSample{Name: node.Name, Type: node.Type})
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

func matchVirtualNodeCandidates(nodes []safeSubscriptionNode, label NodeLabel) ([]VirtualNodeSample, error) {
	compiled := make([]*regexp.Regexp, 0, len(label.Match))
	for _, pattern := range label.Match {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("pattern %q is invalid: %w", pattern, err)
		}
		compiled = append(compiled, re)
	}

	var matched []VirtualNodeSample
	seen := map[string]bool{}
	for _, node := range nodes {
		for _, re := range compiled {
			if re.MatchString(node.Name) {
				if !seen[node.Name] {
					matched = append(matched, VirtualNodeSample{Name: node.Name, Type: node.Type})
					seen[node.Name] = true
				}
				break
			}
		}
	}
	return matched, nil
}

func sortedNodeLabelIDs(labels map[string]NodeLabel) []string {
	ids := make([]string, 0, len(labels))
	for id := range labels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func nodeLabelName(id string, label NodeLabel) string {
	if strings.TrimSpace(label.Name) != "" {
		return label.Name
	}
	return id
}

func limitVirtualNodeSamples(samples []VirtualNodeSample, limit int) []VirtualNodeSample {
	if limit < 0 {
		limit = 0
	}
	if len(samples) > limit {
		samples = samples[:limit]
	}
	out := make([]VirtualNodeSample, 0, len(samples))
	return append(out, samples...)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
