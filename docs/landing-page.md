# Landing Page Plan

## 目標

為 localClash 建立一個面向 **OpenWrt 小白用戶** 的 landing page，部署在 GitHub Pages（`/docs` 目錄）。

## 受眾

- **主要**：OpenWrt 路由器用戶，想要一鍵透明代理，不想折騰 YAML
- **次要**：想了解 localClash 和 localclash-luci 關係的潛在用戶
- **非目標**：AI agent / MCP 用戶（放在「進階玩法」折疊區或獨立子頁面）

## 核心敘事

> 訂閱 → 啟動。兩步，就這麼簡單。

用戶心智路徑：

1. 打開 OpenWrt LuCI → Services → localClash
2. 貼上訂閱鏈接 → 點「開始初始化」
3. 透明代理就跑起來了

Landing page 的任務是讓用戶在看到 LuCI 介面之前就已經理解並信任這個流程。

## 頁面結構

### 1. Hero

- 大標題：一到兩句話講清楚 localClash 是什麼
- 一張 LuCI overview 截圖（`localclash-luci/docs/assets/luci-overview-running.png`）
- 兩個 CTA：
  - 主 CTA：「安裝到路由器」 → 跳到 Install 區塊
  - 次 CTA：「GitHub」 → 連結到 repo

### 2. 就是這麼簡單（How It Works）

三張卡片，對應 LuCI 的三個狀態畫面：

| 步驟 | 截圖 | 文案 |
|---|---|---|
| ① 填入訂閱 | overview 初始化前狀態（待補） | 貼上你的 Clash/Mihomo 訂閱鏈接 |
| ② 點擊初始化 | 任務進行中（待補） | 自動下載核心、渲染配置、啟動運行時 |
| ③ 搞定 | `luci-overview-running.png` | 透明代理已就緒，儀表板即時監控 |

### 3. 功能亮點（Features）

三列，每列一個 icon + 一句話：

- **透明代理**：nftables + DNS 劫持 + fwmark，全屋設備生效
- **自動分流**：ACL4SSR 風格規則，國內直連、海外代理，開箱即用
- **儀表板監控**：zashboard 即時查看連線、切換節點、測速

### 4. 安裝（Install）

- 一行指令：`opkg install luci-app-localclash`
- 架構支援表：OpenWrt 24.10+ / 25.12+，aarch64 / x86_64
- 簡短說明：安裝後在 LuCI Services 選單中出現，無需額外配置

### 5. 兩個專案（Two Projects）

簡短說明 core 和 LuCI 的關係，避免用戶混淆：

| localClash (Core) | localclash-luci (UI) |
|---|---|
| Go 核心，負責下載、配置、運行時 | OpenWrt LuCI 網頁管理介面 |
| CLI + MCP 管理界面 | 路由器上的圖形化控制台 |
| 跨平台 (macOS / Linux) | 僅 OpenWrt |

### 6. Footer

- 兩個 GitHub 連結（core + luci）
- License: MIT
- 簡短的一句話說明

## 技術方案

- **靜態 HTML + CSS**，無框架，一把梭
- 放在 `docs/landing/` 目錄
- 透過 GitHub Pages 部署（repo Settings → Pages → `/docs`）
- 響應式，手機友善（OpenWrt 用戶可能在手機上查閱）

## 非目標（本階段不做）

- ❌ MCP / AI agent 教學內容 → 後續獨立子頁面或文檔
- ❌ Logo / 品牌色定義 → 後續單獨處理，本階段用佔位色
- ❌ 多語言（i18n）→ 先做中文
- ❌ 部落格 / 文檔站 → 只做單頁 landing

## 待補截圖

目前只有 `luci-overview-running.png`，還需要補：

- 初始狀態截圖（空白訂閱框 + 初始化按鈕）
- 任務進行中截圖（live log 輸出）

## Open Questions

- [ ] Hero 區塊的具體標題文案
- [ ] 主色調（目前暫用 neutral gray，品牌色後續迭代）
- [ ] 是否需要一個簡短的動畫/gif 展示 LuCI 操作流程
