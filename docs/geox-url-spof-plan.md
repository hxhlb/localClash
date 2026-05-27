# geox-url 單點故障：建議方案

## Context

`geox-url` 是 Mihomo 運行時下載 geo 數據文件（geoip.dat, geosite.dat, Country.mmdb, ASN.mmdb）的 URL 配置。目前所有 4 個 URL 都指向 **同一個 CDN**：`testingcf.jsdelivr.net`。這是一個脆弱的單點故障位置，原因如下：

- **jsDelivr 在中國大陸的可用性不穩定**（GFW 干擾、DNS 污染、速度慢）
- **Mihomo 本身只接受每個類型一個 URL**，無法配置備用鏡像
- **localClash 沒有運行時 geodata 下載/刷新機制**——只有構建腳本 `build-release-assets.sh` 有多鏡像回退鏈，但這僅在發佈時使用
- **影響面廣**：geodata 缺失會導致 GEOIP/GEOSITE 規則失效、DNS fallback-filter 損壞、路由器模式下幾乎所有分流邏輯癱瘓

### 當前數據流

```
構建時: build-release-assets.sh → 多鏡像下載 → base-assets.tar.gz → GitHub Releases
安裝時: baseassets.Install() → 下載 base-assets.tar.gz → 解壓到 .runtime/mihomo/
運行時: Mihomo 讀取 geox-url → 從 testingcf.jsdelivr.net 重新下載 → 可能覆蓋已有文件
```

關鍵矛盾：**base assets 已經預置了 geodata 文件，但 Mihomo 的 geox-url 配置仍會在啟動時嘗試從單一 CDN 重新下載。**

---

## 建議方案

### 短期（低工作量，高影響）：新增 `localclash geodata update` 命令

復用 `build-release-assets.sh` 中已有的多鏡像回退邏輯，在 Go 代碼中實現一個 geodata 更新命令：

- **鏡像鏈**：每個文件嘗試多個 URL
  - `v1.ax/` + `ghp.xptvhelper.link/` + GitHub Releases 原始地址 + `testingcf.jsdelivr.net` 作為最後回退
- **目標目錄**：`.runtime/mihomo/`
- **使用方式**：`localclash geodata update`（手動觸發或 cron 定時任務）

這樣用戶可以可靠地刷新 geodata，而不依賴 Mihomo 的單 CDN 下載。

### 中期：增強 doctor 檢查 + geox-url 降級為可選

1. **doctor 增加 geodata 完整性檢查**：在 `internal/doctor/doctor.go` 中檢查 4 個 geodata 文件是否存在且有效，啟動 Mihomo 前發出警告
2. **`geox-url` 改為可選**：在 runtime profile 中增加開關，允許用戶禁用 Mihomo 自動下載（因為文件已由 localClash 管理）
3. **渲染時根據文件是否存在決定是否寫入 geox-url**：如果 localClash 確認文件已存在，則跳過 geox-url 配置，讓 Mihomo 直接使用本地文件

### 長期（可選）：考慮 geox-url 欄位改為列表

向 Mihomo 上游提交 feature request，讓 geox-url 支援多 URL 列表（如 `"geoip": ["url1", "url2"]`），實現 Mihomo 層面的原生回退。這是從根本上解決問題的方式，但需要上游支持。

---

## 實施計劃

### 步驟 1：新增 `internal/geodata/` 包

- 創建 `internal/geodata/download.go`
- 實現 `UpdateGeodata(runtimeDir string) error`
- 復用 `scripts/build-release-assets.sh` 的多鏡像邏輯：
  - `raw_github_mirrors()` → 用於 raw.githubusercontent.com 文件（Country.mmdb）
  - `github_release_mirrors()` → 用於 GitHub Releases 文件（geoip.dat, geosite.dat, ASN.mmdb）
- 每個文件依次嘗試所有鏡像，全部失敗才報錯

### 步驟 2：新增 CLI 子命令

- 修改 `main.go`，增加 `localclash geodata update` 命令
- 調用 `geodata.UpdateGeodata()`
- 支持 `--runtime-dir` flag（默認 `.runtime/mihomo`）

### 步驟 3：增強 doctor 檢查

- 修改 `internal/doctor/doctor.go`
- 增加 geodata 文件存在性檢查
- 增加文件基本有效性檢查（非空、可讀）
- 缺失時給出明確的修復指引：`localclash geodata update`

### 步驟 4（可選）：調整 geox-url 渲染行為

- 修改 `internal/configrender/render.go`
- 在渲染時檢查 `.runtime/mihomo/` 下 geodata 文件是否完整
- 如果完整，跳過 geox-url 輸出（Mihomo 直接使用本地文件）
- 增加 runtime profile 開關控制這一行為

---

## 關鍵文件

| 文件 | 改動 |
|------|------|
| `internal/geodata/download.go` | **新增**：多鏡像 geodata 下載邏輯 |
| `internal/geodata/download_test.go` | **新增**：測試 |
| `main.go` | **修改**：增加 `geodata update` 子命令 |
| `internal/doctor/doctor.go` | **修改**：增加 geodata 完整性檢查 |
| `internal/configrender/render.go` | **可選修改**：條件性跳過 geox-url |
| `internal/runtimeprofile/profiles/*.default.json` | **可選修改**：增加 geox-url 開關 |

## 驗證方式

1. `go test ./internal/geodata/...` — 單元測試
2. `go build . && ./localclash geodata update` — 手動驗證下載功能
3. `./localclash doctor` — 確認 geodata 檢查通過
4. 模擬 CDN 不可用場景：手動刪除 geodata 文件，屏蔽 jsdelivr，確認更新命令仍可通過鏡像成功
