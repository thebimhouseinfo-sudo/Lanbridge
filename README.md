# LAN Test Bridge

Bridge kết nối máy Mac Apple Silicon với máy Windows 10/11 trên cùng mạng Wi-Fi để thử dự án: trao đổi chat, cấp quyền đọc/sync, chạy build script hoặc Shell Command theo Windows Grant và chuyển tiếp MCP của bất kỳ dự án Windows nào. Hai file config dự án được lưu độc lập trên từng máy; kiểm thử trên hai máy thật vẫn là bước nghiệm thu cuối.

## Cài bản thử qua shell

Hai lệnh dưới đây cài cùng bản thử **test-de9c4ddb6754** đã qua CI, kiểm tra SHA-256 trước khi giải nén và giữ cấu hình hiện có. Cần Internet. Thoát LAN Bridge và phiên MCP trên cả hai máy trước khi cài. Nghiệm thu máy thật vẫn chờ thực hiện.

**Windows 10/11 x64:** Mở PowerShell và chạy một lệnh:

```powershell
& { $ErrorActionPreference = 'Stop'; $d = Join-Path $env:TEMP ([guid]::NewGuid().ToString()); New-Item -ItemType Directory -Path $d | Out-Null; Invoke-WebRequest -UseBasicParsing 'https://github.com/thebimhouseinfo-sudo/Lanbridge/releases/download/test-de9c4ddb6754/testbridge-windows-amd64.zip' -OutFile "$d\package.zip"; if ((Get-FileHash "$d\package.zip" -Algorithm SHA256).Hash -ne '2817bb355c013e5d4bf3826b2f5aa9f3bba511a850e1226f828f0bf4199306d3') { throw 'Checksum không khớp' }; Expand-Archive "$d\package.zip" -DestinationPath $d; powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$d\install.ps1"; if ($LASTEXITCODE -ne 0) { throw 'Cài đặt thất bại' } }
```

Installer chép chương trình vào `%LOCALAPPDATA%\LanBridge`, tạo shortcut và tạo `%APPDATA%\LanBridge\config.json` nếu chưa có. Chạy shortcut, tìm icon Bridge trong khay hệ thống, chọn **Mở giao diện**. Khi Windows Firewall hỏi, cho phép trên mạng Private tin cậy. Cài lại giữ file config hiện có.

**Mac Apple Silicon:** Mở Terminal và chạy một lệnh:

```sh
d="$(mktemp -d)" && curl -fL 'https://github.com/thebimhouseinfo-sudo/Lanbridge/releases/download/test-de9c4ddb6754/testbridge-macos-arm64.zip' -o "$d/package.zip" && [ "$(shasum -a 256 "$d/package.zip" | cut -d ' ' -f 1)" = "a4e7421c97653a074aa00f9f30f1e8777c29a50edae443d7f519716fb44f6483" ] && ditto -x -k "$d/package.zip" "$d" && sh "$d/mac/install.sh"
```

Installer chép app vào `~/Applications/LAN Bridge.app`, chép lệnh `bridge` vào `~/.local/bin` và mở app. Chọn **Mở giao diện** từ icon trên thanh menu; cho phép Local Network nếu macOS hỏi. Nếu macOS chặn app chưa được ký, làm theo thông báo của hệ điều hành trước khi tiếp tục.

README ghim cùng một bản trên hai máy; bản phát hành mới hơn chỉ được dùng khi cập nhật các lệnh này. Workflow chỉ xuất bản package sau khi CI của `main` PASS. Mỗi release có tag theo SHA của source và file `SHA256SUMS.txt` để xác minh gói. Xem [GitHub Releases](https://github.com/thebimhouseinfo-sudo/Lanbridge/releases) và [GitHub Actions](https://github.com/thebimhouseinfo-sudo/Lanbridge/actions). **Thoát cả hai Bridge và phiên MCP trước khi cài lại**; installer giữ App Data/config cũ. Không dùng release cũ `test-2026-11-03`.

Giao diện ưu tiên mở cửa sổ Edge hoặc Chrome cỡ ban đầu khoảng 500 × 900; có thể thay đổi kích thước. Khi không tìm thấy hoặc không khởi chạy được, Bridge thử trình duyệt mặc định. Đóng cửa sổ chỉ đóng giao diện: chọn **Thoát** trong Bridge để kết thúc phiên.

## Kết nối và cấu hình agent (v2)

1. Mở Bridge trên **cả hai máy** cùng Wi-Fi. Windows tạo mã ghép nối trong Settings; Mac tìm host hoặc nhập IP:port rồi pairing. Cổng mặc định là `54823`.
2. **Windows — Setup / Permission Config:** trong Settings nhập Project ID (ví dụ `my-app`) và executable MCP nếu có; tạo file Permission Config lần đầu. Grant/Edit/Revoke folder và program ngay trên Windows UI, không phải chỉnh `config.json` thủ công. Windows có thể cấp nhiều, ít hoặc khác quyền Mac yêu cầu. **Sau khi kết nối, Windows user phải tự Open config đã lưu.**
3. **Mac — Blacklist Config:** trong Settings dùng **First-time Setup** với **cùng Project ID** để tạo file blacklist rỗng trong Mac App Data. Sau đó Mac user hoặc agent có thể Open file tồn tại; agent `config_load` chỉ đọc, không tạo mới hoặc chỉnh file. Mac UI có **Lock/Unlock thủ công**; agent chỉ sửa blacklist theo yêu cầu khi user đã Unlock. Danh sách mặc định rỗng, không chặn tool nào.
4. **Chỉ khi hai config thực sự load thành công và Project ID khớp** mới có `PROJECT_READY`. File blacklist **thiếu hoặc chưa load** sẽ chặn Read/Sync/Exec/App/MCP, dù Windows đã Grant. Hai file config **không sync hoặc copy qua LAN**. Mỗi lần mở Bridge, Windows user Open lại config và Mac/agent Open lại blacklist.
5. Mac Settings chọn **Codex** hoặc **Claude Code** để đăng ký MCP `lanbridge` ở user scope. Chỉ một provider hoạt động. Xem [hướng dẫn Codex](docs/CODEX_SETUP.md). Khi Windows Grant Exec, agent có thể chạy named build command hoặc Shell argv theo nhu cầu; Sync tối đa 5 MB/request tới thư mục Windows đã Grant, không đòi thuộc `projects`/`related_dirs`. Với target `root` hoặc thư mục chạy mặc định, Bridge dùng folder duy nhất có quyền tương ứng; nếu có nhiều folder, hãy chỉ định đường dẫn cụ thể.

**Quan trọng khi cài lại:** Installer giữ dữ liệu Bridge hiện có trong App Data. Legacy `config.json` vẫn có thể tồn tại vì tương thích, nhưng project workflow v2 cần **Windows Permission Config** và **Mac Command Blacklist Config** riêng. Không dùng CAI/RAI hoặc AutoCAD OPEN làm blacklist mặc định.

Không chia sẻ pairing code hoặc URL giao diện có token. Chỉ sử dụng trên LAN tin cậy; các giới hạn blacklist là chính sách workflow theo yêu cầu user, không phải sandbox chống mọi lệnh Shell.

## Danh sách thử trên hai máy

- Cài và mở cả hai app, ghép nối, đóng/mở lại để thử tự kết nối và fallback IP thủ công.
- Gửi text và script nhiều dòng hai chiều; dùng Copy và dán để so sánh nguyên văn. Đóng cửa sổ rồi mở lại từ tray; dùng **Thoát** để kết thúc Bridge.
- Với một thư mục/repo thử, kiểm tra yêu cầu quyền, Accept/Reject, sửa/thu hồi, đọc log, sync file nhỏ và từ chối khi vượt giới hạn 5 MB.
- Cấu hình lệnh và MCP thực của project, kiểm tra build/test, output và chuyển tiếp tool; kiểm tra Start/Stop ứng dụng chỉ khi đã cấp quyền.
- Thử Codex/Claude registration, agent gửi chat, mất/kết nối lại, thoát khi có tác vụ và mức CPU/RAM nhàn rỗi. Ghi rõ PASS/FAIL và log; không coi CI thay cho kiểm thử hai máy.

## Build từ source

Trên Mac có Go 1.22 và công cụ build native của Apple: `make dist-mac` tạo gói Mac; `make dist-windows` tạo gói Windows. Chạy `go test ./...` và `go vet ./...` để kiểm tra source. GitHub Actions cũng chạy test và build trên Windows và macOS ARM64. Thư viện mạng hiện tại chưa liên kết được với Go 1.23 trên macOS; dùng Go 1.22 cho bản thử này.
