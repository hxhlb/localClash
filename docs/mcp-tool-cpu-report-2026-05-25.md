# MCP 工具 CPU 報告 - 2026-05-25

這份報告記錄 2026-05-25 測試輪次中的真實路由器 MCP 工具覆蓋情況與 CPU 觀察結果。

目標路由器：

- 主機：`root@192.168.6.1`
- MCP endpoint：`http://192.168.6.1:8765/mcp`
- 測試二進位 SHA-256:
  `0ade115d24e0d7595018e11ac97094dfcb9b581c320499a954b23b41bc182a21`
- Commit: `85c8f42 Add staged logs for MCP execution tools`

安全邊界：

- `router_takeover_apply` 和 `router_takeover_stop` 只以 `dry_run=true` 測試。
- 沒有實際套用路由器接管。
- Runtime 測試使用 `/tmp/localclash-mcp-tooltest`，不是目前使用中的
  `/root/localclash` runtime 目錄。
- 測試用 runtime 已在最後停止。

## 摘要

共成功測試 33 個 MCP 工具。

目前的 CPU 問題並不是平均分散在所有 MCP 工具上，而是集中在兩類路徑：

1. localClash 的設定解析路徑，尤其是 `localconfig.Resolve`。
2. Mihomo 設定驗證，尤其是 `mihomo -t`。

測試清理完成後，MCP 伺服器回到閒置狀態：

- `localclash` 狀態：sleeping
- RSS：約 40 MB
- 採樣 CPU 閒置率：約 93.6%

因此，這次測試沒有重現 localClash 程序持續閒置卻佔用 100% CPU 的問題；但有重現特定 MCP 操作期間的高 CPU。

## 2026-05-26 修正後回測

後續修正將 pack runtime path 從 YAML catalog / directory scan 改成
`index.gob` runtime contract：

- `rules adapt` 生成 `.runtime/rules/packs/index.gob`。
- runtime 缺少 `index.gob` 時 hard fail，提示重新執行 `localclash rules adapt`。
- runtime 不再 fallback 到 YAML catalog 或目錄掃描。
- `localconfig.Resolve` 只載入一次 `PackIndex`，後續 pack 解析全部走 map lookup。
- 新增 `rules index-dump --format json|yaml` 作為觀測手段。

回測部署：

- 主機：`root@192.168.6.1`
- Commit：`f027711 refactor: make pack index a runtime contract`
- 路由器二進位 SHA-256：
  `61010f89ba700b42e7129fbe199fe0554667e55d97824e4f51d453b6c022f655`
- `index.gob`：`/root/localclash/.runtime/rules/packs/index.gob`
- `index.gob` 大小：約 `2.8M`
- `rules adapt` 產物：
  - `blackmatrix7`: 668 packs
  - `sukkaw`: 31 packs
  - `syncnext`: 2 packs
  - `v2fly-dlc`: 1472 packs

安全邊界：

- 沒有啟動或停止 Mihomo runtime。
- 沒有執行 Network Takeover。
- 只測試 `rules adapt`、`config render`、MCP `config_render` 和
  `routing_explain` 等不改變路由器接管狀態的路徑。

### 修正後耗時

CLI `config render`：

| Run | 時間 |
| --- | ---: |
| 1 | 1140ms |
| 2 | 1080ms |
| 3 | 760ms |

MCP `config_render` 任務階段：

| 階段 | 時間 |
| --- | ---: |
| `resolve_localclash_config` | 363ms |
| `load_subscription_nodes` | 122ms |
| `load_pack_index` | 157ms |
| `resolve_packs` | 171ms |
| `render_generated_config` | 552ms |
| `render_generated_config.render_pack_selection` | 334ms |

MCP `routing_explain`：

| 查詢 | 時間 |
| --- | ---: |
| `telegram`, `include_rule_matches=false` | 537ms |
| `telegram`, `include_rule_matches=true` | 880ms |

### 前後對比

| 路徑 | 修正前 | 修正後 |
| --- | ---: | ---: |
| `config_render` 總時間 | 16828ms | 760-1140ms |
| `resolve_localclash_config` | 15024ms | 363ms |
| `routing_explain` | 16756-26888ms | 537-880ms |

結論：

- `localconfig.Resolve` 的主要瓶頸已確認是舊 pack runtime path 的重複
  YAML catalog / 目錄掃描與解析。
- `index.gob` runtime contract 有效；它把常見 config/routing 解析路徑從
  十秒級降到秒內。
- 連續執行 10 次 `config render` 時，單次短窗口仍會看到短暫
  `localclash` CPU 100% 左右，但 10 次總耗時為 8820ms；完成後 MCP 常駐
  `localclash` 進程回到 0.0% CPU。
- 這次沒有重現修正前那種長時間卡住 CPU 的 localClash 狀態。

剩餘觀察：

- MCP 任務日誌顯示 `.runtime/subscriptions/sub1.gob` 不存在時會 fallback 到
  `subscription.gob`。這不是本輪 CPU 主因，但後續應整理訂閱 source
  artifact 狀態，避免路徑語意混亂。
- Mihomo `-t` 的 30s 級成本不屬於 pack index 問題，仍需另外做驗證快取或
  runtime restart 優化。

## 工具覆蓋

### 快速讀取與建構工具

這些工具在約 0.7s 到 2.2s 內完成，採樣窗口中沒有看到持續的 localClash CPU 壓力。下表按耗時由短到長排列：

| 工具 | 時間 |
| --- | ---: |
| `config_configure` | 747ms |
| `subscriptions_configure` | 886ms |
| `config_status` | 894ms |
| `sed_file` | 899ms |
| `policy_group_build` | 910ms |
| `nl_file` | 924ms |
| `tools_list` | 929ms |
| `custom_rules_build` | 940ms |
| `runtime_profile_status` | 961ms |
| `subscriptions_status` | 962ms |
| `subscription_nodes_search` | 966ms |
| `proxy_group_build` | 967ms |
| `rule_provider_build` | 1004ms |
| `subscription_nodes_list` | 1090ms |
| `doctor` | 1136ms |
| `runtime_status` | 1236ms |
| `environment_inspect` | 1332ms |
| `packs_get` | 1429ms |
| `pack_rules_read` | 1520ms |
| `pack_rules_query` | 1531ms |
| `packs_list` | 1570ms |
| `router_takeover_status` | 1687ms |
| `pack_rules_prefetch` | 2223ms |

### 慢速讀取工具

`routing_explain` 成功完成，但對一般讀取路徑來說成本過高。

觀察到的時間：

- 第一次矩陣執行：16756ms
- 聚焦重跑：26888ms

聚焦 CPU 採樣顯示，請求執行期間 `localclash` 約在 83% 到 136% 之間。

解讀：

- `routing_explain` 目前正在執行很重的設定意圖解析工作。
- 它不應該在資源受限的路由器上表現得像便宜的狀態/讀取工具。

## 帶階段日誌的執行工具

新的階段式任務日誌有效。長時間執行的工具現在會顯示時間花在哪裡，而不是只回報最後的 `done`。

### `subscriptions_refresh`

觀察到的時間：

- 21900ms

重要階段：

| 階段 | 時間 |
| --- | ---: |
| `fetch_source` for `sub1` | 2504ms |
| `write_source_artifact` for `sub1` | 143ms |
| `fetch_source` for `sub2` | 2359ms |
| `write_source_artifact` for `sub2` | 129ms |
| `read_artifacts` | 250ms |
| `write_merged_subscription` | 30ms |
| `load_subscription_nodes_after` | 244ms |
| `evaluate_localclash_impact` | 15481ms |

CPU 觀察：

- 這個工具執行期間，`localclash` 峰值約 127%。

解讀：

- 網路抓取和 YAML 寫入不是主要瓶頸。
- 昂貴的階段是刷新後的 localClash 影響評估。
- 這指向設定解析，而不是訂閱下載。

### `config_render`

觀察到的時間：

- 16828ms

重要階段：

| 階段 | 時間 |
| --- | ---: |
| `resolve_localclash_config` | 15024ms |
| `render_generated_config` | 526ms |
| `render_generated_config.render_pack_selection` | 471ms |
| `render_generated_config.write_output` | 29ms |

CPU 觀察：

- `localclash` 峰值約 108%。

解讀：

- 渲染本身相對便宜。
- 將 `localclash.json` 解析成已選 pack、proxy group、policy group 和 custom rule 是主要成本。

### `config_patch_create`

觀察到的時間：

- 16905ms

重要階段：

| 階段 | 時間 |
| --- | ---: |
| `resolve_candidate_config` | 15051ms |
| `render_candidate` | 568ms |
| `render_candidate.render_pack_selection` | 513ms |
| `write_summary` | 1ms |

CPU 觀察：

- `localclash` 峰值約 125%。

解讀：

- 建立 patch 和 `config_render` 有相同的解析瓶頸。
- 候選渲染和摘要寫入不是問題。

### `config_patch_apply`

觀察到的時間：

- 16681ms

重要階段：

| 階段 | 時間 |
| --- | ---: |
| `resolve_apply_config` | 14964ms |
| `render_candidate` | 531ms |
| `backup_apply_targets` | 1ms |
| `write_active_config` | 14ms |

CPU 觀察：

- `localclash` 峰值約 118%。

解讀：

- 套用 patch 有相同的解析瓶頸。
- 檔案寫入與備份操作不是主要貢獻者。

### `run_runtime`

觀察到的時間：

- 37273ms

重要階段：

| 階段 | 時間 |
| --- | ---: |
| `config_test` | 35941ms |
| `start_process` | 1ms |

CPU 觀察：

- `mihomo-meta` 峰值約 183%。

解讀：

- 驗證完成後，runtime 啟動很快。
- 慢的操作是 Mihomo 設定驗證。

### `restart_runtime`

觀察到的時間：

- 33316ms

重要階段：

| 階段 | 時間 |
| --- | ---: |
| `config_test` | 32527ms |
| `stop` | 1ms |
| `start` | 2ms |
| `status` | 1ms |

CPU 觀察：

- `mihomo-meta` 峰值約 192%。

解讀：

- 重啟已經不再是神祕路徑。
- 停止/啟動/狀態查詢幾乎可以忽略。
- `mihomo -t` 是主要成本。

### 路由器接管 Dry Run

觀察到的時間：

- `router_takeover_apply`: 1890ms
- `router_takeover_stop`: 1875ms

重要階段：

- `read_runtime_profile`: 3ms

解讀：

- 在 `dry_run=true` 且 `runtime_profile=normal` 時，這些工具會提早返回。
- 這不驗證真實防火牆修改成本。
- 真實接管測試仍應只放在隔離的 OpenWrt 環境。

### `stop_runtime`

觀察到的時間：

- 1877ms

重要階段：

| 階段 | 時間 |
| --- | ---: |
| `takeover_guard_status` | 744ms |
| `stop_runtime` | 31ms |

解讀：

- 停止本身很便宜。
- Guard status 可量測，但在這輪中不嚴重。

## CPU 發現

### localClash CPU

觀察到的熱點路徑：

- `routing_explain`
- `subscriptions_refresh` 階段 `evaluate_localclash_impact`
- `config_render` 階段 `resolve_localclash_config`
- `config_patch_create` 階段 `resolve_candidate_config`
- `config_patch_apply` 階段 `resolve_apply_config`

工作假設：

- `localconfig.Resolve` 正在對相同的訂閱、設定和 rule pack 資料重複執行昂貴工作。
- 這個成本大到足以在路由器上佔用超過一顆 CPU 的時間。

現在範圍已經收斂到足以停止把問題視為「MCP 伺服器普遍很貴」。

### Mihomo CPU

觀察到的熱點路徑：

- `run_runtime` 階段 `config_test`
- `restart_runtime` 階段 `config_test`

工作假設：

- 對目前生成設定、訂閱規模和 rule/provider 結構來說，Mihomo 設定驗證很昂貴。
- localClash 目前會在 runtime 啟動/重啟之前支付這個成本。

這是和 localClash 自身設定解析 CPU 不同的另一個問題。

### OpenClash / 背景雜訊

採樣期間，路由器上既有的 OpenClash `clash` 程序出現背景尖峰，有時超過 90%。這不能證明那些尖峰是 localClash 造成的。

有用的比較是：

- 特定 MCP 呼叫期間的 localClash CPU
- 這些呼叫完成後的 localClash 閒置 CPU
- 設定驗證或 runtime 預熱期間的 Mihomo CPU

## 產品影響

目前的階段式日誌是必要改善，因為它讓 Agent 可以回答：

- 目前正在執行哪個階段
- 哪個階段失敗
- 每個階段花了多久
- 長時間等待是 localClash 解析還是 Mihomo 驗證

但階段式日誌本身還不夠。幾個面向使用者的操作對資源受限的路由器來說仍然太昂貴：

- "explain routing" 可能超過 20 秒。
- "render config" 約 17 秒，而且大多花在真正渲染之前。
- "patch create/apply" 約 17 秒，而且大多花在真正渲染/寫入之前。
- "run/restart runtime" 超過 30 秒，而且大多花在 Mihomo `-t`。

## 建議討論點

### 1. Pack index runtime contract 已解決主要 `localconfig.Resolve` 瓶頸

已完成：

- 將 pack runtime protocol 從 YAML catalog / directory scan 替換為
  `index.gob`。
- `localconfig.Resolve` 改為單次載入 `PackIndex`。
- runtime 缺失或 schema mismatch 時 hard fail，而不是 fallback。
- 常見 resolve 路徑在真實路由器上已低於 2s。

後續應避免重新引入：

- runtime YAML catalog fallback。
- 每個 pack resolve 重新讀檔或掃目錄。
- 把 debug 手段混入 runtime hot path。

### 2. 將便宜狀態和完整稽核拆開

`config_status` 預設已經很輕量。同樣原則也應該套用到 `routing_explain`。

修正後狀態：

- `routing_explain` 在目前真機配置已降到 537-880ms。
- 目前不再是首要 CPU 熱點。

仍可保留的產品方向：

- `routing_explain` 預設模式應使用既有的持久化 metadata，避免完整 selector resolution。
- 為昂貴的完整證明加入 `detail=true` 或 `resolve=true`。
- 在避開重工作時回傳清楚的 `resolve_skipped` 標記。

成功目標：

- 基本 routing explanation 低於 2s。
- 完整證據模式可以仍然較慢，但必須明確。

### 3. 重新考慮何時需要 `mihomo -t`

目前的 preflight 安全，但昂貴。

可能做法：

- 設定變更時保留 `mihomo -t`。
- 如果 generated config hash 已經通過驗證，就跳過重複的 `-t`。
- 儲存驗證 metadata：
  - config SHA-256
  - core type/version
  - validation time
  - result
- 如果 generated config 和 core 都沒有變，`restart_runtime` 可以重用驗證結果。

成功目標：

- 未變更 runtime 的 restart 低於 3s。
- 設定變更後的第一次驗證仍可花 30s，但任務日誌必須說明正在驗證 Mihomo 設定。

### 4. 保持 Runtime Mutation 和 Router Takeover 分離

這仍然是正確的：

- `run_runtime` 只啟動 Mihomo。
- `restart_runtime` 只重啟 Mihomo。
- `router_takeover_apply` 修改 firewall/DNS/policy-routing 狀態。
- `router_takeover_stop` 還原 takeover 狀態。

不要在沒有階段日誌的情況下，把它們合併成一個不透明動作。

### 5. 加入可重複的效能 Harness

這個臨時腳本應該變成 repo 腳本。

建議腳本：

- `scripts/mcp-tool-perf-smoke.mjs`

它應該收集：

- 工具名稱
- 經過時間
- 任務狀態
- 階段耗時
- `localclash` max CPU
- `mihomo` max CPU
- `clash` background max CPU
- output JSON artifact

成功目標：

- 一條命令可以對路由器或測試 OpenWrt VM 重現這份報告。

## 立即下一步

最高價值的下一個工程步驟已不再是 pack resolution。`index.gob` 已經把主要
localClash CPU 熱點降到可接受範圍。

下一步建議：

1. 修正訂閱 source artifact 狀態，避免 `.runtime/subscriptions/sub1.gob`
   缺失時長期 fallback 到 `subscription.gob`。
2. 為 Mihomo `-t` 設計驗證快取，因為它仍是 `run_runtime` 和
   `restart_runtime` 的主要成本。
3. 建立可重複的 MCP performance smoke 腳本，把本報告中的真機測試流程固定下來。
