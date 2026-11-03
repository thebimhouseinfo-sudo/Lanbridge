# LAN Test Bridge

Bridge kết nối máy Mac Apple Silicon với máy Windows 10/11 trên cùng mạng Wi-Fi để thử dự án: trao đổi chat, cấp quyền đọc/sync, chạy các lệnh build đã cấu hình và chuyển tiếp MCP của dự án. Đây là bản thử cá nhân; kiểm thử trên hai máy thật và tích hợp CAI vẫn đang chờ thực hiện.

## Cài bản thử qua shell

Bản `test-2026-11-03` là bản thử, chưa được nghiệm thu trên hai máy thật. Mỗi lệnh tải gói từ GitHub vào thư mục tạm riêng, giải nén rồi chạy installer. Cần có kết nối Internet khi cài.

**Windows 10/11 x64:** Mở PowerShell và chạy một lệnh:

```powershell
& { $ErrorActionPreference = 'Stop'; $d = Join-Path $env:TEMP ([guid]::NewGuid().ToString()); New-Item -ItemType Directory -Path $d | Out-Null; Invoke-WebRequest 'https://github.com/thebimhouseinfo-sudo/testbridge/releases/download/test-2026-11-03/testbridge-windows-amd64.zip' -OutFile "$d\package.zip"; Expand-Archive "$d\package.zip" -DestinationPath $d; & "$d\install.ps1" }
```

Installer chép chương trình vào `%LOCALAPPDATA%\LanBridge`, tạo shortcut và tạo `%APPDATA%\LanBridge\config.json` nếu chưa có. Chạy shortcut, tìm icon Bridge trong khay hệ thống, chọn **Mở giao diện**. Khi Windows Firewall hỏi, cho phép trên mạng Private tin cậy. Cài lại giữ file config hiện có.

**Mac Apple Silicon:** Mở Terminal và chạy một lệnh:

```sh
d="$(mktemp -d)" && curl -fL 'https://github.com/thebimhouseinfo-sudo/testbridge/releases/download/test-2026-11-03/testbridge-macos-arm64.zip' -o "$d/package.zip" && ditto -x -k "$d/package.zip" "$d" && sh "$d/mac/install.sh"
```

Installer chép app vào `~/Applications/LAN Bridge.app`, chép lệnh `bridge` vào `~/.local/bin` và mở app. Chọn **Mở giao diện** từ icon trên thanh menu; cho phép Local Network nếu macOS hỏi. Nếu macOS chặn app chưa được ký, làm theo thông báo của hệ điều hành trước khi tiếp tục.

Gói và kết quả build có thể xem tại [GitHub Releases](https://github.com/thebimhouseinfo-sudo/testbridge/releases/tag/test-2026-11-03) và [GitHub Actions](https://github.com/thebimhouseinfo-sudo/testbridge/actions).

Giao diện ưu tiên mở cửa sổ Edge hoặc Chrome cỡ ban đầu khoảng 500 × 900; có thể thay đổi kích thước. Khi không tìm thấy hoặc không khởi chạy được, Bridge thử trình duyệt mặc định. Đóng cửa sổ chỉ đóng giao diện: chọn **Thoát** trong Bridge để kết thúc phiên.

## Kết nối và cấu hình agent

1. Mở Bridge trên cả hai máy cùng Wi-Fi. Trên Windows vào **Settings** tạo mã ghép nối; trên Mac tìm host hoặc nhập IP:port hiển thị trên Windows, rồi nhập mã. Nếu discovery không tìm thấy, dùng địa chỉ thủ công; cổng mặc định là `54823`.
2. Trong Settings trên Mac chọn **Codex** hoặc **Claude Code** để Bridge đăng ký MCP `lanbridge` ở user scope. Chỉ một provider hoạt động; đóng phiên MCP đang dùng trước khi chuyển. Xem [hướng dẫn Codex và đăng ký thủ công](docs/CODEX_SETUP.md).
3. Trên Windows, chỉnh `%APPDATA%\LanBridge\config.json` để khai báo project, các lệnh cố định, MCP và app cần thử; dùng nút mở cấu hình/tải lại trong Settings. Cấu hình mới mặc định chưa có project hay app. Mẫu trường và ràng buộc có trong [Technical Proposal](docs/TECHNICAL_PROPOSAL.md).
4. Khi agent yêu cầu quyền, xem và Accept/Reject trên Windows. Sync chỉ dành cho project hoặc thư mục liên quan đã cấu hình. Không chia sẻ mã ghép nối hay URL giao diện có token.

## Danh sách thử trên hai máy

- Cài và mở cả hai app, ghép nối, đóng/mở lại để thử tự kết nối và fallback IP thủ công.
- Gửi text và script nhiều dòng hai chiều; dùng Copy và dán để so sánh nguyên văn. Đóng cửa sổ rồi mở lại từ tray; dùng **Thoát** để kết thúc Bridge.
- Với một thư mục/repo thử, kiểm tra yêu cầu quyền, Accept/Reject, sửa/thu hồi, đọc log, sync file nhỏ và từ chối khi vượt giới hạn 5 MB.
- Cấu hình lệnh và MCP thực của project, kiểm tra build/test, output và chuyển tiếp tool; kiểm tra Start/Stop ứng dụng chỉ khi đã cấp quyền.
- Thử Codex/Claude registration, agent gửi chat, mất/kết nối lại, thoát khi có tác vụ và mức CPU/RAM nhàn rỗi. Ghi rõ PASS/FAIL và log; không coi CI thay cho kiểm thử hai máy.

## Build từ source

Trên Mac có Go 1.22 và công cụ build native của Apple: `make dist-mac` tạo gói Mac; `make dist-windows` tạo gói Windows. Chạy `go test ./...` và `go vet ./...` để kiểm tra source. GitHub Actions cũng chạy test và build trên Windows và macOS ARM64. Thư viện mạng hiện tại chưa liên kết được với Go 1.23 trên macOS; dùng Go 1.22 cho bản thử này.
