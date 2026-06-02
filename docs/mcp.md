目前 localClash MCP 工具可以理解成 6 組，核心目標是讓 Agent 能「先觀察、再規劃、再生成、最後執行」，避免一上來就動路由器網路。

**1. 自我發現與環境診斷**

- `tools_list`：給不會展示 MCP registry 的 Agent 自己查工具清單。
- `environment_inspect`：看主機、網路能力、localClash 狀態，不暴露憑證。
- `doctor`：做 read-only 診斷。
- `nl_file`：帶行號讀檔，讓 Agent 能引用證據或追 task log。
- `sed_file`：repo-local 文本修補，預設 dry-run。

服務場景：Agent 剛連上 MCP 時，先判斷「這是什麼、能不能動、風險多大」。

**2. 訂閱與節點素材**

- `subscriptions_status`：看訂閱來源與本地 effective subscription 狀態。
- `subscriptions_configure`：寫入訂閱來源，不立即刷新。
- `subscriptions_refresh`：下載訂閱、生成本地 artifact、合併成 `subscription.gob`。
- `subscription_nodes_list`：列出節點摘要。
- `subscription_nodes_search`：按名稱搜尋節點。

服務場景：小白給訂閱 URL 後，Agent 建立節點池，並能查 HK/JP/US 等節點候選。

**3. 規則包與規則證據**

- `packs_list`：查有哪些可用規則包。
- `packs_get`：用 `{source, pack}` 看單個規則包詳情。
- `pack_rules_prefetch`：下載 provider rules 到本地 cache。
- `pack_rules_read`：用 `{source, pack}` 讀某個 pack 的規則，必要時下載。
- `pack_rules_query`：在本地 cache 搜 domain/keyword，回答「某個域名應該落在哪類規則」。

服務場景：Agent 不憑空猜 Telegram、Steam、AI、Apple，而是查 rule pack 的實際規則證據。
這組工具的 pack selector 只接受 `{source, pack}`，例如
`{"source":"sukkaw","pack":"ai"}`；`packs_list` / `packs_get` 會返回可直接複製
的 `tool_args`。`sukkaw_ai`、`syncnext_SyncnextProxy` 這類 composite provider
或 renderer 名稱不是 MCP selector，只能出現在明確讀取 generated config/file 的
原始 Mihomo 內容裡。

**4. 配置狀態、構建與 Patch 工作流**

- `config_status`：看 `patches/*.json` registry、compiled `localclash-intent.json`、`.runtime/mihomo/config.yaml`、render readiness。預設輕量，`patches=true/resolve=true/detail=true` 才做重查或列 patch。
- `config_configure`：改核心、runtime profile、policy template；policy template 會 import 成 `patches/*.json`，再 build compiled intent。
- `proxy_group_build`：建立出口組，例如 HK/JP/US/⚡ 自动选择。
- `policy_group_build`：建立業務組，例如 Steam -> HK/JP/US。
- `custom_rules_build`：建立自訂 domain/CIDR/GEOIP 規則。
- `rule_provider_build`：建立外部 rule-provider intent。
- `config_patch_get`：讀取一個 durable patch 的完整 overlay 與 hash。
- `config_patch_draft`：用 `upsert_patch/remove_patch/set_patch_status/reorder_patch` 預覽 patch registry 操作，只保留一個 in-memory draft。
- `config_patch_apply`：套用 current draft 或顯式 operations，寫入 `patches/*.json` 並重建 compiled intent / generated config。
- `config_render`：從 durable patch registry 或 compiled intent 重新渲染 `.runtime/mihomo/config.yaml`。

服務場景：這是現在的「patch registry first」模型。Agent 先構建候選 overlay，再 draft patch operation 給用戶/自己審核，最後 apply，不直接亂改 compiled artifacts。

**5. 路由解釋與可理解性**

- `routing_explain`：解釋某個服務、域名、pack、policy group、出口在 localClash compiled intent 裡應該怎麼路由。這是 config/intent evidence，不證明 Mihomo runtime 已載入，也不證明當前流量正在使用。
- `runtime_profile_status`：看當前 Meta/Smart、normal/router 等 runtime profile。
- `runtime_status`：看 Mihomo 是否運行、PID、controller/UI endpoint。
- `router_takeover_status`：看 OpenWrt firewall/nft/DNS hijack/fwmark/TUN 接管狀態。

服務場景：回答「現在 Telegram 為什麼不走代理」、「Steam 最終落到哪個出口」、「是否已接管路由器網路」。

**6. Mihomo Runtime API 與執行型工具**

- `run_runtime`：啟動 Mihomo。
- `mihomo_config_test`：顯式驗證 server state 中的 generated config，通過後記錄 config SHA256 attestation，供 hot reload 校對使用。MCP caller 不傳 config path、runtime dir、core binary 或 attestation path；非標準路徑檢查應走 CLI/SSH 診斷。
- `mihomo_api_request`：只通過本地已配置的 Mihomo controller 呼叫 bounded API path；拒絕完整 URL，不能作為通用 HTTP client。推薦用 `/version`、`/configs`、`/rules`、`/providers/rules`、`/proxies` 查 loaded runtime evidence；`/connections/` 存在，但 active connection 摘要優先用 `mihomo_connections_read`。
- `mihomo_connections_read`：讀取 bounded Mihomo active connection snapshot；預設用 `GET /connections/` 做一次性觀測，`mode=stream` 才使用 WebSocket `/connections/` 讀取有限幀。用於回答「當前活躍連接」的命中規則與 selected proxy chain；沒有某個 domain 的 active connection，不代表未來連接不會匹配該規則。
- `mihomo_logs_read`：從 Mihomo controller 讀取 bounded WebSocket/HTTP stream logs，不要求 caller 傳 token，也不輸出 token。
- `restart_runtime`：MCP 預設 hot reload。它只校對已通過 `mihomo_config_test` 的 config hash，然後呼叫 Mihomo `PUT /configs`。Mihomo reload 是同步長操作；request timeout 只能表示結果不確定，不等於 reload 失敗。工具不做配置語義驗證，Agent 應根據本次改動用 `mihomo_api_request` 查 `/rules`、`/providers/rules`、`/proxies` 或 `/configs`。若要 stop/start，必須顯式傳 `strategy=process_restart`。
- `stop_runtime`：停止 Mihomo；如果 router takeover 生效，預設拒絕，避免斷網。
- `router_takeover_apply`：套用 localClash 管理的 OpenWrt runtime 接管規則。
- `router_takeover_stop`：撤銷 localClash 管理的接管規則，不停止 Mihomo。

服務場景：真正改變運行狀態或路由器網路狀態，所以是 `confirm_required`。這組現在也會輸出階段性 task log，Agent 可以追 `stop -> start -> status` 或 takeover script/verify 的進度。

**推薦 Agent 流程**

**Runtime evidence ladder**

1. `routing_explain`：配置意圖，回答「localClash 編譯後應該怎麼走」。
2. `mihomo_api_request`：已載入 runtime，查 `/rules`、`/providers/rules`、`/proxies`、`/configs`。
3. `mihomo_connections_read`：active data plane，查目前連接實際命中的 rule / selected proxy chain。
4. `mihomo_logs_read` 或製造新流量：當沒有 active connection，但需要 runtime evidence 時使用。

1. `tools_list` + `environment_inspect`
2. `subscriptions_status` / `config_status` / `runtime_status`
3. 需要規則證據時走 `packs_list` / `pack_rules_prefetch` / `pack_rules_query`，pack 參數使用 `{source, pack}`
4. 需要修改配置時走 `proxy_group_build` / `policy_group_build` / `custom_rules_build`
5. 用 `config_patch_draft` 產生 current draft
6. 用 `config_patch_apply` 套用
7. 用 `config_render` 或直接由 apply 產生 `.runtime/mihomo/config.yaml`
8. 用 `mihomo_config_test` 對即將載入的 config 做顯式驗證
9. 經用戶確認後 `restart_runtime`；預設 hot reload，若需要進程重啟才傳 `strategy=process_restart`
10. 若 hot reload timeout，視為 `indeterminate`，不要推斷失敗或自動 process restart；按本次改動語義用 `mihomo_api_request` 做追查
11. 路由器接管只在明確確認後 `router_takeover_apply`

長任務現在的觀測入口是：工具返回 `task_id`、`log_file`、`status_file`，Agent 應該用 `nl_file` 持續讀 log，而不是等待 MCP 一次性返回。
