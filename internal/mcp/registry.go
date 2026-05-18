package mcp

import (
	"encoding/json"
	"io"
	"sort"
)

type SafetyLevel string

const (
	SafeRead        SafetyLevel = "safe_read"
	SafeWrite       SafetyLevel = "safe_write"
	ConfirmRequired SafetyLevel = "confirm_required"
	HighRisk        SafetyLevel = "high_risk"
)

type Tool struct {
	Name        string      `json:"name"`
	SafetyLevel SafetyLevel `json:"safety_level"`
	Description string      `json:"description"`
}

type ListedTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	SafetyLevel SafetyLevel    `json:"safety_level"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

type ToolSummary struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	SafetyLevel SafetyLevel `json:"safety_level"`
}

type ToolsListResult struct {
	Tools []ToolSummary `json:"tools"`
	Count int           `json:"count"`
}

func Registry() []Tool {
	tools := []Tool{
		{Name: "config_status", SafetyLevel: SafeRead, Description: "Inspect localClash config status: source-of-truth localclash.yaml, generated/mihomo.yaml build artifact, render readiness, and pending patches. Use this before claiming what routing is configured; rules_sample fields are truncated samples."},
		{Name: "doctor", SafetyLevel: SafeRead, Description: "Run read-only localClash diagnostics."},
		{Name: "environment_inspect", SafetyLevel: SafeRead, Description: "Inspect host, network capability evidence, and localClash state without exposing credentials."},
		{Name: "nl_file", SafetyLevel: SafeRead, Description: "Read a repository-local text file with nl-style stable line numbers for follow-up sed_file edits."},
		{Name: "pack_rules_query", SafetyLevel: SafeRead, Description: "Search locally cached pack provider rules for a domain or keyword. Does not download provider rules; call pack_rules_prefetch first when cache coverage is incomplete."},
		{Name: "packs_get", SafetyLevel: SafeRead, Description: "Read details for one generated rule pack cache entry. The target field is a catalog default/recommended target, not proof the pack is active; use config_status for active routing."},
		{Name: "packs_list", SafetyLevel: SafeRead, Description: "List and filter available rule pack catalog entries. This is discovery only: target is catalog default/recommended target, not current active configuration."},
		{Name: "subscription_nodes_list", SafetyLevel: SafeRead, Description: "List safe subscription proxy name/type summaries without exposing connection credentials."},
		{Name: "subscription_nodes_search", SafetyLevel: SafeRead, Description: "Search subscription proxy names and return safe name/type summaries; does not verify network egress location."},
		{Name: "runtime_profile_status", SafetyLevel: SafeRead, Description: "Inspect the active Mihomo runtime profile and its safe summary without exposing proxy credentials."},
		{Name: "subscriptions_status", SafetyLevel: SafeRead, Description: "Inspect configured subscription sources and local effective subscription state."},
		{Name: "runtime_status", SafetyLevel: SafeRead, Description: "Inspect Mihomo runtime status from the local PID file without changing runtime state."},
		{Name: "router_takeover_status", SafetyLevel: SafeRead, Description: "Inspect localClash-owned OpenWrt router takeover runtime state: runtime profile, Mihomo runtime, fw4/nft chains, DNS hijack, fwmark route, and TUN device."},
		{Name: "tools_list", SafetyLevel: SafeRead, Description: "List localClash MCP tools as ordinary tool output for clients that do not expose MCP registry introspection to the model."},
		{Name: "config_patch_apply", SafetyLevel: SafeWrite, Description: "Apply a reviewed config patch by writing localclash.yaml, deriving localclash-packs.yaml, and regenerating generated/mihomo.yaml without starting the runtime. Use the exact patch_id returned by config_patch_create."},
		{Name: "config_patch_create", SafetyLevel: SafeWrite, Description: "Create a reviewable config patch and candidate Mihomo config from proxy groups, packs, and custom rules. It does not modify active localclash.yaml or generated/mihomo.yaml; apply the returned patch_id only after review."},
		{Name: "config_render", SafetyLevel: SafeWrite, Description: "Render generated/mihomo.yaml from the current durable localclash.yaml source of truth, subscription, policy, and runtime profile. Does not read patches and does not start runtime."},
		{Name: "custom_rules_build", SafetyLevel: SafeWrite, Description: "Build and validate user custom routing rules for domains or CIDRs before adding them to a config patch."},
		{Name: "pack_rules_prefetch", SafetyLevel: SafeWrite, Description: "Download provider rules for selected packs into local provider-cache so pack_rules_query can search them locally."},
		{Name: "pack_rules_read", SafetyLevel: SafeWrite, Description: "Read provider rules for one pack by id, downloading missing provider-cache entries for that pack only."},
		{Name: "proxy_group_build", SafetyLevel: SafeWrite, Description: "Build and validate a reusable proxy group target from subscription node selectors or exact nodes. This does not persist state; copy the returned proxy_group into config_patch_create.overlay.proxy_groups when a patch should use it."},
		{Name: "rule_provider_build", SafetyLevel: SafeWrite, Description: "Build and validate a reusable external rule-provider intent for user-supplied Mihomo rule-provider URLs before adding it to config_patch_create.overlay.rule_providers."},
		{Name: "runtime_profile_configure", SafetyLevel: SafeWrite, Description: "Switch the active Mihomo runtime mode and/or core by writing localclash-runtime.yaml. Profile contents live in editable profiles/normal.yaml and profiles/router.yaml, copied from .default.yaml files on first use. This does not start or restart Mihomo."},
		{Name: "subscriptions_configure", SafetyLevel: SafeWrite, Description: "Write local subscription source configuration without refreshing."},
		{Name: "subscriptions_refresh", SafetyLevel: SafeWrite, Description: "Refresh configured subscription sources into local artifacts and effective subscription.yaml."},
		{Name: "run_runtime", SafetyLevel: ConfirmRequired, Description: "Start the Mihomo runtime from generated config, rendering generated/mihomo.yaml first when the effective subscription is available but the generated config is missing. Requires external Agent/MCP client confirmation; starting or restarting the proxy runtime may temporarily interrupt network connectivity, and the Agent itself may be disconnected if it depends on the current network/proxy path."},
		{Name: "restart_runtime", SafetyLevel: ConfirmRequired, Description: "Atomically validate/render config, stop the current Mihomo runtime if needed, and start it again in one confirmed call. Use this instead of stop_runtime then run_runtime when the Agent may depend on the proxy path."},
		{Name: "router_takeover_apply", SafetyLevel: ConfirmRequired, Description: "Apply localClash-owned OpenWrt router takeover runtime rules for router profile mode. Uses localClash router redir-host-mix behavior: TCP redir-host, DNS hijack, fwmark route, and TUN forwarding. Does not persist firewall config; call only after run_runtime or restart_runtime and user confirmation."},
		{Name: "router_takeover_stop", SafetyLevel: ConfirmRequired, Description: "Remove localClash-owned OpenWrt router takeover runtime rules without stopping Mihomo. This changes firewall, DNS, and policy-routing runtime state and requires user confirmation."},
		{Name: "sed_file", SafetyLevel: SafeWrite, Description: "Apply sed-style repository-local text edits with dry-run diff output. Defaults to dry_run=true."},
		{Name: "stop_runtime", SafetyLevel: ConfirmRequired, Description: "Stop the Mihomo runtime recorded by the local PID file. Refuses by default when router takeover is effective because router traffic still depends on Mihomo; call router_takeover_stop first or pass force=true only after explicit user confirmation."},
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

func WriteRegistry(w io.Writer) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(Registry())
}

func ListedTools() []ListedTool {
	tools := Registry()
	out := make([]ListedTool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, ListedTool{
			Name:        tool.Name,
			Description: tool.Description,
			SafetyLevel: tool.SafetyLevel,
			InputSchema: inputSchemaForTool(tool.Name),
			Annotations: map[string]any{
				"safety_level": tool.SafetyLevel,
			},
		})
	}
	return out
}

func ToolSummaries() ToolsListResult {
	tools := Registry()
	summaries := make([]ToolSummary, 0, len(tools))
	for _, tool := range tools {
		summaries = append(summaries, ToolSummary{
			Name:        tool.Name,
			Description: tool.Description,
			SafetyLevel: tool.SafetyLevel,
		})
	}
	return ToolsListResult{
		Tools: summaries,
		Count: len(summaries),
	}
}

func inputSchemaForTool(name string) map[string]any {
	switch name {
	case "tools_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
	case "environment_inspect":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties":           map[string]any{},
		}
	case "config_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":               map[string]any{"type": "string", "description": "Durable localClash source-of-truth config path. Defaults to localclash.yaml."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
				"policy":               map[string]any{"type": "string", "description": "Policy YAML path. Defaults to policies/loyalsoldier.yaml."},
				"mode":                 map[string]any{"type": "string", "description": "Policy render mode. Defaults to the policy default."},
				"rules_cache":          map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_profile":      map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.yaml."},
				"selection":            map[string]any{"type": "string", "description": "Derived packs selection path. Defaults to localclash-packs.yaml."},
				"output":               map[string]any{"type": "string", "description": "Generated Mihomo config path. Defaults to generated/mihomo.yaml."},
				"patches_dir":          map[string]any{"type": "string", "description": "Review patch artifact root. Defaults to .runtime/patches."},
				"limit":                map[string]any{"type": "integer", "minimum": 1, "description": "Maximum summary entries per section. Defaults to 20."},
			},
		}
	case "config_render":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":               map[string]any{"type": "string", "description": "Durable localClash source-of-truth config path. Defaults to localclash.yaml."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
				"policy":               map[string]any{"type": "string", "description": "Policy YAML path. Defaults to policies/loyalsoldier.yaml."},
				"mode":                 map[string]any{"type": "string", "description": "Policy render mode. Defaults to the policy default."},
				"rules_cache":          map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_profile":      map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.yaml."},
				"selection":            map[string]any{"type": "string", "description": "Derived packs selection path. Defaults to localclash-packs.yaml."},
				"output":               map[string]any{"type": "string", "description": "Generated Mihomo config path. Defaults to generated/mihomo.yaml."},
				"force":                map[string]any{"type": "boolean", "description": "Overwrite generated output. Defaults to true because generated/mihomo.yaml is a build artifact."},
			},
		}
	case "config_patch_apply":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"patch_id":             map[string]any{"type": "string", "description": "Patch directory id returned by config_patch_create."},
				"patches_dir":          map[string]any{"type": "string", "description": "Patch artifact root. Defaults to .runtime/patches."},
				"summary_path":         map[string]any{"type": "string", "description": "Optional explicit summary.json path. Use patch_id for normal flows."},
				"config":               map[string]any{"type": "string", "description": "Persistent localClash config path. Defaults to localclash.yaml."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
				"policy":               map[string]any{"type": "string", "description": "Policy YAML path. Defaults to policies/loyalsoldier.yaml."},
				"mode":                 map[string]any{"type": "string", "description": "Policy render mode. Defaults to the policy default."},
				"rules_cache":          map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_profile":      map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.yaml."},
				"selection":            map[string]any{"type": "string", "description": "Persistent packs selection path. Defaults to localclash-packs.yaml."},
				"output":               map[string]any{"type": "string", "description": "Generated Mihomo config path. Defaults to generated/mihomo.yaml."},
				"backup_dir":           map[string]any{"type": "string", "description": "Backup root for overwritten local artifacts. Defaults to .runtime/backups/config-patch-apply."},
				"test":                 map[string]any{"type": "boolean", "description": "Run Mihomo config test before applying. Defaults to true."},
				"core":                 map[string]any{"type": "string", "description": "Mihomo core path for config test. Defaults to the active runtime profile core path."},
				"runtime_dir":          map[string]any{"type": "string", "description": "Mihomo work directory for config test. Defaults to .runtime/mihomo."},
			},
		}
	case "nl_file":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"path":        map[string]any{"type": "string", "description": "Repository-local text file path."},
				"start_line":  map[string]any{"type": "integer", "minimum": 1, "description": "First 1-based line to return. Defaults to 1."},
				"limit_lines": map[string]any{"type": "integer", "minimum": 1, "description": "Maximum number of lines to return. Defaults to 120."},
				"max_bytes":   map[string]any{"type": "integer", "minimum": 1, "description": "Maximum returned content bytes. Defaults to 65536."},
			},
			"required": []string{"path"},
		}
	case "sed_file":
		edit := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"op":         map[string]any{"type": "string", "enum": []string{"replace", "insert_before", "insert_after", "delete_range", "append"}},
				"old":        map[string]any{"type": "string", "description": "Exact text to replace for replace edits."},
				"new":        map[string]any{"type": "string", "description": "Replacement text for replace edits."},
				"count":      map[string]any{"type": "integer", "minimum": 1, "description": "Number of exact replacements. Defaults to 1."},
				"line":       map[string]any{"type": "integer", "minimum": 1, "description": "Target line for insert_before or insert_after."},
				"start_line": map[string]any{"type": "integer", "minimum": 1, "description": "First line for delete_range."},
				"end_line":   map[string]any{"type": "integer", "minimum": 1, "description": "Last line for delete_range."},
				"text":       map[string]any{"type": "string", "description": "Text for insert_before, insert_after, or append."},
			},
			"required": []string{"op"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"path":            map[string]any{"type": "string", "description": "Repository-local text file path."},
				"dry_run":         map[string]any{"type": "boolean", "description": "Return the diff without writing. Defaults to true."},
				"expected_sha256": map[string]any{"type": "string", "description": "Optional file sha256 from nl_file; rejects edits if the file changed."},
				"edits":           map[string]any{"type": "array", "items": edit, "description": "Ordered sed-style edits to apply."},
			},
			"required": []string{"path", "edits"},
		}
	case "proxy_group_build":
		matchIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"type":           map[string]any{"type": "string", "enum": []string{"name_regex"}, "description": "Selector type. name_regex matches subscription proxy names only."},
				"pattern":        map[string]any{"type": "string", "description": "Regular expression matched against subscription proxy names."},
				"source_ids":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional subscription source ids to constrain matches."},
				"min":            map[string]any{"type": "integer", "minimum": 0, "description": "Minimum required matches. Defaults to 1."},
				"max":            map[string]any{"type": "integer", "minimum": 0, "description": "Maximum matches to materialize. 0 means unlimited."},
				"case_sensitive": map[string]any{"type": "boolean", "description": "Whether pattern matching is case-sensitive. Defaults to false."},
			},
			"required": []string{"type", "pattern"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":                   map[string]any{"type": "string", "description": "Reusable proxy group id, for example TempLine or SteamHK."},
				"match":                matchIntent,
				"nodes":                map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Exact subscription proxy names for a user-specified line. Use either match or nodes, not both."},
				"mode":                 map[string]any{"type": "string", "enum": []string{"manual", "auto", "smart"}, "description": "Desired proxy-group mode. manual becomes select; auto becomes url-test on meta core and smart on smart core; smart explicitly requires the smart core."},
				"reason":               map[string]any{"type": "string", "description": "Short durable reason used if selector repair needs user involvement."},
				"boundary":             map[string]any{"type": "string", "description": "Boundary note, for example name_based_hint_only."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
			},
			"required": []string{"id", "mode"},
		}
	case "custom_rules_build":
		ruleIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"type":       map[string]any{"type": "string", "enum": []string{"domain", "domain_suffix", "ip_cidr", "ip_cidr6"}, "description": "Mihomo rule type to generate."},
				"value":      map[string]any{"type": "string", "description": "Domain, domain suffix, or CIDR value."},
				"no_resolve": map[string]any{"type": "boolean", "description": "Append no-resolve for IP CIDR rules when needed."},
			},
			"required": []string{"type", "value"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Stable custom rule id, for example huggingface_temp."},
				"target": map[string]any{"type": "string", "description": "Built-in target such as DIRECT/REJECT/PROXY, or a proxy group id built by proxy_group_build."},
				"reason": map[string]any{"type": "string", "description": "Short durable reason for this user rule."},
				"rules":  map[string]any{"type": "array", "items": ruleIntent, "description": "Rules that share the same target."},
			},
			"required": []string{"id", "target", "rules"},
		}
	case "rule_provider_build":
		return ruleProviderInputSchema("External rule-provider id, for example US-Proxy.")
	case "config_patch_create":
		matchIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"type":           map[string]any{"type": "string", "enum": []string{"name_regex"}, "description": "Selector type. name_regex matches subscription proxy names only."},
				"pattern":        map[string]any{"type": "string", "description": "Regular expression matched against subscription proxy names."},
				"source_ids":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional subscription source ids to constrain matches."},
				"min":            map[string]any{"type": "integer", "minimum": 0, "description": "Minimum required matches. Defaults to 1."},
				"max":            map[string]any{"type": "integer", "minimum": 0, "description": "Maximum matches to materialize. 0 means unlimited."},
				"case_sensitive": map[string]any{"type": "boolean", "description": "Whether pattern matching is case-sensitive. Defaults to false."},
			},
			"required": []string{"type", "pattern"},
		}
		packIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Pack id, for example blackmatrix7_OpenAI or sukkaw_ai_non_ip."},
				"target": map[string]any{"type": "string", "description": "Desired rule target, for example DIRECT, REJECT, PROXY, or AI."},
				"reason": map[string]any{"type": "string", "description": "Short durable reason for this pack routing choice."},
			},
			"required": []string{"id", "target"},
		}
		ruleIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"type":       map[string]any{"type": "string", "enum": []string{"domain", "domain_suffix", "ip_cidr", "ip_cidr6"}, "description": "Mihomo rule type to generate."},
				"value":      map[string]any{"type": "string", "description": "Domain, domain suffix, or CIDR value."},
				"no_resolve": map[string]any{"type": "boolean", "description": "Append no-resolve for IP CIDR rules when needed."},
			},
			"required": []string{"type", "value"},
		}
		customRuleIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":     map[string]any{"type": "string", "description": "Stable custom rule id, for example huggingface_temp."},
				"target": map[string]any{"type": "string", "description": "Built-in target such as DIRECT/REJECT/PROXY, or a proxy group id."},
				"reason": map[string]any{"type": "string", "description": "Short durable reason for this user rule."},
				"rules":  map[string]any{"type": "array", "items": ruleIntent, "description": "Rules that share the same target."},
			},
			"required": []string{"id", "target", "rules"},
		}
		ruleProviderIntent := ruleProviderInputSchema("External rule-provider id, for example US-Proxy.")
		proxyGroupIntent := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":       map[string]any{"type": "string", "description": "Proxy group id referenced by packs[].target, for example SteamHK."},
				"match":    matchIntent,
				"nodes":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Exact subscription proxy names for a user-specified line. Use either match or nodes, not both."},
				"mode":     map[string]any{"type": "string", "enum": []string{"manual", "auto", "smart"}, "description": "Desired proxy-group mode. manual becomes select; auto becomes url-test on meta core and smart on smart core; smart explicitly requires the smart core."},
				"reason":   map[string]any{"type": "string", "description": "Short durable reason used if selector repair needs Agent involvement."},
				"boundary": map[string]any{"type": "string", "description": "Boundary note, for example name_based_hint_only."},
			},
			"required": []string{"id", "mode"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"patch_name":           map[string]any{"type": "string", "description": "Human-readable patch slug prefix."},
				"subscription":         map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"policy":               map[string]any{"type": "string", "description": "Policy YAML path. Defaults to policies/loyalsoldier.yaml."},
				"mode":                 map[string]any{"type": "string", "description": "Policy render mode. Defaults to the policy default."},
				"rules_cache":          map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_profile":      map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.yaml."},
				"patches_dir":          map[string]any{"type": "string", "description": "Patch artifact root. Defaults to .runtime/patches."},
				"config":               map[string]any{"type": "string", "description": "Candidate localClash config filename in the patch. Defaults to localclash.yaml."},
				"subscription_config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"subscription_runtime": map[string]any{"type": "string", "description": "Per-source subscription artifact directory. Defaults to .runtime/subscriptions."},
				"test":                 map[string]any{"type": "boolean", "description": "Run Mihomo config test. Defaults to true."},
				"overlay": map[string]any{
					"type":                 "object",
					"description":          "Desired localClash overlay. If packs[].target, custom_rules[].target, or rule_providers[].target references a proxy group that is not already in durable localclash.yaml, include that proxy group in overlay.proxy_groups in this same call.",
					"additionalProperties": false,
					"properties": map[string]any{
						"packs":          map[string]any{"type": "array", "items": packIntent},
						"custom_rules":   map[string]any{"type": "array", "items": customRuleIntent},
						"rule_providers": map[string]any{"type": "array", "items": ruleProviderIntent, "description": "User-supplied external Mihomo rule-providers rendered as rule-providers plus RULE-SET rules."},
						"proxy_groups":   map[string]any{"type": "array", "items": proxyGroupIntent},
					},
				},
			},
			"required": []string{"overlay"},
		}
	case "run_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir": map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"core":        map[string]any{"type": "string", "description": "Mihomo core binary path. Defaults to the active runtime profile core path."},
				"foreground":  map[string]any{"type": "boolean", "description": "Foreground mode is not supported by MCP run_runtime; use CLI run for foreground execution."},
				"log_file":    map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
			},
		}
	case "restart_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir": map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"core":        map[string]any{"type": "string", "description": "Mihomo core binary path. Defaults to the active runtime profile core path."},
				"log_file":    map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
				"timeout_ms":  map[string]any{"type": "integer", "minimum": 0, "description": "Milliseconds to wait after SIGTERM before reporting timeout. Defaults to 5000."},
				"force":       map[string]any{"type": "boolean", "description": "Send SIGKILL if the runtime does not exit before timeout. Defaults to false."},
			},
		}
	case "runtime_profile_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config": map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.yaml."},
			},
		}
	case "runtime_profile_configure":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"mode":   map[string]any{"type": "string", "enum": []string{"normal", "router"}, "description": "Runtime mode to activate."},
				"core":   map[string]any{"type": "string", "enum": []string{"meta", "smart"}, "description": "Runtime core flavor to activate."},
				"config": map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.yaml."},
			},
		}
	case "runtime_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir": map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"log_file":    map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
			},
		}
	case "router_takeover_status", "router_takeover_apply", "router_takeover_stop":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"runtime_profile": map[string]any{"type": "string", "description": "Runtime profile YAML path. Defaults to localclash-runtime.yaml."},
				"config":          map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir":     map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"log_file":        map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
				"state_dir":       map[string]any{"type": "string", "description": "localClash router takeover runtime state directory. Defaults to /tmp/localclash/router-takeover so reboot clears it."},
				"dns_port":        map[string]any{"type": "integer", "minimum": 1, "description": "Mihomo DNS listen port. Defaults to the router profile DNS listen port or 7874."},
				"redir_port":      map[string]any{"type": "integer", "minimum": 1, "description": "Mihomo redir-port. Defaults to the router profile redir-port or 7892."},
				"tun_device":      map[string]any{"type": "string", "description": "Mihomo TUN device name. Defaults to the router profile TUN device or utun."},
				"dry_run":         map[string]any{"type": "boolean", "description": "Return the shell script without applying changes. Supported by router_takeover_apply and router_takeover_stop."},
			},
		}
	case "stop_runtime":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"runtime_profile": map[string]any{"type": "string", "description": "Runtime profile YAML path used to detect router takeover. Defaults to localclash-runtime.yaml."},
				"config":          map[string]any{"type": "string", "description": "Mihomo generated config path. Defaults to generated/mihomo.yaml."},
				"runtime_dir":     map[string]any{"type": "string", "description": "Mihomo runtime data directory. Defaults to .runtime/mihomo."},
				"log_file":        map[string]any{"type": "string", "description": "Runtime log file. Defaults to .runtime/mihomo/mihomo.log."},
				"state_dir":       map[string]any{"type": "string", "description": "localClash router takeover runtime state directory used for takeover detection. Defaults to /tmp/localclash/router-takeover."},
				"dns_port":        map[string]any{"type": "integer", "minimum": 1, "description": "Mihomo DNS listen port used for takeover detection. Defaults to router profile DNS listen port or 7874."},
				"redir_port":      map[string]any{"type": "integer", "minimum": 1, "description": "Mihomo redir-port used for takeover detection. Defaults to router profile redir-port or 7892."},
				"tun_device":      map[string]any{"type": "string", "description": "Mihomo TUN device used for takeover detection. Defaults to router profile TUN device or utun."},
				"timeout_ms":      map[string]any{"type": "integer", "minimum": 0, "description": "Milliseconds to wait after SIGTERM before reporting timeout. Defaults to 5000."},
				"force":           map[string]any{"type": "boolean", "description": "Bypass the active router takeover guard and send SIGKILL if the runtime does not exit before timeout. Defaults to false."},
			},
		}
	case "subscriptions_status":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":      map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"merged":      map[string]any{"type": "string", "description": "Effective merged subscription path. Defaults to subscription.yaml."},
				"runtime_dir": map[string]any{"type": "string", "description": "Runtime source artifact directory. Defaults to .runtime/subscriptions."},
			},
		}
	case "subscription_nodes_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"subscription": map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"limit":        map[string]any{"type": "integer", "minimum": 0, "description": "Maximum returned proxy summaries. Defaults to 100."},
			},
		}
	case "subscription_nodes_search":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"subscription":   map[string]any{"type": "string", "description": "Subscription YAML path. Defaults to subscription.yaml."},
				"query":          map[string]any{"type": "string", "description": "Literal substring to match against subscription proxy names."},
				"patterns":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Regular expressions to match against subscription proxy names."},
				"case_sensitive": map[string]any{"type": "boolean", "description": "Use case-sensitive query and pattern matching. Defaults to false."},
				"limit":          map[string]any{"type": "integer", "minimum": 0, "description": "Maximum returned proxy summaries. Defaults to 50."},
			},
		}
	case "subscriptions_configure":
		source := map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":  map[string]any{"type": "string", "description": "Source id. Only letters, digits, underscore, and hyphen are allowed."},
				"url": map[string]any{"type": "string", "description": "HTTP or HTTPS Clash/Mihomo subscription URL."},
			},
			"required": []string{"id", "url"},
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":  map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"sources": map[string]any{"type": "array", "items": source, "description": "Complete subscription source list to write."},
				"replace": map[string]any{"type": "boolean", "description": "Replace existing sources. Defaults to true; false is not supported in the first version."},
			},
			"required": []string{"sources"},
		}
	case "subscriptions_refresh":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"config":            map[string]any{"type": "string", "description": "Subscription sources config path. Defaults to localclash-subscriptions.yaml."},
				"ids":               map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional source ids to refresh. Defaults to all."},
				"runtime_dir":       map[string]any{"type": "string", "description": "Runtime source artifact directory. Defaults to .runtime/subscriptions."},
				"merged":            map[string]any{"type": "string", "description": "Effective merged subscription path. Defaults to subscription.yaml."},
				"force":             map[string]any{"type": "boolean", "description": "Reserved compatibility flag. Existing artifacts are replaced."},
				"user_agent":        map[string]any{"type": "string", "description": "Subscription request User-Agent. Defaults to a Clash/Mihomo-like user agent."},
				"localclash_config": map[string]any{"type": "string", "description": "Durable localClash selector config path. Defaults to localclash.yaml."},
				"selection":         map[string]any{"type": "string", "description": "Derived pack selection path. Defaults to localclash-packs.yaml."},
				"policy":            map[string]any{"type": "string", "description": "Policy directory used when auto-rendering after selector refresh."},
				"rules_cache":       map[string]any{"type": "string", "description": "Rule pack cache directory used when auto-rendering after selector refresh."},
				"runtime_profile":   map[string]any{"type": "string", "description": "Runtime profile YAML path used when auto-rendering after selector refresh. Defaults to localclash-runtime.yaml."},
				"output":            map[string]any{"type": "string", "description": "Generated Mihomo config path to update when selector refresh succeeds. Defaults to generated/mihomo.yaml."},
			},
		}
	case "packs_list":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"source": map[string]any{"type": "string", "description": "Exact pack source, for example sukkaw or blackmatrix7."},
				"name":   map[string]any{"type": "string", "description": "Case-insensitive substring filter for pack name or id."},
				"target": map[string]any{"type": "string", "description": "Exact catalog default/recommended target filter, for example DIRECT, REJECT, or AI. This is not an active-config filter."},
				"limit":  map[string]any{"type": "integer", "minimum": 1},
			},
		}
	case "packs_get":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":          map[string]any{"type": "string", "description": "Catalog pack id, for example blackmatrix7_OpenAI."},
				"cache":       map[string]any{"type": "string", "description": "Pack cache directory. Defaults to .runtime/rules/packs."},
				"runtime_dir": map[string]any{"type": "string", "description": "Mihomo runtime data directory used to resolve provider paths. Defaults to .runtime/mihomo."},
			},
			"required": []string{"id"},
		}
	case "pack_rules_read":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"id":             map[string]any{"type": "string", "description": "Catalog pack id, for example sukkaw_ai or blackmatrix7_OpenAI."},
				"component":      map[string]any{"type": "string", "description": "Optional component id such as domainset, non_ip, ip, or mixed provider id."},
				"limit":          map[string]any{"type": "integer", "minimum": 0, "description": "Maximum sample rules per component. Defaults to 120. Use 0 to omit samples."},
				"refresh":        map[string]any{"type": "boolean", "description": "Force refetching provider rules instead of using provider-cache."},
				"cache":          map[string]any{"type": "string", "description": "Pack catalog cache directory. Defaults to .runtime/rules/packs."},
				"sources":        map[string]any{"type": "string", "description": "Rule sources directory used if pack catalog must be ensured. Defaults to rule-sources."},
				"provider_cache": map[string]any{"type": "string", "description": "Provider rules cache directory. Defaults to .runtime/rules/provider-cache."},
			},
			"required": []string{"id"},
		}
	case "pack_rules_prefetch":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"ids":            map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Explicit pack ids to prefetch."},
				"source":         map[string]any{"type": "string", "description": "Exact pack source filter, for example sukkaw or blackmatrix7."},
				"name":           map[string]any{"type": "string", "description": "Case-insensitive substring filter for pack name or id."},
				"target":         map[string]any{"type": "string", "description": "Exact target filter."},
				"limit":          map[string]any{"type": "integer", "minimum": 1, "description": "Maximum packs selected by filters. Defaults to 20."},
				"refresh":        map[string]any{"type": "boolean", "description": "Force refetching provider rules instead of using provider-cache."},
				"cache":          map[string]any{"type": "string", "description": "Pack catalog cache directory. Defaults to .runtime/rules/packs."},
				"sources":        map[string]any{"type": "string", "description": "Rule sources directory used if pack catalog must be ensured. Defaults to rule-sources."},
				"provider_cache": map[string]any{"type": "string", "description": "Provider rules cache directory. Defaults to .runtime/rules/provider-cache."},
			},
		}
	case "pack_rules_query":
		return map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"query":          map[string]any{"type": "string", "description": "Domain or keyword to search in locally cached provider rules, for example huggingface.co."},
				"source":         map[string]any{"type": "string", "description": "Optional exact pack source filter."},
				"name":           map[string]any{"type": "string", "description": "Optional case-insensitive pack name/id filter."},
				"target":         map[string]any{"type": "string", "description": "Optional exact target filter."},
				"limit":          map[string]any{"type": "integer", "minimum": 1, "description": "Maximum returned matches. Defaults to 20."},
				"cache":          map[string]any{"type": "string", "description": "Pack catalog cache directory. Defaults to .runtime/rules/packs."},
				"sources":        map[string]any{"type": "string", "description": "Rule sources directory used if pack catalog must be ensured. Defaults to rule-sources."},
				"provider_cache": map[string]any{"type": "string", "description": "Provider rules cache directory. Defaults to .runtime/rules/provider-cache."},
			},
			"required": []string{"query"},
		}
	default:
		return map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		}
	}
}

func ruleProviderInputSchema(idDescription string) map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"id":       map[string]any{"type": "string", "description": idDescription},
			"target":   map[string]any{"type": "string", "description": "Rule target such as DIRECT, REJECT, PROXY, or a proxy group id."},
			"reason":   map[string]any{"type": "string", "description": "Short durable reason for this external provider."},
			"type":     map[string]any{"type": "string", "enum": []string{"http", "file"}, "description": "Mihomo rule-provider type. Defaults to http."},
			"behavior": map[string]any{"type": "string", "enum": []string{"classical", "domain", "ipcidr"}, "description": "Mihomo rule-provider behavior. Defaults to classical."},
			"format":   map[string]any{"type": "string", "enum": []string{"yaml", "text", "mrs"}, "description": "Mihomo rule-provider format. Defaults to yaml."},
			"path":     map[string]any{"type": "string", "description": "Local rule-provider cache path. Defaults to ./rule_provider/<id>.yaml."},
			"url":      map[string]any{"type": "string", "description": "Remote provider URL. Required for http providers."},
			"interval": map[string]any{"type": "integer", "minimum": 0, "description": "Refresh interval in seconds. Defaults to 86400 for http providers."},
		},
		"required": []string{"id", "target"},
	}
}
