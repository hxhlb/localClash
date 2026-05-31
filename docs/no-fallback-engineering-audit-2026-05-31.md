# No Fallback 工程審查

日期：2026-05-31

範圍：只檢查工程實現路徑。Mihomo DNS 設定、Mihomo 規則 `MATCH` 行為、
policy-template 路由語義都不在本次範圍內，因為它們屬於產品/配置語義，
不是隱藏的工程 fallback。

這份文檔是討論草稿。下面任何條目在沒有明確決策之前，都不應被視為已批准
實作的變更。

## 原則基線

這裡的 No Fallback 是用來識別隱藏 fallback 的審查視角，而不是要求所有
fallback 都必須移除。對每一個疑似 fallback，都應先確認：

1. 它保護的是哪一個具體失敗。
2. 顯式失敗是否可以接受。
3. 哪一條程式碼路徑負責這個行為。
4. 測試如何證明 fallback 沒有被靜默使用。

本次審查重點包括：

- legacy compatibility path
- silent default value
- placeholder data
- mock data in production path
- best-effort migration
- auto-created missing state
- catch-and-continue behavior
- fallback algorithm
- inferred provenance when source is unknown

## 討論條目

### NF-001：subscription artifact 中缺 name 的 proxy 被跳過，重名 normalization 需確認

證據：

- `BuildSubscriptionNodesFromArtifactsMeasured` 會跳過 name 為空的 proxy。
- 重名節點會透過 `uniqueNameMeasured` 自動改名，例如 `HK 01` 變成
  `HK 01 (2)`。

可能的 policy 衝突：

- proxy name 缺失不是 Mihomo 的規範配置，但現實中可能出現在不安全或不規範的
  Mihomo 下發配置中。這裡跳過 nameless proxy 是兼容這類 artifact，而不是
  invent local name。
- 這個兼容行為需要用 code comment 明確說明，避免被誤判成無意識 fallback。
- 重名自動改名會改變 subscription node 的有效身份。這不一定是錯誤 fallback，
  因為 Mihomo 要求 proxy name 唯一，而不安全或不規範的 subscription payload
  可能包含重名節點。這裡視為兼容這類 artifact 的 normalization contract；需要
  用 code comment 明確說明，且結果應可觀測。
- `source.ID` 為空不列為本條 violation：單源訂閱可以允許沒有 source id，因為此時
  不需要 source 前綴來區分來源。若相關程式碼對空 source id 的處理有問題，應作為
  single-source normalization/legacy migration 問題另外討論。

它保護的失敗：

- 不安全或不規範的 Mihomo subscription artifact 中出現缺少 proxy name 的節點。
- 上游 provider 或多源 merge 產生重名 proxy metadata。

需要討論的問題：

- nameless proxy 的兼容是否只需要註釋說明，還是也應在 stage stats/result 中
  明確暴露 skipped count？
- 如果上游重名很常見，多 source artifact 時是否應強制 source-prefixing？
- 同一個 source 內部的重名允許依 compatibility normalization 自動改名；剩餘問題
  是是否需要更清楚的 result/stage event。
- 自動改名是否需要在 result/stage event 中明確回報，而不只是計數 internal checks？

可能的測試形態：

- source artifact 中 proxy `name` 為空時可跳過，但測試應證明不會 invent local
  name，且 skipped count 可觀測。
- final node name 重複時，如果決策是允許 normalization，測試應證明結果會明確回報
  renamed count 或 renamed mapping。
- 單源且 source id 為空的配置，如仍屬支持語義，應有獨立測試覆蓋，避免被本條
  no-fallback 審查誤傷。

狀態：accepted compatibility, comment required。

### NF-002：Config plan overlay summary 忽略 pack index 載入錯誤

證據：

- `internal/configplan/plan.go` 呼叫
  `packIndex, _ = rules.LoadPackIndex(...)`。
- 如果載入失敗，summary 會退回 raw overlay fields。

可能的 policy 衝突：

- 這是 catch-and-continue behavior。missing 或 corrupt pack index 仍可能產生一份
  看起來合理的 summary。
- 但這個問題位於 plan review 的 summary 層；真正的 pack 解析已在
  `localconfig.Resolve` 和後續 `configrender.Render` 中進行。因此這不應阻斷
  render，而應避免靜默降級。

它保護的失敗：

- summary rendering 時 rules cache 缺失。

需要討論的問題：

- requested packs 需要 pack index，但 pack index 無法載入時，overlay summary
  應保留 raw overlay fields，同時輸出 warning。
- warning 是否應包含 pack index path、load error，以及受影響的 source/pack？
- 是否需要在 summary schema 中增加 unresolved 欄位，還是 `Result.Warnings` 足夠？

可能的測試形態：

- requested overlay 包含 packs 且 summary 補 metadata 時遇到 pack index load
  error，`Render` 不應因此 fail。
- `Result.Warnings` / `summary.json` 應包含明確 warning，指出 overlay summary
  使用 raw pack fields，且包含 pack index path 或 load error。
- 正常 pack index 載入與 resolved metadata path 不應新增 warning。

落地：

- `requestedOverlaySummary` 在 pack index 載入失敗時保留 requested raw
  source/pack/type/target fields。
- warning 進入 `Result.Warnings` / `summary.json`，包含 pack index load error。
- 覆蓋測試：`TestRequestedOverlaySummaryWarnsWhenPackIndexUnavailable`。

狀態：implemented warning。

### NF-003：Generated config stale detection 忽略缺失的 source files

證據：

- `internal/mcp/server.go` 透過比較 source file mtime 判斷 generated config 是否
  stale。
- 如果 `os.Stat(source)` 失敗，程式會 `continue`，不會把 output 標為 stale 或
  error。

可能的 policy 衝突：

- 這是 catch-and-continue behavior。source state 缺失可能被隱藏在「generated
  output 沒有 stale」的狀態後面。
- 這不一定影響正在運行的 Mihomo，因為 Mihomo 可能仍使用已生成且已載入的
  `generated/mihomo.yaml`。但它會讓 localClash 無法可信地判斷下一步是否能
  render/apply/restart，因此 `config_status` 應輸出 warning。

它保護的失敗：

- lightweight status check 中，某些 source file 可能是 optional 或尚未建立。

需要討論的問題：

- source file 缺失時，`config_status` 應保留 status response，但輸出 warning。
- warning 應說明：Mihomo 現有運行不一定受影響，但 localClash 無法完整驗證
  generated config freshness，也可能無法進一步 render/apply。
- status payload 是否也應包含具體 missing source paths，供 agent/operator 修復？

可能的測試形態：

- source path 缺失時，`config_status` 不應 fail，但 response warnings 應包含
  missing source path。
- warning 應能區分「Mihomo runtime may still be using existing generated config」
  和「localClash cannot safely determine freshness/proceed」。
- 非 `ENOENT` 的 stat error 也應被 surfaced 到 warning 或 status field，而不是忽略。

落地：

- `config_status.render.warnings` 與 top-level `warnings` 會列出無法檢查的
  generated source path。
- 缺失 source file 不會讓 `config_status` fail；warning 會說明 Mihomo 可能仍使用
  既有 generated config，但 localClash 無法完整驗證 freshness。
- `next_actions` 會要求先恢復或重建缺失 source artifact，避免在 freshness unknown
  時繼續引導新的 localClash 變更。
- 覆蓋測試：`TestToolsCallConfigStatusWarnsWhenGeneratedSourceIsMissing`。

狀態：implemented warning。

### NF-004：Config patch order allocation 跳過既有 invalid order IDs

證據：

- `internal/configpatch/patch.go` 會計算下一個 user `order_id`。
- 當既有 non-tombstoned record 有 invalid `order_id` 時，parse error 會被忽略，
  allocation 繼續。

可能的 policy 衝突：

- 這是在 corrupted registry state 上的 catch-and-continue behavior。

它保護的失敗：

- 格式錯誤的 patch registry record 阻塞新 patch 建立。

決議：

- 任何 invalid non-tombstoned registry record 都應阻塞新的 patch draft/apply。
- `config_patch_draft` 和 `config_patch_apply` 應直接回傳 explicit error，指出壞掉的
  patch id、invalid `order_id`、以及下一步應由 Agent 重新建立受影響 Patch。
- 這不適合走 `doctor` 自動修復。invalid `order_id` 代表 registry 的排序語義已經不可信，
  自動 normalize 可能把錯誤順序固化成新的 durable state。
- tombstoned records 是否可以保留 invalid `order_id` 可以獨立討論；但 active
  records 必須 fail fast。

可能的測試形態：

- active `order_id` invalid 時，新 patch creation fail。
- tombstoned invalid records 要嘛由測試明確允許，要嘛依決策走同一套 validation
  path 拒絕。
- error message 包含壞掉的 `patch_id` 和 `order_id`，並提示重新建立 Patch，而不是
  執行 doctor repair。

落地：

- `nextUserOrderID` 不再忽略 active record 的 invalid `order_id`。
- 當自動分配 order id 前遇到 invalid active record，`config_patch_draft` /
  `config_patch_apply` 會收到 explicit error，提示重建受影響 Patch 並顯式傳入有效
  `order_id`。
- 覆蓋測試：
  `TestPreviewOperationsRejectsAutoOrderAllocationWithInvalidActiveOrderID`。

狀態：implemented explicit error。

## 建議討論順序

1. Subscription artifact 品質：NF-001。
2. Status 和 planning 正確性：NF-002、NF-003、NF-004。

第一組最直接影響配置正確性和 provenance。剩餘 status/planning 條目影響的是
operator 在 apply changes 前，能否信任目前看到的 config state。
