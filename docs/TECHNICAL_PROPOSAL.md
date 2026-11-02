# Technical Proposal — LAN Test Bridge v1.0 (rev 6)

Status: FINAL rev 7 — Critic CR PASS. Rev 3 receipt covers only rev 3; rev 6 was previously reviewed.

## Overview

Một binary Go (`bridge`) chạy 3 vai:

| Vai | Máy | Ai dùng | Mô tả |
|---|---|---|---|
| `bridge host` | Windows | Windows user | Giữ kết nối, gate quyền, chạy lệnh/MCP/app. Icon tray + trang UI. |
| `bridge client` | macOS | Mac user | Giữ kết nối tới host, pairing, chat. Icon menu bar + trang UI. |
| `bridge mcp` | macOS | Mac agent (Claude Code…) | MCP server stdio nhỏ do agent tự khởi chạy; chuyển yêu cầu sang `bridge client` đang chạy. |

Giao tiếp Mac ↔ Windows: JSON messages trên TLS TCP (mTLS, pin fingerprint), Wi-Fi LAN.

Nguyên tắc: Windows chỉ cần **một file `bridge.exe`** — không cài Go, Node, Python,
WebView, .NET hay dịch vụ nền. Mọi build làm trên Mac. Một Bridge phục vụ nhiều
repo (CAI trước, RAI và repo Windows khác sau) chỉ bằng thêm một mục config.

### Phụ thuộc ngoài stdlib
Danh sách này là nguồn duy nhất; `PROJECT_PROFILE.md` trỏ về đây.
- `fyne.io/systray` — icon tray. Windows: pure Go, build `CGO_ENABLED=0`; macOS: cần CGO, build native trên Mac.
- `github.com/grandcat/zeroconf` — mDNS, pure Go. Ít được bảo trì (commit cuối 2023) nhưng chỉ là
  tiện ích tìm máy; luôn có đường dự phòng nhập IP. Không thêm thư viện mDNS khác.
- CPace (PAKE cho mã ghép nối) — **chép** gói `rsc.io/cmd/mote/internal/cpace` (bản draft-21, X25519,
  có xác nhận khóa Tag/Verify) vào `internal/third_party/cpace/` kèm file LICENSE (BSD), ghi rõ nguồn và commit.
  Không tự viết crypto.
- `golang.org/x/sys/windows` — Job Object, `CREATE_NO_WINDOW`, `WM_CLOSE`.
- Icon: bộ icon mở **Lucide** (giấy phép ISC), kiểu flat nét đơn. Chỉ dùng lúc phát triển:
  `tools/genicons` (Go, **`go.mod` riêng**, dùng `github.com/srwiley/oksvg` + `rasterx`) chuyển SVG thành PNG/ICO một lần,
  file kết quả commit vào `internal/ui/icons/` kèm LICENSE của Lucide. Binary chạy không mang thêm thư viện nào.
- Phụ thuộc gián tiếp của các gói trên (ví dụ `miekg/dns` của zeroconf). Gói cpace chép vào chỉ dùng stdlib (`crypto/ecdh` cho X25519).

Không dùng framework UI: giao diện là một trang HTML nhúng trong binary, mở bằng
trình duyệt mặc định tại `127.0.0.1:<cổng ngẫu nhiên>/?t=<token>`; chỉ lắng nghe localhost.

### Config và trạng thái (Windows host)
Thư mục `%APPDATA%\LanBridge\`:
- `config.json` — **người dùng soạn**, app chỉ đọc (UI có nút "Mở file cấu hình" và "Tải lại"):
  ```json
  {
    "port": 54823,
    "max_sync_mb": 5,
    "projects": [
      {
        "name": "CAI",
        "root": "D:\\Repos\\CAI",
        "related_dirs": {
          "addin": "C:\\Users\\<you>\\AppData\\Roaming\\Autodesk\\ApplicationPlugins\\CAI.bundle"
        },
        "commands": {
          "build":   ["powershell", "-NoProfile", "-File", "scripts\\build.ps1"],
          "test":    ["powershell", "-NoProfile", "-File", "scripts\\test.ps1"],
          "install": ["powershell", "-NoProfile", "-File", "scripts\\install-addin.ps1"]
        },
        "command_timeout_min": 30,
        "mcp": {"transport": "stdio", "command": ["D:\\Repos\\CAI\\mcp\\server.exe"]}
      }
    ],
    "apps": [
      {"name": "AutoCAD 2025", "path": "C:\\Program Files\\Autodesk\\AutoCAD 2025\\acad.exe"}
    ]
  }
  ```
  - Thêm RAI = thêm một mục vào `projects`.
  - `name` chỉ gồm chữ, số, `_`, `-`, tối đa 16 ký tự (để tên tool chuyển tiếp không vượt 64 ký tự).
  - `related_dirs`: thư mục liên quan trực tiếp tới dự án (CRITERIA §5), ví dụ thư mục nạp add-in.
  - Key trong `commands` chỉ chấp nhận 8 ID của CRITERIA §7 (`build`, `clean-build`, `install`,
    `restore`, `doctor`, `test`, `status`, `version`).
  - Config sai → UI hiện lỗi cụ thể; project lỗi bị bỏ qua, phần còn lại vẫn chạy.
- `state.json` — **app tự ghi**: identity, thiết bị đã pair, quyền đã cấp. Ghi temp → rename.

Mac client: `~/Library/Application Support/LanBridge/state.json` (identity, host đã pair, địa chỉ cuối).

---

## Milestone 1: Nền tảng — scaffold, kết nối, ghép nối, UI tối thiểu

### Task 1.1: Scaffold & build
- `go mod init github.com/thebimhouseinfo-sudo/testbridge`; `cmd/bridge/main.go`, `internal/…`.
- `Makefile` (chạy trên Mac):
  - `dist-windows`: `GOOS=windows GOARCH=amd64 CGO_ENABLED=0`, `-H windowsgui` → `dist/windows/bridge.exe` + `install.ps1`.
  - `dist-mac`: build native arm64 (CGO) và đóng gói `dist/mac/LAN Bridge.app`. `Info.plist` gồm
    `LSUIElement` (chỉ icon menu bar), `NSLocalNetworkUsageDescription` và
    `NSBonjourServices = [_lanbridge._tcp]` để macOS 15+ cho phép tìm và kết nối máy trong LAN.
  - `test`.

### Task 1.2: Spike tìm máy (làm đầu tiên)
- Chạy zeroconf trên Windows thật (cùng lúc với dịch vụ mDNS sẵn có của Windows trên UDP 5353)
  và từ `.app` trên Mac. Không được → giữ nhập IP thủ công làm đường chính, ghi lại kết quả.

### Task 1.3: Transport & discovery
- Host listen TLS trên `port` (mặc định 54823, `--port` ghi đè); quảng bá `_lanbridge._tcp`.
- Client tìm qua mDNS; không thấy → địa chỉ lần trước → người dùng nhập `IP[:port]`.
- Mỗi bên tạo một lần cặp khóa ECDSA P-256 + cert self-signed (stdlib), lưu trong `state.json`.
- Sau khi pair: mTLS, mỗi bên kiểm tra fingerprint SHA-256 cert đối phương trùng giá trị đã lưu. Không CA.
- Khung message: 1 dòng JSON / message, có `id` để ghép request–response.
- Heartbeat 10 s; mất 3 nhịp → coi như ngắt. Client tự kết nối lại (3 s, tăng dần tới 30 s).
- Chỉ **một** client được kết nối tại một thời điểm.
- **Ngắt kết nối:**
  - Mac bấm Thoát → gửi `bye` → host hủy các lệnh đang chạy và yêu cầu đang chờ của client đó (CRITERIA §11.3).
  - Mất heartbeat (Wi-Fi chập chờn) → host **giữ** các lệnh đang chạy; sau khi nối lại, `job_id` vẫn dùng được.

### Task 1.4: Ghép nối
- Windows UI bấm "Ghép nối thiết bị mới" → hiện mã 6 số, hiệu lực 2 phút.
- Mac UI nhập mã. Hai bên mở TLS **hai chiều** (cả hai trình cert, chưa pin).
- Mỗi bên lấy fingerprint của cả hai cert **từ trạng thái kết nối TLS**, không bao giờ từ nội dung message.
- Chạy CPace với mã làm mật khẩu (vai cố định: Mac = Initiator, Windows = Responder), ràng buộc hai fingerprint qua `ChannelID` = `host_fp‖client_fp` (thứ tự cố định, không qua `AD`); sau đó **xác nhận khóa cả hai chiều**
  (Tag/Verify). Chỉ khi cả hai chiều đúng mới lưu fingerprint đối phương.
- Xác nhận sai tính là một lần nhập sai; sai 3 lần → mã hủy, phải tạo mã mới.
- Lần sau tự kết nối, không cần mã.
- Unpair: ở bất kỳ bên nào → xóa fingerprint đối phương (host xóa luôn mọi quyền đã cấp).

### Task 1.5: Khung UI (dùng lại cho mọi milestone)
- `internal/ui`: tray/menu bar với "Mở giao diện" và "Thoát"; HTTP server localhost
  phục vụ 1 trang HTML/JS thuần (embed), cập nhật qua SSE.
- **Icon tray/menu bar báo trạng thái bằng màu** (cùng một hình, đổi màu; mất kết nối đổi sang hình "rút phích"
  để người không phân biệt màu vẫn nhận ra):

  | Trạng thái | Màu | Hình Lucide |
  |---|---|---|
  | Đã kết nối, sẵn sàng | Xanh lá `#34C759` | `cable` |
  | Đang chạy lệnh build/test | Xanh dương `#0A84FF` | `cable` |
  | Đang kết nối / chờ ghép nối / có yêu cầu quyền chờ duyệt | Vàng cam `#FF9F0A` | `cable` |
  | Mất kết nối / chưa ghép nối | Xám `#8E8E93` | `unplug` |

  - Kích thước: macOS một PNG 32×32 truyền vào `SetIcon` (không dùng `SetTemplateIcon`, để giữ màu), hiển thị 16 pt, nét trên Retina;
    Windows một ICO chứa 16/20/24/32/40/48 px. Màu xám đủ tương phản trên thanh menu/taskbar sáng và tối.
  - Rê chuột lên icon hiện dòng chữ trạng thái, ví dụ "Đã kết nối tới DESKTOP-ABC" hoặc "Đang build CAI…".
  - Icon cập nhật ngay khi trạng thái đổi; không nhấp nháy, không thông báo bật lên.
- M1 hiển thị: trạng thái kết nối, **IP:port của host** (để nhập tay khi cần), ghép nối, Unpair, Thoát.
- **Thoát** (từ tray hoặc nút trên trang) = dừng hẳn process: hủy lệnh đang chạy, đóng kết nối.
  Đóng tab trình duyệt không thoát app (icon tray vẫn hiện nên không phải chạy ngầm).

**Kiểm chứng M1 (chạy từ `.app` và `bridge.exe` thật, 2 máy):** tự tìm thấy nhau (hoặc nhập IP),
ghép nối bằng mã, mã sai bị từ chối, sai 3 lần → mã hủy; tắt/mở lại cả 2 → tự kết nối;
mất Wi-Fi rồi có lại → tự nối lại; Thoát → process biến mất.

---

## Milestone 2: Quyền, Read, Sync và giao diện cho agent

### Task 2.1: Quyền thư mục
- `internal/permissions`: `Grant{Path, Caps []string}`, `Caps ⊂ {read, sync, execute, mcp}`, lưu `state.json`.
- Agent gửi yêu cầu → Windows UI hiện thẻ Accept / Reject (có thể bỏ tick từng quyền trước khi Accept).
  Tool của agent chờ tối đa 5 phút cho câu trả lời.
- Ràng buộc theo CRITERIA §5:
  - `sync` chỉ cấp cho `root` hoặc một `related_dirs` của project (hoặc thư mục con của chúng).
  - `execute` và `mcp` cấp theo project (đường dẫn = `root`).
  - `read` cấp cho bất kỳ thư mục nào user duyệt.
- UI liệt kê quyền đã cấp, cho sửa / thu hồi.
- **Kiểm tra đường dẫn** (một hàm dùng chung `permissions.Resolve`): ghép đường dẫn,
  làm sạch, giải symlink/junction (`filepath.EvalSymlinks`), so sánh không phân biệt hoa
  thường trên Windows; nằm ngoài thư mục được cấp → từ chối. Đây là phần cốt lõi của gate,
  không phải bảo mật thêm.

### Task 2.2: Read
- Ops: `list_dir`, `file_info`, `read_file{path, offset, length}`.
- Mỗi lần đọc tối đa 1 MB; lớn hơn → đọc theo đoạn. Hỗ trợ đọc N dòng cuối (`tail_lines`) cho log.
- Lỗi rõ: không có quyền / không tồn tại / lỗi IO.

### Task 2.3: Sync (Mac → Windows)
- Tool agent: `sync_files{project, target = "root" | "<tên related_dir>", local_root, files: ["src/A.cs", …]}`
  — đường dẫn tương đối; `bridge mcp` tự đọc file từ `local_root` trên Mac (mặc định thư mục làm việc
  của agent) rồi gửi đi; agent không phải nhét nội dung file vào lời gọi.
- Tổng ≤ `max_sync_mb` (5 MB) mỗi lần; vượt → từ chối cả lần, không ghi gì.
- Host: kiểm tra quyền `sync` từng file qua `permissions.Resolve`, tạo thư mục cha nếu thiếu,
  ghi file tạm cạnh đích → rename. Trả SHA-256 từng file để Mac đối chiếu.
- File đích đang bị khóa (DLL đang được AutoCAD nạp) → báo lỗi rõ tên file, không ghi đè nửa chừng.

### Task 2.4: `bridge mcp` — giao diện cho Mac agent
- **Kênh nội bộ Mac:** `bridge client` mở socket `127.0.0.1:<cổng ngẫu nhiên>`, ghi cổng + token
  vào file trong thư mục state (chỉ user hiện tại đọc được). `bridge mcp` đọc file đó để nối vào client.
  Client chưa mở → tool trả lỗi rõ: "LAN Bridge trên Mac chưa mở".
- MCP server stdio tự viết tối giản: `initialize` (khai báo `tools.listChanged`), `tools/list`,
  `tools/call`, `notifications/tools/list_changed`; không cần SDK.
- Tool cố định: `bridge_status`, `request_access`, `list_dir`, `file_info`, `read_file`, `sync_files`
  (M3 thêm `run_command`, `command_result`; M4 thêm `app_start`, `app_stop` và nối việc gửi chat agent vào chat text hai chiều hiện có theo Task 4.2).
- Chat không phải capability MCP/chat mới: Criteria §10 đã duyệt gửi/nhận text, script snippets, pasted logs và Copy. MCP cần một tool `chat_send(text)` để agent dùng đúng chat Bridge hiện có, đặc biệt khi thao tác Windows bị chặn hoặc cần user làm tiếp. Tin nhắn nhiều dòng được giữ nguyên; user Windows tự Copy/chạy, Bridge không thực thi chat text.
- Đăng ký 1 lần trong Claude Code: `claude mcp add lanbridge -- bridge mcp`.
- **Kiểm tra trên Mac thật trong M2:** Claude Code có nhận `list_changed` không (không → hướng dẫn
  người dùng gõ `/mcp` để tải lại) và thời gian chờ tối đa của một lời gọi tool (dùng để chọn mặc định
  `wait_seconds` ở M3).
- Task 4.2 adds `chat_send(text)` to this existing MCP interface as a caller for the existing Bridge chat, not as a separate chat capability. It uses a dedicated token-authenticated localhost endpoint, with a server-selected agent sender kind; it never routes chat text to a project MCP tool or command executor.

**Kiểm chứng M2:** agent xin quyền → Accept → đọc log; đọc ngoài phạm vi, `..\`,
junction trỏ ra ngoài → bị từ chối; sync 3 file vào repo và 1 file vào `related_dirs` đúng hash;
> 5 MB → từ chối và không file nào bị ghi; thu hồi quyền → lần gọi tiếp bị từ chối.

---

## Milestone 3: Lệnh build và chuyển tiếp MCP của dự án

### Task 3.1: Lệnh cố định
- Tool `run_command{project, command, wait_seconds}` (mặc định 600, hạ xuống nếu M2 đo được
  thời gian chờ tool ngắn hơn): cần quyền `execute` trên project và ID có trong `config.json`.
  Không có tham số tự do → không có shell tùy ý.
- Host chạy `argv` với thư mục làm việc = `root`, cờ `CREATE_NO_WINDOW` (không bật cửa sổ console),
  trong Windows **Job Object** (KILL_ON_JOB_CLOSE) để Thoát/timeout/`bye` dọn sạch cả process con.
- **Xong** = process chính thoát. Đọc nốt output tối đa vài giây (`cmd.WaitDelay`), rồi kết thúc Job —
  tránh treo vì tiến trình phụ của .NET (MSBuild node reuse, VBCSCompiler) giữ ống output.
- Mỗi project chạy 1 lệnh một lúc; đang chạy → báo bận.
- Xong trong `wait_seconds` → trả exit code + stdout/stderr. Chưa xong → trả `job_id` và
  phần output đã có; agent gọi `command_result{job_id}` để lấy tiếp.
- **Lưu output:** host giữ output của job trong bộ nhớ, tối đa ~20 MB/job (vượt thì giữ phần cuối, có ghi chú
  bị cắt); giữ job đã xong tới khi có job mới của project đó. Trả cho agent 200 KB cuối mỗi luồng;
  khi job xong, `bridge mcp` ghi toàn bộ output vào file tạm trên Mac và trả đường dẫn.
- Quá `command_timeout_min` → dừng job, báo timeout.

### Task 3.2: Chuyển tiếp MCP của dự án
- Host mở **một phiên MCP** tới server của project khi lần đầu cần, làm `initialize` một lần.
- Phiên lỗi (server/AutoCAD crash hoặc khởi động lại) → bỏ phiên, lần gọi sau tự mở và `initialize` lại,
  và báo `tools/list_changed` cho Mac.
- Chỉ chuyển tiếp **tools** (đúng CRITERIA §8): `tools/list` và `tools/call`, không lọc.
- `bridge mcp` gộp tool của mọi project có quyền `mcp`, đặt tên `<project>__<tool>`
  (ví dụ `CAI__draw_line`; Claude Code hiển thị `mcp__lanbridge__CAI__draw_line`, phải ≤ 64 ký tự —
  tool nào vượt thì báo lỗi rõ trên UI), và báo `tools/list_changed` khi quyền hoặc kết nối đổi.
- **Trước khi code M3:** kiểm tra transport thật của MCP server CAI và chỉ cài transport đó
  (stdio, hoặc Streamable HTTP). Transport kia làm sau khi có repo cần.

**Kiểm chứng M3:** `build` của CAI chạy thật, trả exit code/output, không bật cửa sổ console;
ID lạ → từ chối; lệnh dài > `wait_seconds` lấy tiếp bằng `command_result`, kể cả sau khi Wi-Fi rớt rồi nối lại;
Thoát giữa chừng → không còn process build; gọi được 1 tool thật của CAI MCP từ Claude Code trên Mac;
tắt/mở lại CAI MCP server → gọi tool lại được mà không phải khởi động lại Bridge.

---

## Milestone 4: Start/Stop, Chat, hoàn thiện UI

### Task 4.1: Start/Stop phần mềm
- Chỉ app có trong `config.json` `apps` **và** được Windows user bật "Cho phép Start/Stop" trên UI.
- Start: chạy exe (không thuộc Job Object, để Thoát Bridge không tắt AutoCAD).
- Stop: gửi `WM_CLOSE` tới **cửa sổ chính** (hiển thị, không có cửa sổ cha) của các process có đúng
  đường dẫn exe đó. Không force-kill; app hỏi lưu file thì để người dùng xử lý.
  Trả trạng thái "đã gửi lệnh đóng / vẫn đang chạy".

### Task 4.2: Chat
- Khung chat trên trang UI của cả 2 bên: gửi/nhận text, dán log, nút Copy. Chỉ giữ trong bộ nhớ phiên.
- Nối agent Mac vào cùng chat bằng MCP `chat_send(text)` theo Task 2.4; không thêm history/store/channel riêng. Tin nhắn agent đi qua `TypeChat` hiện có. Callback phía Mac đặt một sender kind cố định (`mac_user` cho UI, `mac_agent` cho MCP), không nhận sender tùy ý trong arguments. Host yêu cầu client connected+paired, chỉ nhận sender kinds hỗ trợ và map sang nhãn UI cố định `Mac` / `Agent (Mac)`.
- Dùng request/confirmation `TypeChat` hiện có: Host append vào session chat rồi trả tin đã gắn nhãn; Mac append bản xác nhận. MCP chỉ báo thành công sau confirmation. Timeout sau lần gửi báo delivery unknown, không tự retry; không tuyên bố exactly-once khi kết nối ngắt sau khi Host append.
- Giữ nguyên văn bản UTF-8 nhiều dòng/special characters. Tái dùng `textContent`, CSS `white-space: pre-wrap` và Copy nguyên `message.text` hiện có; không thêm parser, nút run hay định dạng script riêng. Một giới hạn text là **8 MiB tính theo bytes** ở MCP/local endpoint/Host; local HTTP envelope có overhead cho JSON escaping. Lỗi text rỗng/quá size, chưa kết nối/chưa pair và timeout phải trả thông báo rõ. Giới hạn này không tuyên bố bảo vệ bộ nhớ trước khi protocol frame hiện hữu đọc toàn frame.
- Không route nội dung chat vào `TypeExecStart` hoặc `TypeMCPCall`; Bridge không thực thi text nhận từ chat.

### Task 4.3: Hoàn thiện UI
- Host: trang chính là chat; Settings chứa trạng thái + IP:port, ghép nối, yêu cầu quyền, danh sách quyền (sửa/thu hồi), danh sách project đọc từ config (báo lỗi nếu có), danh sách app (bật/tắt Start/Stop), lệnh đang chạy và Thoát.
- Client: trang chính là chat kèm trạng thái activity Bridge; Settings chứa trạng thái, ghép nối/nhập IP, quyền đang có, hướng dẫn đăng ký `bridge mcp`, cảnh báo và Thoát.
- Cả hai vai giữ trang HTML/CSS/JS nhúng, cùng bộ điều khiển hiện có. Đưa chat thành view mặc định; nhóm toàn bộ cài đặt/kết nối/thông báo trong view Settings. Badge trên Settings chỉ đếm thông báo/yêu cầu đang chờ; chi tiết và thao tác xử lý nằm bên trong Settings. Không tạo hệ thống notification mới.
- `internal/ui/embed/index.html` (MODIFY): thêm điều hướng Chat/Settings, chuyển các khu vực không phải chat vào Settings, giữ nguyên thao tác hiện hữu; chat là nội dung chính khi mở trang. Reuse message text render `textContent`, multiline `pre-wrap`, per-message Copy.
- `internal/ui/web.go` (MODIFY): mở rộng `UIState` với activity MCP hiện tại/gần nhất; thêm API nội bộ `POST /api/mcp/activity` và `POST /api/mcp/chat/send`, dùng token cục bộ hiện có, xác thực method/body và phát thay đổi qua SSE. Activity và chat chỉ tồn tại trong bộ nhớ phiên; route chat đặt sender kind `mac_agent` ở phía server, không tin sender do JSON body cung cấp.
- `internal/agentmcp/server.go` (MODIFY): thêm `chat_send(text)` vào danh sách MCP tools; callback dùng endpoint localhost riêng được xác thực, không route sang project MCP. Activity gửi tên thao tác gọn khi bắt đầu/kết thúc tool, không kèm arguments/output/log/file contents.
- `cmd/bridge/main.go` (MODIFY): giữ gate connected+paired; chat từ UI Mac và agent dùng cùng `TypeChat` request/confirmation, với sender kind cố định từ từng callback; Host map `mac_user` thành `Mac`, `mac_agent` thành `Agent (Mac)`, từ chối sender kind khác. Duy trì history dùng chung và append Host/Mac đúng vị trí trong luồng confirmation.
- Tạo nhãn gọn theo tool: command → `<project> · Chạy lệnh`; project tool → `<project> · <tool>`; tool Bridge còn lại → tên thao tác dễ đọc. Không đưa nguyên văn command/script arguments, output, log hoặc nội dung file vào activity; không xem `bridge_status`/notification nội bộ là task agent. Dữ liệu project rỗng → nhãn thao tác chung.
- Nhãn chỉ mô tả MCP operation mà Bridge nhận được, không phải task title tổng quát hay đầy đủ hoạt động của Claude Code. Khi idle/không có sự kiện, UI nói rõ Bridge chưa quan sát thấy thao tác nào. Không lưu lịch sử qua phiên.
- Không thêm IPC transport mới, quyền mới, desktop notifications, generic notification queue, system tray behavior, script parser/run button hay dependency UI; badge Settings đếm permission requests đang pending hiện có; informational MCP/config warnings không tính vào badge và vẫn đọc được trong Settings.
- Tests: nhãn cho known/project tools, thiếu metadata, success/error/idle; `run_command` với project `CAI` và command argument đặc trưng phải tạo `CAI · Chạy lệnh` và loại command argument khỏi label; project trống dùng `Chạy lệnh`; project tool giữ `<project> · <tool>`; API sai token/method/body; UI hiển thị Settings/chat và nhãn bằng `textContent`; bảo đảm không đưa args/output vào activity; MCP `chat_send` empty/oversize/disconnected/error; TypeChat route, mapped sender labels, multiline/special-character preservation, Copy exact text; confirm no automatic retry when delivery is unknown; confirm chat content is never dispatched to executor/MCPCall. Real agent deciding when to ask Windows user and native clipboard remain real-host checks.

**Kiểm chứng M4:** Start/Stop AutoCAD được phép; app không được phép → từ chối;
Thoát Bridge không tắt AutoCAD; chat 2 chiều gồm text agent qua cùng chat hiện có,
Windows user nhìn thấy và copy được script nhiều dòng để tự chạy khi cần handoff; chat text
không tự thực thi; mọi thao tác §11 làm được trên UI; activity MCP hiện nhãn đúng,
trạng thái kết thúc đúng và không rò nội dung call.
**- test required:** xác minh với Claude Code/CAI MCP thật và giao diện native trên Mac/Windows.

---

## Milestone 5: Cài đặt & nghiệm thu

### Task 5.1: Cài đặt
- `install.sh` (Mac): chép `LAN Bridge.app` vào `~/Applications/`, chép `bridge` vào `~/.local/bin`,
  in lệnh `claude mcp add` để đăng ký. Lần đầu mở, macOS hỏi quyền "Mạng cục bộ" → Cho phép.
- `install.ps1` (Windows, không cần quyền admin):
  - chép `bridge.exe` vào `%LOCALAPPDATA%\LanBridge\`, tạo shortcut Desktop + Start Menu,
    tạo `config.json` mẫu nếu chưa có;
  - kiểm tra `Get-NetConnectionProfile`: nếu Wi-Fi đang là **Public** → in hướng dẫn dễ hiểu cách chuyển
    sang **Private** (Settings → Network → Wi-Fi → Private), vì Windows 11 mặc định mạng mới là Public
    và khi đó máy Mac không kết nối được;
  - lần đầu chạy, Windows hỏi tường lửa → chọn Allow cho mạng Private.
- Đưa `dist/windows/` sang Windows bằng USB / thư mục chia sẻ / Git. Cập nhật = chép đè và chạy lại.

### Task 5.2: Nghiệm thu
- Chạy 12 mục CRITERIA §11 trên 2 máy thật với repo CAI.
- Đo idle: mỗi bên < 30 MB RAM, CPU ~0% khi không có việc (chỉ heartbeat).
- Ghi kết quả vào `docs/reports/`.

---

## Module dùng chung

- `internal/protocol` — kiểu message + mã lỗi, dùng chung cho host, client, `bridge mcp`.
- `internal/permissions.Resolve` — kiểm tra đường dẫn duy nhất cho Read/Sync/Execute/MCP.
- `internal/store` — đọc `config.json` (chỉ đọc); đọc/ghi `state.json` an toàn (temp → rename).
- `internal/ui` — tray (icon màu theo trạng thái) + HTTP/SSE localhost, dùng cho cả host và client.
- `internal/mcp` — kiểu JSON-RPC/MCP dùng chung; `internal/agentmcp` — `bridge mcp` (M2);
  `internal/mcpfwd` — phiên MCP tới server dự án trên host (M3).
- Log: `log/slog` ra 1 file trong thư mục state, xóa trắng mỗi lần khởi động.

## Cố ý không làm
Nhiều client cùng lúc; tự cập nhật; sync 2 chiều hoặc xóa file; chuyển tiếp resources/prompts
của MCP; chat AI/auto-execute; service Windows/tự khởi động; transport MCP chưa có repo nào dùng;
thư viện mDNS thứ hai; xoay vòng log.

---

Critic CR: PASS (rev 7, independent PRE_CODE_TECHNICAL review; Plan/Milestones alignment and explicit command-label regression oracle confirmed)
(rev 3, 2026-11-02 — docs/reports/2026-11-02-critic-cr-technical-proposal.md)
