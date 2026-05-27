# localClash 架构审查报告

## 1. 项目概要

localClash 是一个 Go 1.24 编写的 Mihomo 运行时管理工具（`internal/` 下一级包 22 个，非测试 Go 源文件 36 个，测试文件 31 个），通过 MCP（Model Context Protocol）接口让 AI Agent 管理本地代理运行时。项目运行于本地机器、NAS、家庭服务器或 OpenWrt 路由器上，负责 Mihomo 内核的下载、配置生成、运行生命周期管理以及路由器透明代理（redir-host-mix）接管。

### 核心设计原则

- **localclash.json 是唯一真相源**，generated/mihomo.yaml 是构建产物
- **安全基线不可禁**，本地网络保护规则硬编码在渲染器中
- **两层策略体系**：精简（minimal）与预设（localclash-default）
- **Patch 叠加模型**：预设模板通过 8 个有序 patch 文件叠加构建
- **MCP 作为主要管理面**，CLI 作为辅助调试入口
- **AI Agent 产生意图，localClash 编译为 Mihomo 配置**

---

## 2. 架构全景图

```
┌─────────────────────────────────────────────────────────────┐
│                      MCP Client (AI Agent)                    │
└──────────────────────────┬──────────────────────────────────┘
                           │ JSON-RPC
┌──────────────────────────▼──────────────────────────────────┐
│                   MCP HTTP Server (:8765)                     │
│  33 tools: safe_read(15) | safe_write(13) | confirm_required(5) │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                     CLI / main.go                             │
│  run | mcp | doctor | config render | core download | ...    │
└──────────────────────────┬──────────────────────────────────┘
                           │
┌──────────────────────────▼──────────────────────────────────┐
│                   Bootstrap (appinit)                         │
│  RuntimePaths, CoreState, SubscriptionState, RulesState      │
└──────────────────────────┬──────────────────────────────────┘
                           │
      ┌────────────────────┼──────────────────────┐
      ▼                    ▼                       ▼
┌─────────────┐   ┌───────────────┐    ┌──────────────────┐
│ localconfig │   │  configrender │    │   configplan      │
│ 真相源解析   │   │  配置渲染引擎  │    │  Patch 创建/应用  │
└──────┬──────┘   └───────┬───────┘    └────────┬─────────┘
       │                  │                      │
       ▼                  ▼                      ▼
┌─────────────┐   ┌───────────────┐    ┌──────────────────┐
│   rules     │   │ runtimeprofile│    │  policytemplate   │
│ 规则/Pack   │   │ 运行时配置管理 │    │  模板加载与合并   │
└─────────────┘   └───────────────┘    └──────────────────┘
       │
       ▼
┌─────────────────────────────────────────────────────────────┐
│                     外部依赖                                  │
│  rule-sources/ (4 adapters) + policy-templates/ (2 templates)│
└─────────────────────────────────────────────────────────────┘
```

---

## 3. 两层策略体系深度分析

### 3.1 第一层：精简（minimal）

**定位**：面向高级用户的手动定制模式，提供最少的翻墙组件。

**结构**（`policy-templates/minimal.json`）：
```
proxy_groups:
  🎯 手动选择  (manual, 匹配所有节点 regex ".*")
  ⚡ 自动选择  (auto/url-test, 匹配所有节点 regex ".*")

policy_groups:
  DNSProxy   (manual, exit → ⚡ 自动选择)

packs: []
```

**渲染结果**：
1. 13 条本地安全基线（localhost、LAN、私有 IP、本地 DNS）
2. 两条 Dashboard 可选组：手动选择（全部节点）、自动选择（全部节点 url-test）
3. DNSProxy 策略组指向自动选择
4. `MATCH,DIRECT` 兜底 — 未匹配流量直连

**评价**：
- 确实做到了"最少化"：无任何 pack、无区域出口、无业务分组
- 用户需自行通过 MCP patch 工具添加任何额外路由规则
- DNS 海外解析通过 DNSProxy → ⚡ 自动选择 → 订阅节点完成
- 适合在路由器上快速启动，然后由 Agent 一步步构建自定义规则

### 3.2 第二层：预设（localclash-default）

**定位**：类 ACL4SSR 的开箱即用默认模板，基于 v2fly-dlc 的 GEOSITE 高性能规则集。

**Patch 叠加结构**（8 个有序 patch 文件）：

| 顺序 | 文件 | 职责 |
|------|------|------|
| 00 | `00-region-exits.json` | 全局直连出口 + 6 个区域出口（TW/SG/JP/US/KR/HK），每个通过 name_regex 匹配订阅节点名 |
| 10 | `10-direct-baseline.json` | REJECT 规则 + 私有/追踪器/下载/大陆直连 packs |
| 20 | `20-communication-social.json` | 通讯 + 社交媒体 + Telegram（含 IP CIDR 规则） |
| 30 | `30-ai-dev-speedtest.json` | ChatGPT + AI + GitHub + Speedtest 业务分组 |
| 40 | `40-steam.json` | Steam 业务路由 |
| 50 | `50-platform-media.json` | Apple/Google/Microsoft/TikTok/流媒体平台分组 |
| 60 | `60-games.json` | 巴哈姆特 + 游戏平台分组 |
| 70 | `70-tail-fallback.json` | 娱乐/电商/非中国 geoip/中国尾部回退 |

**路由层级结构**：
```
业务策略组 (Policy Group) → 出口代理组 (Proxy Group) → 订阅节点
例如: 🎮 Steam → 🇭🇰 香港 → [HK Node 1, HK Node 2, ...]
```

**评价**：
- 8 个 patch 的有序叠加设计清晰，每个 patch 职责单一
- 通过 `mergeConfig()` 实现 ID 去重覆盖、新 ID 追加
- 保留了 ACL4SSR 的业务分组惯例：通讯、社交、AI、Steam、流媒体、游戏等
- 区域出口为 Optional，订阅中无对应区域节点时自动跳过，不报错
- 使用 v2fly-dlc 数据生成 GEOSITE 规则，性能优于逐个域名匹配

---

## 4. 核心组件分析

### 4.1 配置渲染管线

```
localclash.json + subscription.gob + policy template + packs selection
    │
    ▼
localconfig.Resolve()
    ├── 加载订阅节点
    ├── 解析代理组（regex 匹配节点名）
    ├── 解析策略组（验证 exits 引用有效代理组）
    ├── 解析 packs（验证 ID + target 合法性）
    ├── 解析自定义规则（验证类型 + target）
    └── 解析外部规则提供者（验证 URL/Behavior/Format）
    │
    ▼
rules.RenderFragment()
    ├── 生成 rule-provider 定义
    ├── 生成 RULE-SET / GEOSITE 规则
    └── 物化代理组/策略组为 Mihomo proxy-groups
    │
    ▼
configrender.buildRuntimeConfig()
    ├── 13 条本地安全基线
    ├── 用户自定义规则
    ├── Pack/外部提供者规则
    └── MATCH,DIRECT 兜底
    │
    ▼
runtimeprofile.ApplyToConfig()
    └── 合并 runtime 设置（端口、DNS、TUN 等）
    │
    ▼
generated/mihomo.yaml
```

### 4.2 Patch 工作流（create-review-apply）

这是 localClash 最重要的安全设计：

1. **config_patch_create**：加载当前 localclash.json → 叠加 overlay → 解析 → 渲染 → 写入 `.runtime/patches/<id>/`（不触碰活跃文件）
2. **审查**：Agent 展示 diff/summary 给用户确认
3. **config_patch_apply**：验证 → 备份旧文件到 `.runtime/backups/` → 顺序写入 localclash.json、localclash-packs.gob、generated/mihomo.yaml（注意：当前是顺序写入而非 temp+rename 原子事务）
4. **不自动重启**：应用后需要单独确认 restart_runtime

### 4.3 MCP 工具安全分级

| 等级 | 数量 | 典型工具 |
|------|------|---------|
| safe_read | 15 | config_status, doctor, routing_explain, packs_list |
| safe_write | 13 | config_patch_create, subscriptions_refresh, config_render, sed_file |
| confirm_required | 5 | run_runtime, restart_runtime, router_takeover_* |

运行时会改变网络连接状态的工具都需要显式确认，这是合理的安全边界。

### 4.4 规则来源适配器

4 个适配器将外部规则数据转为统一的 Pack 目录：

| 适配器 | 来源 | 输出类型 | 用途 |
|------|------|---------|------|
| v2fly-dlc | github.com/v2fly/domain-list-community | GEOSITE | 高性能域名分类（默认模板主要使用） |
| sukkaw | ruleset.skk.moe | rule_provider | Surge 规则 CDN 分发 |
| blackmatrix7 | github.com/blackmatrix7 | rule_provider | Clash 规则集 |
| syncnext | SyncnextClash 维护 | rule_provider | App 维护专用 packs |

---

## 5. 架构优势

### 5.1 设计层面

1. **真相源与构建产物分离**：localclash.json vs generated/mihomo.yaml，避免直接编辑 YAML 带来的不可维护性
2. **两层策略互补**：minimal 给高级用户最大自由度，localclash-default 给普通用户开箱即用体验
3. **Patch 叠加模型**：创建阶段完全隔离（写入 `.runtime/patches/` 而非活跃文件），应用阶段先备份再顺序写入，patch 之间职责单一
4. **AI Agent 原生设计**：MCP 工具提供了完整的观察→计划→审查→应用的闭环
5. **安全基线不可绕过**：13 条本地保护规则硬编码，确保 LAN/私有 IP/本地 DNS 不会因配置错误而泄露
6. **路由器一等公民**：normal/router 双配置文件模式，router 模式的 redir-host-mix 透明代理设计完整

### 5.2 工程层面

1. **明确的文件边界**：`.runtime/` 所有运行时产物，`generated/` 构建产物，`internal/` 功能代码
2. **结构化错误处理**：Resolve/Patch/Apply 都使用 Stage 事件机制，可观测性强
3. **配置版本化**：localclash.json 有 version 字段（当前 v1/v2），为未来迁移留空间
4. **go:embed 嵌入默认配置**：runtime profile 默认文件编译进二进制，首次运行时自动写出
5. **Pack 目录缓存**：避免每次渲染都重新下载规则数据

---

## 6. 架构问题与改进建议

本节不把“文件长度”或“代码重复”作为主要排序依据。localClash 的主要风险来自它运行在资源受限路由器上，并且会改变代理进程、防火墙、DNS 和策略路由状态。因此改善目标应按产品安全、可观测性、热路径成本和配置事务性排序。

### 6.0 应该完成的改善目标

#### P0：让路由器运行状态始终可观察、可恢复

目标不是增加更多日志，而是让真实路由器上的事故能被复盘：

- MCP HTTP、runtime start/restart、config render、patch apply、router takeover apply/stop 都应有持久、有限大小、可通过 MCP 读取的阶段日志。
- LuCI、deploy script、procd service 等部署路径应一致保留已有 stderr request summary，避免只在某个安装方式下可观察。
- `run_runtime` / `restart_runtime` 与 `router_takeover_apply` / `router_takeover_stop` 必须继续分离，不能为了“一键接管”把 Mihomo 进程状态和 OpenWrt 网络状态合成一个不透明动作。
- 路由器接管失败时要留下可执行的恢复指引，且不写入持久 firewall 配置；重启路由器应能清除 runtime takeover 状态。

完成标准：

- 一次失败的 router takeover 或 runtime restart，可以仅凭 MCP task log、service log 和 status artifact 判断卡在 config test、process start、TUN ready、nft chain、DNS hijack 还是 verification。
- 发布默认仍保持轻量：没有无界日志、没有高频轮询、没有默认 verbose。

#### P1：让热路径成本有上限，并能重复测量

`index.gob` runtime contract 已经把主要 `localconfig.Resolve` 热点从十秒级降到秒内；后续目标应转向仍然昂贵或容易回归的路径：

- 为 Mihomo `-t` 建立验证缓存，缓存维度至少包含 generated config SHA-256、core 类型/版本、验证时间和结果。
- generated config 与 core 未变化时，`restart_runtime` 应复用已通过的验证结果；配置变更后的第一次验证仍保留 `mihomo -t`。
- 整理订阅 source artifact 状态，避免 `.runtime/subscriptions/<source>.gob` 长期缺失时隐式 fallback 到 `subscription.gob`。
- 固化真机或 OpenWrt VM 的 MCP performance smoke 脚本，记录工具耗时、阶段耗时、localClash/Mihomo/背景 Clash CPU、输出 JSON artifact。

完成标准：

- 基础 `routing_explain` 保持 2s 内，完整证据模式可以更慢但必须显式标记。
- 无配置变更的 `restart_runtime` 目标低于 3s。
- `config_render`、`config_patch_create/apply`、`routing_explain` 的阶段耗时有可重复的 smoke 结果，避免再次把 CPU 问题归因到“整个 MCP server 很贵”。

#### P2：把配置生命周期做成显式事务

localClash 的核心产品价值是“Agent 产生意图，localClash 编译为 Mihomo 配置”。因此配置生命周期不能有隐藏写入或部分提交：

- `Bootstrap()` 不应在普通进程启动时静默重写 `generated/mihomo.yaml`；最多只允许首次引导生成，或完全改由 `config_render` / patch apply 显式触发。
- `config_patch_apply` 应改成 temp+fsync+rename 的原子提交模型，三个输出文件全部准备成功后再切换 active state。
- 失败时应能明确回答：active config 未变、candidate 已保存、backup 在哪里、下一步如何重试或恢复。
- `config_status`、`doctor --json` 和 patch task log 应能表达当前 durable intent、generated artifact 和 validation metadata 是否一致。

完成标准：

- 不存在“进程启动就改 generated config”的常规路径。
- patch apply 任意一步失败后，不会留下 localclash.json、packs gob、generated yaml 互相不匹配的 active 状态。

#### P3：收敛产品入口与 Agent 工具面

当前 MCP 工具面已经能覆盖观察、规划、patch、运行和接管，但入口代码仍有可维护性风险：

- 统一 CLI 产品入口，明确 legacy/internal command 的边界，避免两套路由、两套默认值和两套测试门禁继续扩张。
- 抽取 MCP 参数解析 helper，减少 `server.go` 中重复 `json.Unmarshal(args, &in)` 的样板，但不要引入过重 framework。
- 保留各核心 package 的 StageEvent 输出契约；只有当重复结构妨碍日志汇总时，再抽薄 helper。

完成标准：

- 新增 MCP 工具时，registry metadata、JSON schema、server dispatch、安全级别和测试能按固定模板落地。
- CLI usage 不再依赖“rewrite 期间 legacy command 暂存”的叙述。

#### P4：暂时挂起，发布版本后提供本地 rule pack 能力支持

规则模型文档已经定义 5 层优先级：安全基线、用户 override、可选 rule pack、policy template patch、fallback。当前第 3 层 standalone local rule pack 仍是目标契约，不是已完成能力。

当前决策：

- 发布前暂时不把 `rule-packs/*.json` 本地规则包作为必须完成项。
- 当前版本优先完成路由器可观测性、热路径成本上限、配置事务化和入口收敛。
- 发布版本后再提供本地 rule pack 能力支持，并确保 UI / Agent 写入 durable localClash intent，而不是直接 patch `generated/mihomo.yaml`。
- 后续实现时，optional pack 与 policy template patch 仍应在文档、MCP 输出和渲染顺序中保持可解释。

### 6.1 Stage 事件代码重复（优先级：低）

**问题**：五个包（`localconfig`、`configrender`、`configplan`、`subscriptions`、`routertakeover`）各自定义了 `StageEvent` 类型和 emitter 函数，结构几乎相同。

```go
// localconfig/config.go
type StageEvent struct { ... }
func localConfigStageEmitter(...) { ... }

// configrender/render.go
type StageEvent struct { ... }
func configRenderStageEmitter(...) { ... }

// configplan/plan.go
type StageEvent struct { ... }
func configPlanStageEmitter(...) { ... }
```

**说明**：这些独立定义让每个 package 保持独立的输出契约，不是高优先级的架构问题。如果要抽取，应只抽取一个薄的 helper（共享 event 结构体），避免让这些核心包反向依赖一个过度抽象的"事件框架"。

### 6.2 类型系统有重复定义但各有职责（优先级：低）

**说明**：`localconfig` 和 `rules` 包中有同名类型（`ProxyGroup`、`PolicyGroup`、`CustomRule`、`ExternalRuleProvider`），但它们服务于不同阶段：
- `localconfig.ProxyGroup` 是 durable intent，字段为 `mode/match/selected_nodes`
- `rules.ProxyGroup` 是 render-ready selection，字段为 `nodes/auto/manual/smart/direct`

这两个阶段之间的显式转换（如 `customRulesForSelection`、`ruleProvidersForSelection`）是有意保留的边界，各自保持独立的输出契约。不应嵌入类型消除转换。

**建议**：为转换函数和两边类型添加注释说明职责边界即可，不建议合并。

### 6.3 `configplan.go` 过长（暂不处理）

**说明**：`internal/configplan/plan.go` 约 1450 行，包含 patch 创建、应用、验证、备份、Mihomo 测试等逻辑。当前虽然文件偏长，但边界仍集中在 config patch 生命周期内，且测试覆盖较完整。

**决策**：暂不处理。除非后续修改同一区域时已经出现明确维护成本，否则不因行数本身拆分文件。

### 6.4 `config.go` 过长（暂不处理）

**说明**：`internal/localconfig/config.go` 约 1220 行，覆盖 durable config 类型、节点加载、Gob 读取、proxy/policy group 解析、pack/custom rule/rule provider 验证等逻辑。它承担的是 localClash intent 解析边界，当前仍是一个可追踪的责任域。

**当前状态**：`localconfig.Resolve` 的主要性能瓶颈已经通过 `index.gob` runtime contract 收敛，常见 config/routing 解析路径从十秒级降到秒内。因此不应再把“拆分 config.go”当成解决 CPU 问题的前置动作。

**决策**：暂不处理。后续只在修改同一区域时顺手改善局部可读性；真正的性能目标转向 Mihomo `-t` 验证缓存、订阅 source artifact 状态整理和可重复 performance smoke。

### 6.5 `main.go` / `product_cli.go` 偏长（暂不处理）

**说明**：`main.go` 约 759 行，`product_cli.go` 约 1105 行，确实承担较多 CLI 命令路由和辅助函数。但当前构建通过，CLI 入口偏长本身不是比 MCP/Bootstrap/config apply 更高优先级的问题。

**决策**：不因行数本身拆分文件。双 CLI 系统本身造成的产品入口和测试门禁问题另见 6.13。

### 6.6 规则层第 3 层为设计目标，发布后支持（暂时挂起）

**说明**：`docs/rule-model.md` 定义的 5 层规则优先级是目标契约，其中第 3 层"可选规则包"（standalone `rule-packs/*.json`）在当前代码中尚未实现。文档自身也明确写明了这一状态：

> Current code still does not yet have: standalone local rule pack files

当前渲染器实际为 `baseline + fragment(custom_rules/packs/external_providers) + MATCH,DIRECT`（`buildOrderedRules()`），所有用户侧内容通过同一个 `RenderFragment` 渲染。

**决策**：暂时挂起，不作为发布前必须完成的改善目标。发布版本后再添加 `rule-packs/*.json` 本地规则包文件支持，使第 3 层成为独立可选项。

### 6.7 `stringValue` / `anyMapSlice` 等工具函数重复（优先级：低）

**问题**：`stringValue`、`anyMapSlice`、`appendUnique` 等工具函数在 `localconfig`、`configrender`、`rules` 中都有独立定义。

**建议**：只有当重复逻辑继续扩张或开始出现不一致行为时，才抽取到极薄的公共 helper。不要为了消除几处小函数重复而引入通用工具包依赖，避免核心 package 边界变模糊。

### 6.8 测试覆盖以单元测试为主，缺少集成级风险导向测试（优先级：中）

**现状**：项目已有 31 个 `*_test.go` 文件，覆盖 23 个 package；本次复核 `go test ./...` 通过（280 passed）。测试涵盖 MCP registry、安全分级、config render、config plan、localconfig、router takeover 等核心模块。

**不足**：
- 缺少跨模块的集成测试（如完整的 subscribe → configure → render → validate 流程）
- 缺少 router takeover 的真机/VM smoke 测试
- 缺少 patch apply 失败、部分写入、回滚/原子性等风险路径测试

**建议**：补充关键路径的集成测试和回归保护，优先覆盖 P0-P2 目标：runtime restart 验证缓存、patch apply 原子性、bootstrap 不静默写入、router takeover 状态验证、MCP performance smoke 输出格式。

### 6.9 配置版本迁移路径（不构成当前问题）

**说明**：`localclash.json` 的 `policy_groups` / version 语义仍属于未发布内容，还不是对外稳定格式。当前代码中曾经出现的 v2 语义不应被视为已经发布的兼容性承诺。

**决策**：不按 v1→v2 迁移债处理。正式发布前可以把 schema 直接收敛并落到 version 1；只有发布后形成用户可依赖格式时，才需要记录 schema 历史和迁移路径。

### 6.10 config_patch_apply 非原子事务（优先级：中）

**问题**：`config_patch_apply` 在备份后顺序写入三个文件（`localclash.json` → `localclash-packs.gob` → `generated/mihomo.yaml`），如果中途失败，已写入的文件不回滚，可能留下部分更新的状态。create/review 阶段是完全隔离的，但 apply 阶段不是 temp+rename 原子提交。

**建议**：改为先写到临时文件，全部成功后 fsync+rename，失败时保持 active state 不变。这个目标应高于单纯的 CLI 拆分或工具函数去重，因为它直接影响 Agent 修改配置时的可恢复性。

### 6.11 MCP server 中 json.Unmarshal(args, ...) 样板代码严重（优先级：中）

**问题**：`internal/mcp/server.go` 中 `json.Unmarshal(args, &in)` 模式出现 31 次，每个工具调用入口都需要手动反序列化参数，大量重复。

**建议**：抽取通用的参数解析 middleware 或 helper，减少重复代码。

### 6.12 Bootstrap 阶段自动写入 generated/mihomo.yaml（优先级：高）

**问题**：`appinit.Bootstrap()` → `ensureGeneratedConfig()` 会在订阅可用时自动调用 `configrender.Render()` 写入 `generated/mihomo.yaml`。这意味着：

- 任何进程启动（包括 MCP server 启动、CLI 调用）都可能静默重写构建产物
- 绕过了 create-review-apply 审查流程
- Bootstrap 本应是只读初始化，却执行了写操作

**建议**：将 bootstrap 中的 config render 改为仅在 `generated/mihomo.yaml` 不存在时才自动生成（首次启动引导），或者完全移除，改为由 Agent 通过 `config_render` MCP 工具显式触发。长期目标是让 bootstrap 成为只读状态组装，所有写入都进入明确的配置生命周期。

### 6.13 双 CLI 系统造成产品入口和测试门禁断裂（优先级：高）

**问题**：`main.go`（765 行）和 `product_cli.go`（1105 行）共同承担 CLI 命令路由和产品命令实现，`main.go` 的 usage 也仍保留 "Legacy/internal commands still available during the CLI rewrite"。这不是单纯文件长度问题，而是当前存在两套命令入口、两套默认值/状态组装路径、两套测试覆盖面的产品边界问题。

当前 `go test ./...` 已通过，说明门禁没有处于红灯状态。但双入口仍会放大 bootstrap、runtime profile、MCP execution tool 和 product command 之间的行为漂移风险。若另一路 CLI 继续扩张，后续问题会更难定位。

**建议**：统一 CLI 入口，将 product_cli.go 的命令合并到 main.go 或拆分到 `internal/cli/`。排序上应放在 bootstrap 隐式写入、patch apply 原子性、Mihomo 验证缓存之后；它是产品边界收敛目标，不是当前最急的路由器安全问题。

---

## 7. 安全审查

### 7.1 已实现的安全措施

- 订阅 URL 存储在 `localclash-subscriptions.json`（gitignored）
- 订阅代理节点凭据不在 MCP 工具输出中暴露
- `subscription_nodes_list/search` 仅返回 name + type
- `run_runtime` / `stop_runtime` / router takeover 工具需要 confirm_required
- `stop_runtime` 在 router takeover active 时拒绝停止
- Patch 应用前自动备份旧文件到 `.runtime/backups/`

### 7.2 值得关注的点

1. **MCP HTTP 默认绑定 127.0.0.1**：安全，但部署到路由器时文档中示例绑定 `192.168.6.1:8765`，需确保局域网访问受控
2. **无认证机制**：MCP HTTP 端点无认证，依赖网络隔离

---

## 8. 总结

localClash 的架构设计体现了清晰的系统工程思维：

- **真相源与产物分离**解决了 Clash 配置管理的核心痛点
- **两层策略体系**兼顾了高级用户的灵活性和普通用户的开箱即用
- **Patch 叠加模型**通过有序、可审查的 JSON patch 实现了安全的配置变更
- **MCP 原生设计**让 AI Agent 可以安全地观察、规划、审查和应用配置变更
- **安全基线硬编码**保证了本地网络在错误配置下的稳定性

应该完成的改善目标：
1. **路由器运行状态可观察、可恢复**：统一保留 MCP/runtime/takeover 阶段日志，让真机事故能从 artifact 复盘（6.0 P0）
2. **热路径成本有上限**：保留 `index.gob` 成果，继续做 Mihomo `-t` 验证缓存、订阅 artifact 整理和 performance smoke（6.0 P1）
3. **配置生命周期显式事务化**：Bootstrap 不静默写入 generated config，`config_patch_apply` 改为 temp+fsync+rename 原子提交（6.0 P2、6.10、6.12）
4. **产品入口收敛**：统一 CLI 边界，减少 MCP 参数解析样板，但不为消除小重复牺牲核心 package 边界（6.0 P3、6.11、6.13）
5. **发布后补齐用户可配置规则层**：本地 rule pack 支持暂时挂起，发布版本后再让 optional pack 成为独立 UI/Agent 自定义层（6.0 P4、6.6）
6. **补充风险导向测试**：优先覆盖 patch apply 原子性、bootstrap 写入边界、restart 验证缓存、router takeover smoke 和 MCP performance smoke（6.8）

整体而言，项目在 33 个 MCP 工具、4 个规则适配器、2 种运行时模式、2 套策略模板的复杂度下，保持了良好的模块边界和一致的设计模式。
