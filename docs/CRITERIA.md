# Product theory — LAN Test Bridge (v1.0)

Status: APPROVED

## 1. What we are building and why

LAN Test Bridge kết nối máy phát triển macOS (Apple Silicon) với môi trường
build/test trên Windows 10/11 qua Wi-Fi LAN nội bộ. Dùng cá nhân.

Luồng chính: Git xử lý thay đổi lớn → khi test trực tiếp trên Windows,
phát sinh lỗi → sửa nhỏ trên Mac → Bridge sync file sửa sang Windows →
chạy script rebuild/test → đọc kết quả/log → lặp lại.

Bridge là gate đơn giản: cho phép hoặc không cho phép. User tin agent —
nếu agent cố tình phá hoại thì quyền sync code + execute đã đủ nguy hiểm,
không cần thêm cơ chế bảo mật nào khác ngoài gate.

Phiên làm việc ngắn, có người dùng quan sát, theo yêu cầu.
Không chạy ngầm qua đêm, không CI/CD tự động.

## 2. Who uses it and priorities

- Một nhà phát triển cá nhân, hai máy trên cùng mạng Wi-Fi.
- Ưu tiên #1: Nhẹ nhất có thể — ít phụ thuộc, binary gọn, khởi động nhanh,
  idle gần như không tiêu tốn CPU/RAM.
- Ưu tiên #2: Độ tin cậy cao — kết nối ổn định, sync file chính xác,
  lỗi báo rõ ràng, không mất dữ liệu.
- Không over-engineer. Không xây biện pháp bảo mật thừa thãi.

## 3. Install and launch

- macOS: `./install.sh`; Windows: `.\install.ps1`.
- Cài đặt chỉ phụ thuộc thiết yếu, tạo launcher/icon để bàn.
- Mở app thủ công bằng icon. Không tự khởi động cùng OS.
- Đóng app (Exit thật sự) → kết thúc phiên, hủy tác vụ dở nếu khả thi,
  ngắt kết nối. Không Docker, WSL, database hay dịch vụ chạy ngầm.

## 4. Discovery, pairing, connection

- V1 chỉ Wi-Fi LAN. Không Bluetooth/fallback.
- Cổng server LAN: mặc định dùng cổng lạ, ít bị trùng (ví dụ `54823` thuộc dải dynamic/private, tránh xa các cổng thông dụng như 3000, 8080, 5000), có thể cấu hình đổi cổng qua flag/config khi cần.
- Discover qua thư viện/giao thức nội bộ (mDNS broadcast cổng đang listen); manual IP:port fallback nếu
  router chặn discovery.
- Lần đầu: Windows hiện mã ghép nối tạm → nhập trên Mac → ghi nhận
  thiết bị tin cậy. Mã ngắn không dùng làm bí mật dài hạn.
- Các lần sau: tự động kết nối lại khi cả 2 app mở.
- Kết nối mã hóa bằng thư viện crypto chuẩn. Không tự chế crypto.
- Lưu trạng thái ghép nối/quyền giữa các phiên; cho phép Unpair/Revoke.

## 5. Folder permission model

Gate đơn giản: Mac agent yêu cầu quyền trên thư mục, Windows user
Accept hoặc Reject. Có thể sửa/thu hồi bất kỳ lúc nào.

- **Read**: yêu cầu đọc bất kỳ thư mục nào mà user duyệt
  (ví dụ: log AutoCAD khi addin crash).
- **Sync**: chỉ vào thư mục repo dự án và thư mục liên quan trực tiếp.
- **Execute**: chạy command từ thư mục chính của repo — các command an toàn
  thường dùng khi build dự án.
- **MCP Access**: gọi tool mà repo đang phát triển, thông qua MCP server
  sẵn có của dự án.

Các quyền có thể kết hợp. Không xây thêm cơ chế bảo mật nào khác
ngoài gate cho phép/không cho phép. Không sandbox, không audit log,
không per-tool policy.

## 6. Read and Sync

- Read: list, metadata, đọc file hoặc line/byte ranges từ thư mục được duyệt.
- Sync: on-demand, Mac → Windows, file nhỏ (source/scripts/config/DLLs).
  Giới hạn mặc định 5 MB/request (configurable). Từ chối nếu vượt quá.
- Không file watcher, không background sync.
- Git riêng biệt cho thay đổi lớn.

## 7. Fixed development commands

Danh sách lệnh cố định thường dùng khi build dự án: `build`, `clean-build`,
`install`, `restore`, `doctor`, `test`, `status`, `version`.

Ánh xạ tới script/tooling thực tế qua config đơn giản.
Chạy từ thư mục chính của repo dự án.
Trả về exit code, stdout, stderr.
Không cho phép shell tùy ý.

## 8. Existing MCP forwarding

Proxy mỏng kết nối Mac agent tới MCP server sẵn có trên Windows.
Khi MCP Access được cấp, forward toàn bộ tools của server đó.
Bridge không filter hay thêm policy lên tools.

## 9. Optional software Start/Stop

Windows user chọn executable cụ thể và cấp quyền Start/Stop.
Bridge chỉ launch hoặc graceful close. Không force-kill,
không mở project files, không điều khiển GUI.

## 10. Chat

Text panel tối giản 2 chiều: gửi/nhận text, script snippets,
pasted logs, Copy. Không chat AI, không auto-execute.

## 11. Acceptance criteria

1. Cài Mac + Windows bằng shell installer; icon mở app không cần terminal.
2. Pair trên Wi-Fi LAN; mở lại cả 2 → auto reconnect.
3. Exit → ngắt kết nối, hủy tác vụ dở nếu khả thi.
4. Mac request folder → Windows Accept/Reject, edit/revoke permissions.
5. Read works cho files/logs/ranges từ thư mục được duyệt.
6. File nhỏ sync chính xác vào thư mục dự án; vượt 5 MB → từ chối.
7. Lệnh dev cố định chạy từ repo root, trả stdout/stderr/exit code.
8. MCP tool thật forward và invoke được.
9. Start/Stop chỉ cho app được cấp phép.
10. Chat gửi text 2 chiều.
11. Không chạy ngầm/qua đêm.
12. Idle tiêu tốn ít CPU/RAM.

## 12. Explicit NON-goals

Không xây: Bluetooth, cloud relay, remote desktop, generic shell,
OS sandbox, process surveillance, database, multi-user auth,
CI/CD, scheduler, file watcher, large-file sync, per-tool policy,
audit platform, coding agent trên Windows, framework UI nặng,
bất kỳ cơ chế bảo mật thừa nào ngoài gate cho phép/không cho phép.

## 13. Implementation rule

Chọn stack cross-platform đơn giản nhất, ít phụ thuộc nhất.
Tránh over-engineering. Inspect runtime/MCP transports thực tế
trước khi chọn thư viện.

## Technical Advisor decision checkpoint

Advisor đã đánh giá: khả thi cao, Go native binary đáp ứng
tiêu chí nhẹ + tin cậy. Phạm vi cá nhân → gate đơn giản là đủ.

## Human approvals and versions

- v1.0 APPROVED: 2026-11-02 — Duyệt ban đầu.
- v1.0a APPROVED: 2026-11-02 — Human nhấn mạnh: tránh over-engineering,
  không thêm bảo mật thừa, gate đơn giản cho phép/không cho phép là đủ.
  Bridge chỉ gate command từ repo root, command build thường dùng,
  và quyền gọi tool MCP của dự án.
