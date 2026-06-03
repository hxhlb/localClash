# 更新日誌

這份文件記錄 localClash 產品層的使用者可見變更。它不是 GitHub
Release 頁面的替代品；Release 頁面仍是下載二進位文件、OpenWrt
package、checksum 和 manifest 的來源。

localClash 有兩條獨立的發佈渠道：

- **localClash Core**：Go runtime、MCP/CLI、release manifest、base assets。
  由 [qoli/localClash](https://github.com/qoli/localClash/releases) 發佈。
- **localclash-luci**：OpenWrt LuCI 頁面、rpcd helper、ACL、menu、IPK/APK。
  由 [qoli/localclash-luci](https://github.com/qoli/localclash-luci/releases) 發佈。

Core 發佈不一定需要 LuCI package 發佈。已安裝最新 LuCI package 的路由器，
可以在 LuCI 頁面裡直接更新 Core。

## 目前最新版本

| 渠道 | 最新版本 | 發佈時間 |
| --- | --- | --- |
| localClash Core | [v0.1.37](https://github.com/qoli/localClash/releases/tag/v0.1.37) | 2026-06-04 00:33 UTC+8 |
| localclash-luci | [v0.1.0-29](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-29) | 2026-06-04 01:22 UTC+8 |

## 2026-06-04

### localClash Core v0.1.37

Release:
[qoli/localClash v0.1.37](https://github.com/qoli/localClash/releases/tag/v0.1.37)

Changes:

- 多訂閱來源生成節點名稱時，優先使用訂閱來源的 `display_name` 作為節點前綴，
  例如 `[01] HK 01`，避免把較長的 source ID 暴露到代理組裡。
- 未設定 `display_name` 的舊來源會使用 source ID 的短前綴作為穩定 fallback，
  保持多來源重名節點仍可被區分。
- MCP 訂閱刷新和 local config 解析流程都會攜帶來源顯示名稱，讓 CLI/MCP
  兩條路徑解析訂閱節點時保持一致。

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- GitHub Release `v0.1.37` 已包含 linux amd64/arm64、base assets、
  release manifest 和 checksum assets。

### localclash-luci v0.1.0-29

Release:
[qoli/localclash-luci v0.1.0-29](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-29)

Changes:

- 新增新手向 `一鍵更新` 入口，串起 LuCI package、localClash Core、Mihomo
  Core、Dashboard、訂閱刷新、配置重建、MCP 服務檢查和網路接管恢復。
- 概覽頁仍在摘要表格內靜默檢查 LuCI/Core 更新狀態，只有偵測到可更新項目
  時才啟用 `一鍵更新` 按鈕。
- 保留進階頁的個別組件維護入口，熟悉流程的使用者仍可單獨更新 Core、
  Mihomo 或 Dashboard。

Release assets:

- `luci-app-localclash_0.1.0-29_all.ipk`
- `luci-app-localclash_0.1.0-29_all.ipk.sha256`
- `luci-app-localclash-0.1.0-r29.apk`
- `luci-app-localclash-0.1.0-r29.apk.sha256`

Verification:

- Release notes 記錄已完成 LuCI JavaScript `node --check`、rpcd helper
  syntax check，以及 one-click/bootstrap/takeover/update-check regression
  scripts。
- Release notes 記錄已建置 OpenWrt IPK/APK artifacts 並驗證 checksum。
- Docker OpenWrt 已安裝 `luci-app-localclash 0.1.0-29`，並透過 ubus
  驗證 `one_click_update`、`core_update_check` 和 `luci_update_check`。

### localClash Core v0.1.36

Release:
[qoli/localClash v0.1.36](https://github.com/qoli/localClash/releases/tag/v0.1.36)

Changes:

- 在訂閱來源狀態中加入顯示名稱，讓多個訂閱來源更容易辨識。
- 將支援入口調整為 USDT 支援方式。
- 更新 GitHub Actions release workflow 使用的 action 版本，維持 Core
  release pipeline 可用。

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

### localclash-luci v0.1.0-28

Release:
[qoli/localclash-luci v0.1.0-28](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-28)

Changes:

- 在概覽摘要區新增靜默背景更新檢查。
- `LuCI 界面` 行會檢查目前 LuCI package 是否已是最新版本。
- `localClash 核心` 行放在 `LuCI 界面` 下方，會讀取 Core release
  manifest 並在有更新時啟用 `更新` 按鈕。
- Core 更新按鈕沿用現有 `component_update_async("localclash")` 更新路徑，
  不在前端引入另一套安裝流程。
- 調整摘要表格視覺：文字預設左對齊，`.cbi-rowstyle-1` 改為低干擾的
  黑色 alpha 0.1 背景。

Release assets:

- `luci-app-localclash_0.1.0-28_all.ipk`
- `luci-app-localclash_0.1.0-28_all.ipk.sha256`
- `luci-app-localclash-0.1.0-r28.apk`
- `luci-app-localclash-0.1.0-r28.apk.sha256`

Verification:

- Docker OpenWrt 24.10.2 installed `luci-app-localclash 0.1.0-28`.
- `luci_update_check` returned `update_available: false` for `0.1.0-28`.
- `core_update_check` detected Core `v0.1.36` from the latest release manifest
  and enabled the Core update path when the installed binary was older.

## 維護規則

新增 release 時，按下面順序更新這份文件：

1. 更新「目前最新版本」表格。
2. 增加一個以本地日期為標題的段落。
3. 分別列出 Core 與 LuCI 的變更；沒有發佈的 channel 不需要新增條目。
4. 只寫使用者或維護者需要知道的變更，不逐字複製 commit log。
5. 若 release 影響安裝、更新、manifest、OpenWrt package 或路由器行為，
   補上驗證證據。

## Telegram 頻道通知

Telegram 更新通知由 `telegram/top.md` 的固定頭部，加上本文件的最新日期
區塊生成：

```bash
scripts/telegram-channel-update.py --dry-run
```

預設頻道與 Syncnext 相同，為 `@RonnieAppsChannel`。正式發送時：

```bash
scripts/telegram-channel-update.py
```

正式發送預設會附加本機手繪 16:9 更新圖：

```text
telegram/out/localclash-telegram-update-handdrawn-16x9.png
```

如需只發文字，可以使用：

```bash
scripts/telegram-channel-update.py --no-image
```

Bot token 讀取順序：

1. `TELEGRAM_BOT_TOKEN`
2. `telegram/.token`
3. `/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token`

固定公告頭部維護在 `telegram/top.md`，可使用 `{date}` 佔位符輸出最新
changelog 日期。腳本會把生成的 Telegram Markdown 寫入
`telegram/changelog.md`。生成文件、預設附圖、本地 token 和發送記錄目錄
都在 `.gitignore` 中，不能進入 Git 追蹤。
