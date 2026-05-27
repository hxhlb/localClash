# localClash 架构审查报告

**日期**: 2026-05-27
**版本**: dev
**审查范围**: 全项目

---

## 目录

1. [项目概述](#项目概述)
2. [严重问题](#严重问题)
3. [高优先级问题](#高优先级问题)
4. [中等问题](#中等问题)
5. [低优先级问题](#低优先级问题)
6. [问题汇总表](#问题汇总表)
7. [改进路线图建议](#改进路线图建议)

---

## 项目概述

localClash 是一个 Go 语言编写的代理管理平台，封装了 Mihomo 代理核心（MetaCubeX 的 Clash 分支），提供 CLI 和 MCP（Model Context Protocol）接口用于 AI 代理驱动的管理。项目部署为单一 Go 二进制文件，除 `gopkg.in/yaml.v3` 外无外部运行时依赖。

### 关键指标

| 指标 | 数值 |
|------|------|
| Go 版本 | 1.24 |
| 内部包数量 | 22 |
| `.go` 文件数量 | 60+ |
| 测试文件数量 | 31 |
| 生产代码行数（估算） | ~20,000 |
| MCP 工具数 | 31 |
| 发现架构问题数 | 13 |

### 架构分层

```
入口层    main.go / product_cli.go      (两个 CLI 系统)
引导层    internal/appinit/             (Bootstrap → RuntimeState)
业务层    internal/localconfig/         (配置模型 + 解析)
         internal/configplan/          (补丁工作流)
         internal/configrender/        (Mihomo YAML 生成)
         internal/rules/               (规则包系统)
         internal/subscriptions/       (订阅管理)
运行时层  internal/corerun/            (核心进程生命周期)
         internal/routertakeover/      (OpenWrt 路由器接管)
MCP 层   internal/mcp/                 (HTTP JSON-RPC 服务)
支撑层   internal/fileops/             (文件操作)
         internal/coredownload/        (核心下载)
         internal/dashboard/           (面板下载)
         internal/subdownload/         (订阅下载)
```

---

## 严重问题

### 1. MCP 工具 Schema 大量重复 — 536 行的 switch 语句

**位置**: `internal/mcp/registry.go`（第 132-668 行）
**影响**: 每个新工具需要约 20-60 行的样板代码，且与已有定义大量重复

`inputSchemaForTool()` 函数使用手动构建的 `map[string]any` 字面量定义 25 个工具的 JSON Schema：

```go
// 这段代码在每个 case 中重复约 30 次:
return map[string]any{
    "type":                 "object",
    "additionalProperties": false,
    "properties": map[string]any{
        // ...
    },
}
```

**具体重复**:

1. **通用路径参数块重复了 10+ 次**（`subscription`、`subscription_config`、`subscription_runtime`、`rules_cache`、`runtime_profile`、`selection`、`output` 七个字段逐字相同）

2. **`matchIntent` 内联 Schema 重复了 2 次** — 在 `config_patch_create`（第 286-298 行）和 `proxy_group_build`（第 342-354 行）中完全重复

3. **`ruleIntent` 内联 Schema 重复了 2 次** — 在 `config_patch_create`（第 317-327 行）和 `custom_rules_build`（第 366-375 行）中完全重复

4. **`config_patch_create` 使用内联 schema 而不重用已有的辅助函数** — `ruleProviderInputSchema()` 和 `policyGroupInputSchema()` 辅助函数存在，但 `config_patch_create`（第 340、387 行）没有使用它们

**后果**:
- 修改一个通用参数需要编辑 10+ 个 case 块
- Schema 与 handler 的 Go 结构体定义不同步
- 新贡献者无法得知哪些字段是共享的

**修复方向**:
- 使用反射从 Go 结构体标签自动生成 JSON Schema（结构体已有 `json` 标签）
- 或将共享参数提取为可组合的 Schema 片段函数
- `config_patch_create` 应立即重构为重用 `ruleProviderInputSchema` 和 `policyGroupInputSchema`

---

## 高优先级问题

### 2. 两个互相竞争的 CLI 系统

**位置**: `main.go`（765 行） + `product_cli.go`（1,105 行）
**影响**: 代码重复、用户困惑、输出格式不一致

项目中存在两个互不兼容的 CLI 路由系统：

| 特性 | 旧 CLI (`main.go`) | 新 CLI (`product_cli.go`) |
|------|-------------------|--------------------------|
| 路由方式 | `if/else if` 链 | `switch` 语句 |
| 标志解析 | `flag.NewFlagSet` 手动定义 | 手动 `args[]` 索引 |
| 输出格式 | 纯文本 | JSON 信封 |
| 命令集 | `core download`, `subscription download`, `dashboard download`, `config render`, `rules`, `run`, `status`, `stop`, `restart`, `doctor`, `mcp` | `status`, `subscription`, `component`, `config`, `runtime`, `takeover`, `apply`, `reset`, `mcp serve` |
| 状态 | 标记为 "Legacy" | 标记为 "rewrite" |

**流程**: `main()` 首先调用 `runProductCommand()`，如果返回 "未处理"，则回退到旧的 `run()`。两个系统都独立定义了 `mcp` 命令。

此外，`product_cli.go` 使用了一个独立维护的 `productCommandWasHandled` 函数，该函数硬编码了哪些命令属于 "新" CLI，与 `runProductCommand` 中的实际 `switch` 语句重复。

**修复方向**:
- 完成 CLI 迁移，移除旧系统
- 或使用 `cobra` / `cli` 等标准 CLI 框架统一两个系统
- 统一输出格式（推荐 JSON 信封，可由 `--format json|text` 标志控制）

---

### 3. `localconfig/config.go` 职责过多（1,219 行）

**位置**: `internal/localconfig/config.go`
**影响**: 单体 Resolve 函数、类型溢出

该文件包含：
- **17 个类型定义**: `Config`、`ProxyGroup`、`PolicyGroup`、`Match`、`Pack`、`CustomRule`、`CustomRuleLine`、`ExternalRuleProvider`、`ResolveOptions`、`StageEvent`、`Resolved`、`ProxyGroupResult`、`PolicyGroupResult`、`PackResult`、`CustomRuleResult`、`RuleProviderResult`、`SubscriptionNode`、`MissingNodesError`、`SubscriptionSourceArtifact`、`SubscriptionNodeBuildStats`、`proxyGroupResolveStats`
- **一个约 230 行的 `Resolve()` 函数**，在单一线性函数中编排了代理组解析、策略组解析、规则包解析、自定义规则解析和规则提供者解析

**问题类型**:

`SubscriptionNode`、`SubscriptionNodeBuildStats`、`proxyGroupResolveStats` 和 `MissingNodesError` 等类型之所以在此文件中，仅仅是因为 `Resolve` 引用了它们——它们应属于 `subscriptions` 包或共享的类型包。

**修复方向**:
- 将 `SubscriptionNode` 及相关类型移回 `subscriptions` 包
- 将 `Resolve()` 拆分为独立的阶段函数，每个阶段有自己的类型
- 提取共享的 `StageEvent` 到公共包（见问题 #4）

---

### 4. `StageEvent` 在 5 个包中重复定义

**位置**: 5 个包，字段完全相同
**影响**: 类型碎片化、包间转换代码

| 文件 | 行号 |
|------|------|
| `internal/localconfig/config.go` | 96 |
| `internal/configplan/plan.go` | 65 |
| `internal/subscriptions/subscriptions.go` | 63 |
| `internal/routertakeover/routertakeover.go` | 39 |
| `internal/configrender/render.go` | 22 |

所有 5 个定义的字节完全相同：

```go
type StageEvent struct {
    Stage      string         `json:"stage"`
    Event      string         `json:"event"`
    DurationMS int64          `json:"duration_ms,omitempty"`
    Error      string         `json:"error,omitempty"`
    Fields     map[string]any `json:"fields,omitempty"`
}
```

此外，`Options` 结构体在 14 个包中独立定义。虽然没有 `StageEvent` 那么完全相同，但很多共享相似的字段族（特别是路径默认值）。

**修复方向**:
- 创建 `internal/shared` 或 `internal/domain` 包
- 将 `StageEvent` 和共享类型移入其中
- 所有包从一个来源导入

---

### 5. 默认路径作为字面量字符串分散在约 15 个包中

**位置**: 多个文件
**影响**: 修改一个默认路径需要编辑约 15 个文件

| 默认值 | 出现位置 |
|--------|---------|
| `"subscription.gob"` | configplan, configrender, subscriptions, server.go |
| `"localclash-runtime.json"` | configplan, configrender, subscriptions, server.go, bootstrap |
| `"generated/mihomo.yaml"` | configplan, configrender, corerun/start, corerun/run, server.go |
| `".runtime/rules/packs"` | configplan, configrender, subscriptions, rules/model, server.go |
| `"localclash.json"` | configplan, server.go, bootstrap |
| `"localclash-subscriptions.json"` | configplan, subscriptions, localconfig, envinspect, server.go |
| `".runtime/subscriptions"` | configplan, subscriptions, server.go |
| `"localclash-packs.gob"` | configplan, subscriptions, bootstrap, rules/model, server.go |
| `".runtime/mihomo"` | configplan, corerun/start, corerun/run, doctor, rules/catalog |

**六个**不同的包都实现了自己的 `normalizeOptions()` 函数，独立地对相同的回退路径进行硬编码：
- `internal/configplan/plan.go`
- `internal/configrender/render.go`
- `internal/corerun/start.go`
- `internal/corerun/run.go`
- `internal/subscriptions/subscriptions.go`
- `internal/rules/model.go`

**修复方向**:
- 创建 `internal/paths` 包，包含所有默认值常量
- 提取一个共享的 `NormalizePaths()` 函数
- 或将这些默认值集中到 `RuntimeState` 中，包通过它获取路径

---

### 6. 三个下载器共享相同的代码但没有共享的抽象

**位置**: `internal/subdownload/`、`internal/dashboard/`、`internal/coredownload/`
**影响**: GitHub 发布下载逻辑重复了 3 次

三个包都实现了完全相同的签名和结构：

```go
func Download(ctx context.Context, opts Options) (Result, error)
```

以及相同的模式：
1. HTTP GET GitHub API 发布端点
2. JSON 解码到相同的 `Release` 结构体
3. 按名称匹配资源
4. HTTP GET 资源下载 URL
5. `os.MkdirAll(filepath.Dir(path), 0o755)`
6. `os.WriteFile(path, data, 0o644)`

所有三个包还共享相同的 GitHub 发布镜像 URL 列表（如 `https://gh.llkk.cc/https://github.com`）。

**修复方向**:
- 提取 `internal/github` 或 `internal/download` 包，提供共享的 GitHub 发布获取逻辑
- 让三个包使用共享的下载基础设施，只提供特定资源的匹配和安装逻辑

---

## 中等问题

### 7. RuntimeState 结构体庞大且没有并发保护

**位置**: `internal/appinit/bootstrap.go`
**影响**: 不安全地跨 goroutine 共享可变状态

`RuntimeState` 有 38 个字段分布在嵌套结构体中。在引导期间通过值传递，但随后作为 `*appinit.RuntimeState` 指针存储，并在 MCP 服务器的所有约 30 个 handler 方法中访问。

- 无互斥锁
- 不可变性无保证
- 无接口边界

任何持有 `*RuntimeState` 引用的代码都可以读取和观察所有字段到任何其他代码的变化。

**修复方向**:
- 将 `RuntimeState` 设为不可变快照（仅值传递）
- 或添加 `sync.RWMutex` 并仅通过方法暴露访问
- 或定义接口，隔离只需部分状态的消费者

---

### 8. MCP Handler 中 30 次重复的 JSON 反序列化模式

**位置**: `internal/mcp/server.go`
**影响**: 样板代码、无错误上下文

几乎每个工具 handler 都以这三行完全相同的方式开头：

```go
var in struct { ... }
if err := json.Unmarshal(args, &in); err != nil {
    return toolResult{}, err
}
```

该模式在以下行出现：493、515、539、554、577、826、885、949、987、1076、1205、1315、1393、1463、1493、1521、1554、1588、1635、1665、1689、1755、2091、2133、2155、2210、2508、2661、2681、2717、2796。

完全没有上下文错误信息——当 Unmarshal 失败时，调用者得到的是一个没有指示哪个工具或哪个字段出错的裸错误。

**修复方向**:
- 创建一个泛型辅助函数：
  ```go
  func unmarshalArgs[T any](args json.RawMessage) (*T, error) {
      var in T
      if err := json.Unmarshal(args, &in); err != nil {
          return nil, fmt.Errorf("invalid arguments: %w", err)
      }
      return &in, nil
  }
  ```
- 将错误包装为包含工具名称

---

### 9. 引导程序执行写入操作

**位置**: `internal/appinit/bootstrap.go` — `ensureGeneratedConfig()`（第 314-373 行）
**影响**: 违反最小惊讶原则

`Bootstrap()` 应该是一个只读操作——检查系统状态并构建 `RuntimeState` 快照。然而，如果 `generated/mihomo.yaml` 不存在，`ensureGeneratedConfig` 会运行一个完整的配置渲染管道，写入文件和目录。

这违反了引导和配置生成之间的关注点分离，并使引导程序的状态依赖更难以推理。

**修复方向**:
- 移除引导程序中的渲染逻辑
- 将 "缺失生成配置" 作为诊断警告报告
- 让渲染作为显式的用户操作运行（`config render` 或 `apply`）

---

### 10. 错误被静默抑制

**位置**: `internal/mcp/server.go` 第 162 行
**影响**: `applyConfigToolDefaults` 中的错误被丢弃

```go
err = nil
```

此行无条件覆盖了 `applyConfigToolDefaults` 返回的错误。无日志、无条件检查、无解释。

`http.ErrServerClosed` 的处理模式（第 160-161 行）已被正确使用——该模式过滤掉正常的关闭错误。但第 162 行的 `err = nil` 语义不同：它丢弃了一个*初始化*错误（`applyConfigToolDefaults`），但使用了与 `http.ErrServerClosed` 过滤器相同的变量名，使读者困惑。

**修复方向**:
- 使用不同的变量名
- 将来自 `applyConfigToolDefaults` 的错误视为诊断警告而非静默丢弃
- 如果错误是预期的，则添加解释性注释

---

## 低优先级问题

### 11. 硬编码的魔法值

**IP 地址和端口**（在 `internal/` 中）：

| 值 | 位置 | 用途 |
|----|------|------|
| `127.0.0.1:8765` | `mcp/server.go:135` | MCP 默认监听地址 |
| `127.0.0.1:9090` | `configrender/render.go:618` | 外部控制器 |
| `0.0.0.0:7874` | `runtimeprofile/profile_test.go` | DNS 监听 |
| 端口 7874、7890、7892、7893、7895、9000 | 多个文件 | 运行时默认端口 |

**平台特定路径**:

| 值 | 位置 | 问题 |
|----|------|------|
| `/root/localclash` | `appinit/bootstrap.go:99` | Linux 专用，在 macOS 上无效 |
| `/tmp/localclash/router-takeover` | `routertakeover/routertakeover.go` | 硬编码的临时目录 |
| `/proc/<pid>/stat` | `corerun/control.go:463` | Linux 专用 procfs |

**外部引用**:

| 值 | 位置 |
|----|------|
| `clash-verge/v1.5.1` | `subscriptions/subscriptions.go:26` |
| `MetaCubeX/mihomo` | `coredownload/download.go` |
| `Zephyruso/zashboard` | `dashboard/download.go` |

**修复方向**: 将硬编码值提取为命名常量和配置。将平台特定路径移到 `runtimeprofile` 配置中。

---

### 12. 测试文件与生产代码一样庞大

最突出的例子：
- `internal/mcp/server_test.go`（2,904 行）测试 `server.go`（2,896 行）
- `internal/configplan/plan_test.go`（1,014 行）

测试按文件耦合，而非按行为组织。测试文件与对应的生产文件一一对应，无法按功能域拆分为独立的测试套件。

---

### 13. 有限的 panic 使用 — 可接受

代码库中只有两处 `panic()` 调用，均在 `internal/runtimeprofile/profile.go` 中：
- 第 253 行：嵌入的默认配置文件无效时
- 第 370 行：缺少嵌入的默认配置文件时

两者都是不变的违规——在编译时嵌入的资产出现问题时触发。这种使用是惯用的且可接受。

---

## 问题汇总表

| # | 严重程度 | 问题 | 文件 | 影响范围 |
|---|---------|------|------|---------|
| 1 | **严重** | 536 行重复 Schema switch | `internal/mcp/registry.go` | 添加/修改工具的成本 |
| 2 | **高** | 两个 CLI 系统 | `main.go` + `product_cli.go` | 用户体验、维护负担 |
| 3 | **高** | 1,219 行配置类，含单体 Resolve | `internal/localconfig/config.go` | 配置解析的可理解性 |
| 4 | **高** | StageEvent 在 5 个包中重复定义 | 多个文件 | 类型碎片化 |
| 5 | **高** | 默认路径在约 15 个包中硬编码 | 多个文件 | 修改路径的成本 |
| 6 | **高** | 三个重复的下载器 | subdownload, dashboard, coredownload | 下载逻辑维护 |
| 7 | **中** | RuntimeState 无并发保护 | `internal/appinit/bootstrap.go` | 运行时安全性 |
| 8 | **中** | json.Unmarshal 模式重复约 30 次 | `internal/mcp/server.go` | 错误上下文缺失 |
| 9 | **中** | 引导程序执行写入操作 | `internal/appinit/bootstrap.go` | 关注点分离 |
| 10 | **中** | 错误被静默抑制 | `internal/mcp/server.go:162` | 调试困难 |
| 11 | **低** | 硬编码魔法值 | 多个文件 | 跨平台移植 |
| 12 | **低** | 测试文件过度耦合 | `*_test.go` | 测试可维护性 |
| 13 | **低** | 有限的 panic 使用 | `internal/runtimeprofile/profile.go` | 可接受 |

---

## 改进路线图建议

### 第一阶段：止血（低风险，高收益）

1. **提取共享类型包** — 创建 `internal/shared/` 包，迁移 `StageEvent`，更新 5 个消费者
2. **提取路径常量** — 创建 `internal/paths/` 或向 `appinit` 添加路径默认值常量
3. **添加 `unmarshalArgs[T]` 泛型辅助函数** — 消除 MCP handler 中约 30 处 `json.Unmarshal` 样板代码
4. **修复 `err = nil`** — 添加适当的错误处理或解释性注释

### 第二阶段：结构重构（中等风险，中等收益）

5. **重构 `config_patch_create` Schema** — 重用已有的 `ruleProviderInputSchema` 和 `policyGroupInputSchema` 辅助函数
6. **完成 CLI 迁移** — 移除旧 `main.go` CLI，仅使用 `product_cli.go`（或反之）
7. **提取共享下载包** — 为 GitHub 发布获取创建 `internal/github/`，供三个下载器使用

### 第三阶段：架构现代化（高风险，高收益）

8. **拆分 `Resolve()` 函数** — 拆分为每个阶段的独立函数，配以专用类型
9. **为 RuntimeState 添加并发安全** — 不可变快照或受保护的访问器
10. **将引导程序设为只读** — 将配置渲染移出 `Bootstrap()`

### 第四阶段：打磨

11. **将硬编码值提取到配置中** — 端口、IP、URL 模式
12. **添加平台抽象** — 使 procfs 和 `/root/localclash` 可移植
13. **Schema 生成** — 考虑基于反射的 JSON Schema 生成以取代 `registry.go` 中的手动 switch

---

*报告结束*
