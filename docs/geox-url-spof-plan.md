# geox-url 與 GEO data 自動更新計劃

## Context

`geox-url` 是 Mihomo 下載 GEO data 文件（geoip.dat, geosite.dat, Country.mmdb, ASN.mmdb）的 URL 配置。目前 localClash default profile 的 4 個 URL 統一指向 `gh-proxy.com` 包裝後的 GitHub raw/release URL。這是一個短期收斂決策，原因如下：

- **路由器無代理探測顯示 `gh-proxy.com` 是當前候選中唯一同時支援 TLS 且返回 200 的 GitHub mirror**
- **避免 YAML 同時依賴 `testingcf.jsdelivr.net` 和 `gh-proxy.com`，把首次啟動失敗面收斂到單一 mirror**
- **Mihomo 本身只接受每個類型一個 URL**，無法配置備用鏡像
- **localClash 沒有運行時 geodata 下載/刷新機制**——只有構建腳本 `build-release-assets.sh` 有多鏡像回退鏈，但這僅在發佈時使用
- **影響面廣**：geodata 缺失會導致 GEOIP/GEOSITE 規則失效、DNS fallback-filter 損壞、路由器模式下幾乎所有分流邏輯癱瘓

### Core 已確定答案

localClash Core 已確定不採用 HTTP 改寫或本地轉發服務來解決 Mihomo 外部資源 URL 問題。`generated/mihomo.yaml` 是 localClash 的管理對象，因此 Core 應在 render 階段決定最終寫入 YAML 的 URL 或本地文件路徑。

這個邊界只適用於 Core 已安裝、可以執行 config render 的階段。LuCI bootstrap 下載 localClash Core 屬於另一個問題，不能依賴本節的 render 決策解決。

Core render 階段應保留三層語義：

- 原始上游意圖：例如 GitHub release/raw、GEO data、Smart model、外部 rule-provider 的 upstream URL。
- 可用性決策輸入：例如路由器網絡下的 probe/cache 結果、候選 mirror 健康度、資源類型。
- 最終 YAML 輸出：Mihomo 只接收普通 URL 或本地路徑，不感知 localClash 的鏡像選擇策略。

### Mihomo 行為邊界

- 缺檔或壞檔時，Mihomo 會在 GEO 規則初始化期間同步下載對應文件。
- `geo-auto-update: true` 開啟後，Mihomo 會在背景 ticker 中按 `geo-update-interval` 下載、驗證並覆蓋 GEO data。
- GEO auto update 不會重啟 Mihomo process，也不會重新套用整份 config。更新成功後只會清除 GeoIP/GeoSite matcher cache，或 reset MMDB/ASN reader。
- 既有連線通常不會被重新判路由；新連線或後續規則匹配會在 cache 重建後使用新資料。

### 當前數據流

```
構建時: build-release-assets.sh -> 多鏡像下載 -> base-assets.tar.gz -> GitHub Releases
安裝時: baseassets.Install() -> 下載 base-assets.tar.gz -> 解壓到 .runtime/mihomo/
啟動時: Mihomo 使用本地 GEO data；缺檔或壞檔時才從 geox-url 補下載
運行時: geo-auto-update=true 時，Mihomo 按 geo-update-interval 從 geox-url 背景刷新
```

## 建議方案

### 短期：統一到 gh-proxy.com 並接受 Mihomo 原生 auto update

短期先在 default runtime profiles 中顯式開啟 Mihomo 的 GEO auto update，並讓 GEO data 與 Smart `lgbm-url` 使用同一個 GitHub mirror 域名：

```yaml
geodata-mode: true
geodata-loader: memconservative
geo-auto-update: true
geo-update-interval: 24
etag-support: true
geox-url:
  geoip: "https://gh-proxy.com/https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat"
  geosite: "https://gh-proxy.com/https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"
  mmdb: "https://gh-proxy.com/https://raw.githubusercontent.com/alecthw/mmdb_china_ip_list/release/Country.mmdb"
  asn: "https://gh-proxy.com/https://github.com/xishang0128/geoip/releases/latest/download/GeoLite2-ASN.mmdb"
```

這個階段的取捨：

- 優點：實作小，立即讓 GEO data 可以背景更新。
- 優點：router 無代理首次啟動只依賴一個 GitHub mirror 域名，失敗模式更少。
- 優點：不重啟 Mihomo，對既有流量基本無感。
- 風險：仍然依賴單一 mirror，鏡像不可用時只會更新失敗並記錄錯誤。
- 風險：更新時會有短暫下載、驗證、IO、matcher cache 重建成本。

### 已拒絕：本地 mini HTTP 鏡像轉發

本地 mini HTTP 轉發不再作為 Core 方向。它會把外部資源下載變成 `Mihomo -> localClash HTTP 服務 -> 外網` 的常駐依賴，讓服務啟動順序、端口、DNS、路由與本地代理可用性都變成新的故障面。

替代方向是：localClash 在 render 階段根據資源類型和 probe/cache 結果，直接把可用 URL 或本地路徑寫入 `generated/mihomo.yaml`。如果 Mihomo 原生欄位只能接受單一 URL，那個單一 URL 也應在 render 前被選定，而不是在運行時由本地 HTTP 服務再改寫。

---

## 實施計劃

### 步驟 1：開啟 default profile auto update 並統一 mirror

- `internal/runtimeprofile/profiles/normal.default.json`
- `internal/runtimeprofile/profiles/router.default.json`
- 保留 `geodata-mode: true` 和 `geodata-loader: memconservative`
- 新增 `geo-auto-update: true`
- 新增 `geo-update-interval: 24`
- 顯式保留 `etag-support: true`
- 將 default `geox-url` 統一為 `gh-proxy.com` 包裝後的 GitHub raw/release URL

### 步驟 2：增強 doctor 檢查

- 檢查 `.runtime/mihomo/` 下 4 個 GEO data 文件是否存在、非空、可讀。
- 缺失時提示 base assets 安裝或未來的 geodata repair/update 命令。
- 對 router 模式，把 GEO data 缺失視為高風險 warning。

### 步驟 3：render 階段 URL 決策

- 盤點 Core 管理的所有 Mihomo 外部資源 URL：`geox-url`、Smart `lgbm-url`、外部 rule-provider URL。
- 按資源類型保存 upstream URL 與候選 mirror，不把候選列表直接等同於可靠性。
- 使用路由器網絡下的 probe/cache 結果作為 render 決策輸入。
- 在 `generated/mihomo.yaml` 中只輸出已選定的普通 URL 或本地文件路徑。
- 在 plan/doctor 類輸出中展示 upstream、選中 URL、probe/cache 時間與失敗原因，避免黑盒替換。

### 步驟 4：可選 CLI repair/update

- 可以保留一個 `localclash geodata update` 或 `localclash geodata repair` 命令。
- 這個命令應復用 Core 的資源 URL 決策與下載策略，讓手動修復、doctor 建議和 render 輸出使用同一份來源健康資料。

---

## 關鍵文件

| 文件 | 改動 |
|------|------|
| `internal/runtimeprofile/profiles/*.default.json` | 開啟 `geo-auto-update` 並設定 interval |
| `internal/runtimeprofile/profile_test.go` | 鎖定 default profile 的 GEO update contract |
| `internal/doctor/doctor.go` | 後續增加 GEO data 完整性檢查 |
| `internal/configrender/` | 在 render 階段輸出已決策的外部資源 URL 或本地路徑 |
| `scripts/build-release-assets.sh` | 作為既有多鏡像下載與資源分類參考，不作為 runtime 代理方案 |

## 驗證方式

1. `go test ./internal/runtimeprofile/...`：確認 default profiles 帶有 auto update 欄位。
2. `go run . config render --force`：確認 `generated/mihomo.yaml` 會輸出 `geo-auto-update` 與 `geo-update-interval`。
3. `go run . doctor --json`：確認現有 runtime validation 不受影響。
4. 後續 render 階段 URL 決策完成後，模擬候選 mirror 失敗場景，確認 `generated/mihomo.yaml` 只輸出當前 probe/cache 選中的普通 URL 或本地路徑。
