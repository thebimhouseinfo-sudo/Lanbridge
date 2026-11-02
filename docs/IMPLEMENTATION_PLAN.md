# Implementation Plan — LAN Test Bridge v1.0

Status: ACTIVE

## Technical direction

**Stack:** Go — một binary, 3 vai: `bridge host` (Windows), `bridge client` (macOS),
`bridge mcp` (macOS, MCP server stdio cho Mac agent). Build toàn bộ trên Mac;
Windows chỉ nhận `bridge.exe` (CGO tắt), không cài runtime nào khác.

**Protocol:** JSON messages over mTLS TCP, pin fingerprint sau ghép nối.

**Discovery:** mDNS/DNS-SD + nhập IP thủ công.

**Pairing:** Mã 6 số trên Windows, nhập trên Mac, xác thực bằng PAKE chuẩn (CPace, chép nguyên gói có license) + xác nhận khóa 2 chiều → lưu fingerprint.

**UI:** Icon tray/menu bar + trang HTML nhúng mở bằng trình duyệt (chỉ localhost). Không framework UI.
**UI interaction:** Trang chính là chat panel; kết nối, pairing, yêu cầu/quyền và thông báo nằm trong Settings. Client hiển thị thao tác gần nhất/đang chạy mà `bridge mcp` thực sự gửi qua Bridge; lệnh hiển thị `<project> · Chạy lệnh` (hoặc `Chạy lệnh` nếu thiếu project), tuyệt đối không hiện command arguments, output hoặc nội dung file. Không tuyên bố biết task title nội bộ của agent. Nếu agent không thể chạy lệnh Windows hoặc cần người dùng thao tác, MCP `chat_send` gửi lời giải thích và script nhiều dòng vào chat Bridge chung; người dùng Windows thấy rõ tin từ `Agent (Mac)` và có thể copy để tự chạy. Bridge không chạy script từ chat.

**Nhiều repo:** mỗi project (CAI, sau này RAI…) là một mục trong `config.json` trên Windows.

**Persistence:** `config.json` (người dùng soạn) + `state.json` (app ghi). Không database.

**Nguyên tắc:** Tránh over-engineering. Gate đơn giản cho phép/không cho phép.
Không thêm cơ chế bảo mật thừa. Chi tiết: `docs/TECHNICAL_PROPOSAL.md`.

---

## GOALMAP

### GOAL-1: Project scaffold và build system
> CRITERIA: §2, §3, §13

Go module, cấu trúc thư mục, cross-compile script.
Observable: `go build` tạo binary chạy được trên cả 2 platform.

### GOAL-2: Transport — TLS connection, discovery, pairing
> CRITERIA: §4

- Server LAN listener mặc định chạy cổng lạ ít trùng (mặc định `54823`, tránh xa 3000), configurable qua `--port`/config.
- mDNS discovery (broadcast port đang listen) + manual `IP:port` fallback.
- Pairing flow: short code → key exchange → identity persistence.
- TLS encrypted connection, auto-reconnect, graceful disconnect.

Observable: 2 instance LAN tìm nhau, pair trên cổng chỉ định (mặc định 54823), giao tiếp mã hóa, reconnect.

### GOAL-3: Folder permissions
> CRITERIA: §5

- Client request quyền trên đường dẫn + capability.
- Host Accept/Reject, edit/revoke.
- Lưu config file.
- Gate đơn giản: check path nằm trong thư mục được duyệt → cho phép/từ chối.

Observable: request/accept/reject/revoke hoạt động.

### GOAL-4: Read
> CRITERIA: §6

List, metadata, read file, read ranges. Check permission trước mỗi op.
Observable: đọc file từ thư mục duyệt OK; ngoài scope → denied.

### GOAL-5: Sync (Mac → Windows)
> CRITERIA: §6

Upload file nhỏ vào thư mục dự án. Check permission + size ≤ 5 MB.
Ghi temp → rename. Observable: sync chính xác; > 5 MB → rejected.

### GOAL-6: Fixed dev commands
> CRITERIA: §7

Registry command ID → script thật. Chạy từ repo root, capture output.
Observable: `build` chạy thật trả output; arbitrary shell → rejected.

### GOAL-7: MCP forwarding
> CRITERIA: §8

Chuyển tiếp tools của MCP server dự án qua transport thật của CAI (kiểm tra trước M3); tự mở lại phiên khi server khởi động lại.
Observable: tool thật invoke từ Mac.

### GOAL-8: Software Start/Stop
> CRITERIA: §9

Start/Stop approved executables.
Observable: approved app start/stop OK; unapproved → rejected.

### GOAL-9: Chat
> CRITERIA: §10

Bidirectional text, script snippets and pasted logs qua TLS connection; Windows user có thể copy text/script agent gửi từ chat và tự quyết định chạy ngoài Bridge.
Observable: gửi/nhận text 2 chiều, giữ nguyên nội dung nhiều dòng và Copy hoạt động; Bridge không auto-execute chat text.

### GOAL-10: UI
> CRITERIA: §11

Host (Windows): tray + trang UI; client (macOS): menu bar + trang UI. Cả hai mở vào chat panel hiển thị chat hai chiều giữa người dùng và tin nhắn agent gửi qua Bridge; phần kết nối, pairing, thông báo/quyền và cấu hình khác nằm trong Settings. Client có dòng hoạt động gọn từ thao tác Bridge quan sát được qua MCP; không đọc hay dựng task title tổng quát của agent.
Khung UI làm ở Milestone 1, hoàn thiện ở Milestone 4.
Observable: chat là trang chính; cấu hình kết nối và thông báo được tìm thấy trong Settings; người dùng thấy tin `chat_send` trên cả hai UI; thao tác Bridge đang chạy/gần nhất hiện đúng tên gọn, chỉ từ MCP call mà Bridge nhận được.

### GOAL-11: Install scripts
> CRITERIA: §3

`install.sh` + `install.ps1` (không admin, không cài runtime). Desktop launcher.
Observable: installer → icon → click mở app.

### GOAL-12: Giao diện cho Mac agent
> CRITERIA: §5, §6, §7, §8, §9, §10

`bridge mcp` — MCP server stdio mà Mac agent đăng ký một lần; cung cấp tool xin quyền,
read, sync, chạy lệnh, Start/Stop, và tools chuyển tiếp từ MCP server của dự án. Dùng chat Bridge hai chiều đã có để gửi text/script cho user; đây là wiring của chat hiện hữu, không phải một loại chat mới.
Observable: Claude Code trên Mac gọi được các tool này qua Bridge.
