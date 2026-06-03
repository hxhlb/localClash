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
| localClash Core | [v0.1.38](https://github.com/qoli/localClash/releases/tag/v0.1.38) | 2026-06-04 01:57 UTC+8 |
| localclash-luci | [v0.1.0-30](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-30) | 2026-06-04 02:23 UTC+8 |

## 2026-06-04

### localClash Core v0.1.30, v0.1.35-v0.1.38

Changes:

- 自 2026-06-02 Telegram 公告後，Core 已累積到 `v0.1.38`；本次整合
  `v0.1.30` 補發資產，以及 `v0.1.35` 到 `v0.1.38` 的正式更新。
- 訂閱輸入與多來源辨識補齊：支援 proxy URI 訂閱來源，節點名前綴優先使用
  `display_name`，多個訂閱來源更容易看懂。
- MCP 與下載更穩：工具調用不再接收 caller 傳入的 server-owned 路徑；
  amd64 Mihomo core 改用更保守的 `amd64-v1` 資產，降低老設備兼容風險。

Release:

- [qoli/localClash v0.1.30](https://github.com/qoli/localClash/releases/tag/v0.1.30)
- [qoli/localClash v0.1.35](https://github.com/qoli/localClash/releases/tag/v0.1.35)
- [qoli/localClash v0.1.36](https://github.com/qoli/localClash/releases/tag/v0.1.36)
- [qoli/localClash v0.1.37](https://github.com/qoli/localClash/releases/tag/v0.1.37)
- [qoli/localClash v0.1.38](https://github.com/qoli/localClash/releases/tag/v0.1.38)

Release assets:

- `localclash-linux-amd64`
- `localclash-linux-arm64`
- `localclash-base-assets.tar.gz`
- `localclash-release-manifest.json`
- 對應的 `.sha256` checksum 文件

Verification:

- GitHub Release `v0.1.38` 已包含 linux amd64/arm64、base assets、
  release manifest 和 checksum assets。

### localclash-luci v0.1.0-22-v0.1.0-30

Changes:

- LuCI 已進入可公開使用的小白流程：下載、安裝、訂閱、啟動都收斂在 LuCI
  頁面，不需要先理解 mihomo YAML、rules 或 proxy-groups。
- 概覽頁重做為摘要表格，會背景檢查 LuCI / Core 更新；進階頁保留 Core、
  Mihomo、Dashboard 等組件級維護。
- `一鍵更新` 串起 LuCI、localClash Core、Mihomo、Dashboard、訂閱刷新、
  配置重建、MCP 服務檢查與網路接管恢復。
- `v0.1.0-30` 起，訂閱刷新失敗時可明確使用既有 merged subscription cache
  繼續，但仍必須通過 config render 和 `mihomo config-test` 才會切換 runtime。

Release:

- [qoli/localclash-luci v0.1.0-22](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-22)
- [qoli/localclash-luci v0.1.0-23](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-23)
- [qoli/localclash-luci v0.1.0-24](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-24)
- [qoli/localclash-luci v0.1.0-25](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-25)
- [qoli/localclash-luci v0.1.0-26](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-26)
- [qoli/localclash-luci v0.1.0-27](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-27)
- [qoli/localclash-luci v0.1.0-28](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-28)
- [qoli/localclash-luci v0.1.0-29](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-29)
- [qoli/localclash-luci v0.1.0-30](https://github.com/qoli/localclash-luci/releases/tag/v0.1.0-30)

Release assets:

- `luci-app-localclash_0.1.0-30_all.ipk`
- `luci-app-localclash_0.1.0-30_all.ipk.sha256`
- `luci-app-localclash-0.1.0-r30.apk`
- `luci-app-localclash-0.1.0-r30.apk.sha256`

Verification:

- `v0.1.0-22` 到 `v0.1.0-30` release notes 持續記錄 LuCI JavaScript
  `node --check`、rpcd helper syntax check、IPK/APK build 與 checksum
  驗證。
- Docker OpenWrt 已安裝 `luci-app-localclash 0.1.0-30`，並透過 ubus
  驗證 `one_click_update`、`core_update_check` 和 `luci_update_check`。

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

正式發送預設會附加本機更新圖：

```text
telegram/localclash-telegram-update.png
```

如需只發文字，可以使用：

```bash
scripts/telegram-channel-update.py --no-image
```

Bot token 讀取順序：

1. `TELEGRAM_BOT_TOKEN`
2. `telegram/.token`
3. `/Volumes/Data/Github/SyncnextProjects/Syncnext/telegram/.token`

固定公告頭部維護在 `telegram/top.md`，腳本會把最新日期區塊提取出的
更新正文寫入 `telegram/changelog.md`。正式預覽或發送時，文字內容由
`telegram/top.md` 加上 `telegram/changelog.md` 組成。生成文件、本地
token 和發送記錄目錄都在 `.gitignore` 中，不能進入 Git 追蹤。
