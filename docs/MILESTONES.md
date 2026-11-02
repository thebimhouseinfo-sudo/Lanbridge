# Milestones — LAN Test Bridge v1.0

Status: ACTIVE (rev 3)

Chi tiết kỹ thuật từng Task: `docs/TECHNICAL_PROPOSAL.md`.

## Milestone 1: Nền tảng — kết nối, ghép nối, UI tối thiểu
**GOALs:** GOAL-1, GOAL-2, GOAL-10 (phần khung)

**Deliverables:**
- Go module, Makefile build trên Mac cho cả 2 nền tảng (Windows: CGO tắt, 1 file exe).
- mDNS discovery + nhập IP thủ công.
- Ghép nối mã 6 số bằng PAKE chuẩn (CPace) + xác nhận khóa 2 chiều → pin fingerprint → mTLS; Unpair.
- Spike tìm máy trên 2 máy thật trước; `.app` macOS khai báo quyền Mạng cục bộ.
- Thông điệp `bye` khi Mac Thoát (dùng để hủy lệnh từ M3).
- Heartbeat, tự kết nối lại, Thoát dừng hẳn process.
- Khung UI: icon tray/menu bar + trang HTML localhost, hiện IP:port của host.

**Verification (2 máy thật, chạy từ `.app` và `bridge.exe`):**
- Tìm thấy nhau, ghép nối đúng mã; mã sai bị từ chối; sai 3 lần → mã hủy.
- Tắt/mở lại cả 2 → tự kết nối; mất Wi-Fi rồi có lại → tự nối lại.
- Thoát → process không còn.

---

## Milestone 2: Quyền, Read, Sync, giao diện agent
**GOALs:** GOAL-3, GOAL-4, GOAL-5, GOAL-12

**Deliverables:**
- Yêu cầu quyền → Accept/Reject trên Windows; sửa/thu hồi; Sync chỉ vào repo hoặc thư mục liên quan đã khai báo (`related_dirs`); Execute/MCP theo project.
- Một hàm kiểm tra đường dẫn dùng chung (chặn `..`, symlink/junction ra ngoài).
- Read: list, metadata, đọc theo đoạn, đọc N dòng cuối.
- Sync nhiều file/lần ≤ 5 MB, ghi tạm → rename, trả SHA-256.
- `bridge mcp`: MCP server stdio cho Mac agent với các tool trên; đo hỗ trợ `list_changed` và thời gian chờ tool của Claude Code.

**Verification:**
- Agent xin quyền → Accept → đọc log thành công.
- Đọc ngoài phạm vi, `..\`, junction ra ngoài → từ chối. Sync ngoài project → từ chối.
- > 5 MB → từ chối, không file nào bị ghi. Thu hồi → lần gọi sau bị từ chối.

---

## Milestone 3: Lệnh build & MCP dự án
**GOALs:** GOAL-6, GOAL-7

**Deliverables:**
- 8 lệnh cố định ánh xạ theo từng project trong `config.json`, chạy từ repo root.
- Job Object dọn process con, không bật cửa sổ console, không treo vì tiến trình phụ .NET; timeout; lệnh dài lấy kết quả tiếp bằng `command_result`.
- Chuyển tiếp tools của MCP server dự án (transport thật của CAI, kiểm tra trước khi code); tự mở lại phiên khi server khởi động lại.

**Verification:**
- `build` của CAI chạy thật, trả exit code + output, không bật cửa sổ console; ID lạ → từ chối.
- Lệnh dài lấy tiếp bằng `command_result`, kể cả sau khi Wi-Fi rớt rồi nối lại.
- Thoát Windows hoặc Thoát Mac giữa lúc build → không còn process build trên Windows.
- Gọi 1 tool thật của CAI MCP từ Claude Code trên Mac; tắt/mở lại CAI MCP server → gọi lại được không cần khởi động lại Bridge.

---

## Milestone 4: Start/Stop, Chat & hoàn thiện UI
**GOALs:** GOAL-8, GOAL-9, GOAL-10

**Deliverables:**
- Start/Stop app được phép (WM_CLOSE, không force-kill, không gắn vào Job Object).
- Chat 2 chiều trên trang UI.
- UI đầy đủ cho host và client theo CRITERIA §11.

**Verification:**
- App được phép Start/Stop OK; app không được phép → từ chối; Thoát Bridge không tắt AutoCAD.
- Chat gửi/nhận 2 chiều. Mọi thao tác §11 làm được trên UI.

---

## Milestone 5: Cài đặt & nghiệm thu
**GOALs:** GOAL-11

**Deliverables:**
- `install.sh` (Mac) và `install.ps1` (Windows, không admin, không cài thêm gì; cảnh báo nếu Wi-Fi đang là Public).
- Nghiệm thu 12 mục §11 trên 2 máy thật với repo CAI.

**Verification:**
- Cài → icon → mở app → pair → đọc log → sync → build → gọi MCP → start/stop → chat → Thoát.
- Idle mỗi bên < 30 MB RAM, CPU ~0%.
- 12 mục §11 PASS, ghi báo cáo vào `docs/reports/`.
