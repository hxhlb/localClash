# geox-url 與 GEO data 自動更新計劃

## Context

`geox-url` 是 Mihomo 下載 GEO data 文件（geoip.dat, geosite.dat, Country.mmdb, ASN.mmdb）的 URL 配置。目前 localClash default profile 的 4 個 URL 統一指向 `gh-proxy.com` 包裝後的 GitHub raw/release URL。這是一個短期收斂決策，原因如下：

- **路由器無代理探測顯示 `gh-proxy.com` 是當前候選中唯一同時支援 TLS 且返回 200 的 GitHub mirror**
- **避免 YAML 同時依賴 `testingcf.jsdelivr.net` 和 `gh-proxy.com`，把首次啟動失敗面收斂到單一 mirror**
- **Mihomo 本身只接受每個類型一個 URL**，無法配置備用鏡像
- **localClash 沒有運行時 geodata 下載/刷新機制**——只有構建腳本 `build-release-assets.sh` 有多鏡像回退鏈，但這僅在發佈時使用
- **影響面廣**：geodata 缺失會導致 GEOIP/GEOSITE 規則失效、DNS fallback-filter 損壞、路由器模式下幾乎所有分流邏輯癱瘓

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

### 中期：本地 mini HTTP 鏡像轉發

更穩妥的方向是在 localClash 側提供一個 mini HTTP 本地轉發程序。Mihomo 仍然只看到單一 URL，但 localClash 在本地負責鏡像選擇和回退：

```yaml
geox-url:
  geoip: "http://127.0.0.1:8787/geodata/geoip.dat"
  geosite: "http://127.0.0.1:8787/geodata/geosite.dat"
  mmdb: "http://127.0.0.1:8787/geodata/Country.mmdb"
  asn: "http://127.0.0.1:8787/geodata/ASN.mmdb"
```

mini HTTP 程序需要負責：

- 維護每個 GEO data 文件的一組 GitHub / CDN 鏡像候選。
- 啟動時探測可用鏡像，按可用性和延遲選擇。
- 支援 timeout、fallback、重試和錯誤分類。
- 支援本地 cache，避免每次 Mihomo 請求都重新打遠端。
- 盡量保留 ETag / Last-Modified 語義，讓 Mihomo 的更新檢查保持便宜。
- 比 Mihomo 先啟動，或確保 base assets 已經存在，避免 Mihomo 缺檔同步下載時打不到本地轉發服務。

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

### 步驟 3：新增 mini HTTP geodata proxy

- 新增本地 HTTP handler，例如 `/geodata/{asset}`。
- 實作每個 asset 的鏡像候選列表。
- 復用 `scripts/build-release-assets.sh` 中已經驗證過的鏡像順序作為初版策略。
- 支援 cache metadata，包含來源 URL、hash、ETag、Last-Modified、更新時間。
- 將 runtime profile 的 `geox-url` 切換到 `127.0.0.1` 本地 URL。

### 步驟 4：可選 CLI repair/update

- 可以保留一個 `localclash geodata update` 或 `localclash geodata repair` 命令。
- 這個命令直接使用 mini HTTP proxy 的鏡像選擇邏輯，讓手動修復和背景轉發共用同一套下載策略。

---

## 關鍵文件

| 文件 | 改動 |
|------|------|
| `internal/runtimeprofile/profiles/*.default.json` | 開啟 `geo-auto-update` 並設定 interval |
| `internal/runtimeprofile/profile_test.go` | 鎖定 default profile 的 GEO update contract |
| `internal/doctor/doctor.go` | 後續增加 GEO data 完整性檢查 |
| `internal/geodata/` | 後續新增 mini HTTP proxy 和共用下載策略 |
| `scripts/build-release-assets.sh` | 作為鏡像候選順序的既有參考 |

## 驗證方式

1. `go test ./internal/runtimeprofile/...`：確認 default profiles 帶有 auto update 欄位。
2. `go run . config render --force`：確認 `generated/mihomo.yaml` 會輸出 `geo-auto-update` 與 `geo-update-interval`。
3. `go run . doctor --json`：確認現有 runtime validation 不受影響。
4. 未來 mini HTTP proxy 完成後，模擬 CDN 不可用場景，確認 Mihomo 的單一本地 URL 仍能透過可用鏡像完成更新。
