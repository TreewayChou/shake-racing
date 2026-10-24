# 🏁 百人搖搖競速 (100-Player Shake Racing)

支援單場最高 **100 人** 同時連線的派對競速遊戲。採用 **Go (`//go:embed`) 單一執行檔** 封裝後端高併發伺服器、內建純 Go QR Code 產生器、大螢幕 Kiosk 視窗與手機搖晃控制器網頁，並自動整合 **Cloudflare HTTPS Tunnel (`cloudflared`)**。

## ✨ 核心功能

1. **2 × 2 四種競賽模式（開賽前隨時切換，免重掃 QR Code）**
   * **個人馬拉松 × 限時制**（15 / 30 / 45 / 60 秒內累積搖晃次數最高者勝）
   * **個人馬拉松 × 競速制**（率先搖滿 100 / 200 / 300 / 500 下者勝，依手機本地完賽毫秒數排名）
   * **四隊大對決 × 限時制**（紅/藍/綠/黃四隊，以**隊伍平均每人搖晃數**推進＋全場個人 MVP Top 5）
   * **四隊大對決 × 競速制**（隊伍平均率先達標獲勝＋全場個人 MVP Top 5）
2. **防延遲與硬體公平機制**
   * 傳送累積總次數 (`totalShakes`) 防掉包漏計。
   * 手機本地精準計時 (`elapsedMs`) ＋ 賽末 **1.5 秒衝線結算緩衝期 (Grace Period)**。
   * G-Sensor 三軸加速度向量偵測 ＋ **80ms 防抖動冷卻**（統一高低階手機取樣上限）。
3. **百人現場防護與壓測工具**
   * 手機網頁 `/play` 為單一內聯 HTML（Gzip 壓縮後僅 **7.2 KB**），防止 100 人同時掃碼觸發 Cloudflare 限流。
   * 等候室支援「**開賽前熱身試搖**」：玩家搖手機時，大螢幕上的名字卡片會即時跳動發光。
   * 內建「**🤖 +10 機器人 / 補滿 100 人**」壓力測試按鈕。

---

## 🚀 啟動方式

### 1. 在 WSL2 / Linux Box PC (`i5-6100U`) 直接執行
```bash
./racing-linux-amd64
```
* **ARM64 (`RK3588`) 版本**：
  ```bash
  ./racing-linux-arm64
  ```
* **Windows 版本**：
  直接雙擊 `racing.exe`。

執行後程式會自動完成：
1. 啟動本機 WebSocket 與網頁伺服器（預設 Port `3000`）。
2. 自動在背景下載/啟動 `cloudflared` 建立合法 `https://xxxx.trycloudflare.com/play` 安全通道，並將 QR Code 顯示於大螢幕。
3. 自動以 `--app` 無邊框全螢幕模式（並解鎖音效自動播放）喚起大螢幕視窗。

### 2. 啟動參數

| 參數 | 預設值 | 說明 |
| --- | --- | --- |
| `-port <n>` | `3000` | HTTP / WebSocket 監聽的 Port（1–65535），避開被占用的 Port 時使用。 |
| `-no-tunnel` | 關閉 | 純區網模式：不啟動 Cloudflare Quick Tunnel，手機改連區網 `http://` 網址（此時 iOS 無法讀取 G-Sensor）。 |
| `-no-kiosk` | 關閉 | 僅啟動伺服器，不自動彈出大螢幕視窗；請自行以本機瀏覽器開啟 `http://localhost:3000/host`。 |
| `-public-url <url>` | （空） | 改用自訂公開網址（自有網域或反向代理），不建立 trycloudflare 通道；QR Code 會顯示此網址 + `/play`。 |
| `-h` | | 列出所有參數後結束。 |

> 大螢幕 `/host` 僅接受本機（loopback）連線，透過 Tunnel 或區網連入一律回應 403。

```bash
# 指定 Port
./racing-linux-amd64 -port 3000

# 純區網模式（不啟動 Cloudflare Tunnel）
./racing-linux-amd64 -no-tunnel

# 僅啟動伺服器（不自動彈出瀏覽器視窗）
./racing-linux-amd64 -no-kiosk

# 指定自訂公開 HTTPS 網址
./racing-linux-amd64 -public-url https://game.yourdomain.com
```

Windows（PowerShell）用法相同，例如：
```powershell
.\racing.exe -no-kiosk -no-tunnel -port 8080
```

---

## 🛠️ 重新編譯各平台單一執行檔

需求：Go 1.27 以上（不需 C 編譯器，全部以 `CGO_ENABLED=0` 純 Go 編譯）。建置腳本會**先跑 `go test ./...`，全數通過後**才交叉編譯出 3 個檔案：

| 檔案 | 平台 |
| --- | --- |
| `racing.exe` | Windows x86_64 |
| `racing-linux-amd64` | Linux x86_64 (i5-6100U) |
| `racing-linux-arm64` | Linux ARM64 (RK3588) |

網頁資源已透過 `//go:embed` 內嵌，每個執行檔都可單獨複製使用，不需任何外部檔案（`cloudflared` 會在執行時自動尋找或下載）。編譯產物已列入 `.gitignore`。

### Windows（PowerShell，原生）
```powershell
# 一鍵：測試 + 編譯 3 個平台
powershell -ExecutionPolicy Bypass -File .\build.ps1
```

手動逐一編譯：
```powershell
# 執行單元測試
go test -v ./...

$env:CGO_ENABLED = "0"

# 編譯 Windows (.exe)
$env:GOOS = "windows"; $env:GOARCH = "amd64"; go build -trimpath -ldflags="-s -w" -o racing.exe .

# 編譯 Linux x86_64 (i5-6100U)
$env:GOOS = "linux"; $env:GOARCH = "amd64"; go build -trimpath -ldflags="-s -w" -o racing-linux-amd64 .

# 編譯 Linux ARM64 (RK3588)
$env:GOOS = "linux"; $env:GOARCH = "arm64"; go build -trimpath -ldflags="-s -w" -o racing-linux-arm64 .

# 還原成本機平台
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
```

### Linux / macOS
```bash
# 一鍵：測試 + 編譯 3 個平台
make
```

手動逐一編譯：
```bash
# 執行單元測試
go test -v ./...

# 編譯 Linux x86_64 (i5-6100U)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o racing-linux-amd64 .

# 編譯 Linux ARM64 (RK3588)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o racing-linux-arm64 .

# 編譯 Windows (.exe)
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o racing.exe .
```
