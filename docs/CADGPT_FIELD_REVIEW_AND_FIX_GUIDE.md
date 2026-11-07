# LanBridge: nhận xét thực tế từ CadGPT và hướng dẫn sửa

Ngày: 2026-11-07. Nguồn: phiên làm việc Mac ↔ Windows để chạy test CadGPT (nhánh `wip/vision-2026-11-06`). Tài liệu này ghi lại những gì đã quan sát, không phải kết luận từ đọc code. Mục "Nguyên nhân nghi ngờ" là giả thuyết cần kiểm chứng trong code.

## 1. Mục tiêu sử dụng (điều người dùng cần)

Người dùng là một người làm việc trên Mac với Claude Code, và có một máy Windows làm máy chạy thử (build, test, AutoCAD). Mục tiêu:

1. Không phải gõ lệnh hay bấm nhiều lần để kết nối lại mỗi ngày.
2. Agent trên Mac đọc được trạng thái và chạy được lệnh trên Windows mà không phụ thuộc vào việc phiên Claude có bị ngắt hay không.
3. Khi có lỗi, thông báo cho biết chính xác bước nào hỏng và phải làm gì tiếp.
4. Không cần code chạy liên tục trên Windows. Bridge chỉ là đường truyền có quyền kiểm soát.
5. Người dùng có thể dùng Bridge trực tiếp từ terminal, không cần agent.

Nếu Bridge không đạt các mục trên, nó không có giá trị với cách làm việc này.

## 2. Những gì đã quan sát

### 2.1 Phiên MCP bị khóa bởi tiến trình cũ (nghiêm trọng nhất)
- `bridge mcp` có khóa một instance (`internal/agentlock`, file `mcp.lock`). Khi phiên Claude kết thúc hoặc bị ngắt mà tiến trình con không thoát, khóa vẫn còn.
- Phiên mới báo "MCP server is already running" và không có tool nào. Phải tìm pid và kill thủ công nhiều lần trong một ngày.
- Tiến trình bị kẹt có Claude cha đã chết hoặc đang chạy phiên khác. Bridge không kiểm tra điều này.
- Các lần kill thủ công (pid 25798, 57824/57826, 64667, 11402) đều là tiến trình `bridge mcp` cũ.

### 2.2 Tool MCP biến mất giữa phiên
- Trong phiên, `lanbridge` báo `CONNECTION_CLOSED`. Sau đó toàn bộ tool `mcp__lanbridge__*` (43 tool) bị gỡ khỏi phiên, chỉ load lại khi Claude reconnect.
- Mỗi lần như vậy phải làm `/mcp` hoặc mở phiên mới. Điều này làm phiên không thể tiếp tục công việc dài.

### 2.3 Kết quả rỗng thay vì lỗi
- `bridge_status`, `windows_config` (status/list), `config_list`, `config_current` đều trả về **rỗng** nhiều lần, không có thông báo nào.
- Kết quả rỗng khiến tôi không biết bridge đang ở trạng thái nào, và mất nhiều vòng thử.
- Khi lệnh trả về `CONFIG_NOT_OPEN`, `ERR_PROJECT`, `OUTSIDE_SCOPE`, mỗi lỗi lại cần một bước khác nhau mà bridge không hướng dẫn rõ.

### 2.4 Hai trạng thái kết nối không đồng nhất
- Tiến trình UI/client (`bridge client`, pid 60317) báo đã kết nối với Windows.
- Tiến trình MCP trong phiên báo `connected: false` hoặc không có output.
- Người dùng thấy "bridge vẫn kết nối, gửi chat được" nhưng agent không dùng được tool. Hai trạng thái không khớp và không có cách nào biết chúng khác nhau.

### 2.5 Địa chỉ Windows thay đổi, không tự tìm lại
- Đã lưu `192.168.1.231:54823`. Sau đó Windows ở `192.168.1.249:54823`. ARP cho `.231` là `incomplete`.
- `state.json` chỉ cập nhật `last_connected_addr` khi kết nối thành công, nên không có thông tin để tự tìm lại.
- Discovery (`internal/transport/discovery.go`) không tự tìm được Windows khi đổi IP, trong khi `bridge_status` không hiển thị địa chỉ hiện tại.

### 2.6 Phạm vi thư mục và cấu hình project trên Windows (nghiêm trọng)
- Phạm vi mặc định là thư mục cài đặt `C:\Users\...\LocalCache\Local\LanBridge`. Thư mục project `D:\00 Other Works\Vibecode\cadgpt-wip-vision` nằm ngoài phạm vi.
- `grant_folder` trả `OUTSIDE_SCOPE` và gợi ý dùng `request_access`.
- `request_access` được Windows duyệt (approved: read, sync, execute, mcp). Nhưng sau đó `grant_folder` vẫn `OUTSIDE_SCOPE`, `open_project` báo `opened` nhưng `run_command` vẫn trả `CONFIG_NOT_OPEN`.
- `setup_project` báo `CONFIG_ALREADY_EXISTS: use Save on existing profile`. `bootstrap` tạo project `cadgpt-wip-vision` nhưng `status` vẫn hiện `project_id: CAI`.
- Kết luận: approval và cấu hình project là hai thứ tách rời, và không có bước nào đảm bảo project dùng được sau khi approve.

### 2.7 Lệnh dài bị ngắt
- Một lần gọi bridge bị `perl alarm 120` (script ở ngoài, không phải bridge) giết giữa chừng. Tool MCP cũng có giới hạn thời gian.
- `wait_seconds` tối đa 600, nên npm ci + build + test có thể vượt. Phải dùng `run_command` rồi `command_result` polling, nhưng job không được lưu bền qua phiên mới.

### 2.8 Chat một chiều trên thực tế
- `chat_send` trả "Tin nhắn đã được xác nhận trong chat Bridge", nhưng agent không có tool đọc lại tin nhắn trả lời. Phải nhờ người dùng dán nội dung vào.
- Nếu muốn hai agent phối hợp, cần inbox có ID và cursor.

### 2.9 Cần thao tác tay mỗi lần
- Mỗi lần khởi động lại, người dùng phải mở Bridge trên cả hai máy, bấm chấp nhận trên Windows, và đôi khi dừng tiến trình.
- Không có cách nào biết trước bước nào sẽ cần người dùng.

## 3. Nguyên nhân nghi ngờ (cần kiểm chứng trong code)

| Hiện tượng | Vị trí nghi ngờ | Kiểm chứng |
|---|---|---|
| Khóa MCP không tự giải phóng | `internal/agentlock/lock.go`, `lock_unix.go`, `lock_windows.go` | **Đã kiểm chứng (một phần).** Khóa không ghi pid, `started_at` hay `ppid`; file khóa rỗng và chỉ dùng `flock` (Unix) hoặc `LockFileEx` (Windows). Hệ điều hành tự nhả khóa khi tiến trình chết, nên khóa "mồ côi" không tồn tại; các pid kẹt là tiến trình `bridge mcp` vẫn còn sống. `Run()` chỉ thoát khi stdin EOF (`server.go:102-110`), không có kiểm tra cha đã chết hay timeout. Cần xác minh bằng `ps -o pid,ppid,command -p <pid>`: nếu `ppid` = 1 thì cha đã chết nhưng pipe stdin vẫn mở. |
| Tool biến mất giữa phiên | `internal/agentmcp/server.go`, `auth.go` | **Đã kiểm chứng (một phần).** `tools/list` luôn trả 14 tool cố định; tool của project chỉ được thêm khi đọc được `mcp_auth.json` (`server.go:439`). Khi bridge client tắt và file auth bị xóa, lần `tools/list` kế tiếp không có tool project và không có thông báo. Con số 43 của phiên gồm 14 tool cố định và 29 tool project. Việc gỡ toàn bộ tool khi `CONNECTION_CLOSED` là hành vi phía Claude Code, cần log phía Claude để xác minh. Không có nhánh nào trong `server.go` tự thoát khi transport lỗi; chỉ thoát khi EOF. |
| Kết quả rỗng | `internal/agentmcp/server.go:730-737` (`forwardToBridgeClient`) | **Đã kiểm chứng.** Hàm này trả nguyên object JSON từ bridge client mà không kiểm tra `content`. Nếu bridge client trả `{}` hoặc object không có `content`, Claude nhận kết quả rỗng. `callBridgeClient` chỉ từ chối `null`. |
| Trạng thái không khớp giữa UI và MCP | `cmd/bridge`, `internal/transport/client.go` | **Đã kiểm chứng một phần.** MCP không mở kết nối riêng tới Windows: mỗi lệnh gọi là HTTP tới bridge client trên `127.0.0.1` qua `mcp_auth.json` (`server.go:739-785`). Vì vậy `connected: false` của MCP phản ánh trạng thái bridge client. Cần đọc `internal/transport/client.go` để xác định vì sao UI báo đã kết nối trong khi MCP báo chưa. |
| Địa chỉ không tự cập nhật | `internal/transport/discovery.go`, `state.json` | Có thử lại discovery khi kết nối thất bại không? |
| OUTSIDE_SCOPE sau approve | `internal/permissions`, `internal/projectconfig` | Approve có ghi vào phạm vi không, hay chỉ ghi capability? |
| CONFIG_NOT_OPEN | `internal/projectconfig` | `open_project` có lưu trạng thái open cho tiến trình hiện tại không? Mỗi lần gọi MCP có tạo tiến trình mới không? |
| CONFIG_ALREADY_EXISTS vs project CAI | `windows_config` handler | Có hai nguồn project id không? |

## 4. Yêu cầu sản phẩm (cách làm việc mong muốn)

### 4.1 Kết nối
- Sau lần ghép nối đầu tiên, kết nối tự động khi cả hai máy mở. Không bấm gì.
- Nếu IP đổi, tự tìm lại theo fingerprint của cặp ghép nối (quét subnet hoặc mDNS), không cần sửa tay.
- Trạng thái luôn hiển thị: đã kết nối / đang tìm / đã ngắt, kèm địa chỉ hiện tại, thời điểm, và lý do nếu không kết nối.

### 4.2 Tiến trình và khóa
- Chỉ một tiến trình giữ kết nối đến Windows tại một thời điểm. Các tiến trình khác (UI, MCP, CLI) đi qua tiến trình đó (socket cục bộ), không tự mở kết nối riêng.
- Khóa ghi kèm pid và thời điểm. Nếu chủ khóa đã chết hoặc stdin đã đóng, tự giải phóng và ghi log.
- MCP thoát khi stdin đóng. Không để tiến trình con sống sau phiên Claude.
- Lệnh `bridge doctor` kiểm tra từng bước và in PASS/FAIL kèm gợi ý sửa.

### 4.3 Tool và kết quả
- Không bao giờ trả kết quả rỗng. Mỗi tool trả JSON có `ok`, `state`, `error_code`, `message`, `next_step`.
- Tool trạng thái (`bridge_status`) luôn trả đủ: pairing, địa chỉ, host connected, project id, project open, project ready, capability, phiên bản.
- Mỗi `error_code` có một bước sửa rõ, ví dụ `OUTSIDE_SCOPE` → "Windows cần duyệt thư mục X tại Settings > Cho phép".

### 4.4 Project và quyền
- Một project = (id, thư mục Windows, capability, lệnh build/test). Tạo bằng một thao tác idempotent: `project_create`, `project_list`, `project_open`, `project_close`.
- Approve trên Windows phải tạo hoặc cập nhật project ngay lập tức và đưa nó vào trạng thái open. Không yêu cầu bước thứ hai.
- Thư mục ngoài phạm vi mặc định được chấp nhận khi người dùng chọn trong giao diện Windows. Không cần ràng buộc vào thư mục cài đặt.
- `list` luôn trả danh sách, kể cả khi rỗng (`{"projects": []}`).

### 4.5 Lệnh và job
- Lệnh chạy dưới dạng job bền: có `job_id`, trạng thái, log stream, exit code, lưu trên Windows.
- `command_result` đọc được sau khi phiên MCP mới bắt đầu. Job không mất khi MCP đứt.
- Không có timeout cứng phía MCP cho job. Agent poll bằng `command_result` với `wait_seconds` tối đa 60.

### 4.6 Chat hai chiều
- `chat_send` trả `message_id`.
- `chat_read` (inbox) trả tin nhắn chưa đọc theo `cursor`, có người gửi, thời điểm và ID.
- Cho phép agent đọc lại mà không cần người dùng dán.

### 4.7 Sử dụng không cần agent
- CLI `bridge status`, `bridge project list`, `bridge run <project> <command>`, `bridge logs <job>`, `bridge doctor` hoạt động trên terminal.
- Người dùng có thể làm việc hoàn toàn qua CLI nếu không muốn dùng agent.

## 5. Hướng dẫn sửa (theo thứ tự ưu tiên)

### Giai đoạn A: Ổn định tiến trình (chặn mọi việc khác)
1. Thêm `bridge mcp --status` và `bridge mcp --takeover`. Khóa ghi `{pid, started_at, parent_pid}` ngay sau khi lấy được `flock`/`LockFileEx`. Hiện tại khóa không chứa gì, nên bước này là thay đổi mới, không phải sửa lỗi.
2. Khi khởi động MCP: nếu `flock` bị giữ, đọc `{pid, started_at, parent_pid}` từ khóa và in pid cùng lệnh để dừng (`kill <pid>`). Không tự kill tiến trình khác (xem mục 7). Nếu khóa rỗng hoặc thiếu thông tin, báo chưa xác định được chủ khóa.
3. MCP thoát khi stdin EOF (đã có sẵn trong `Run()`). Bổ sung: thoát khi cha của tiến trình đã chết (`ppid` = 1 trên Unix, hoặc cha không còn tồn tại), và kiểm tra định kỳ. Nếu stdin đóng nhưng tiến trình vẫn kẹt, ghi log lý do.
4. Test: giết Claude cha, khởi động phiên mới → MCP tự phục hồi không cần thao tác tay.

**Kiểm thu A:** Chạy 5 vòng: mở Claude, kill Claude không sạch, mở lại. Tiêu chí: tool available trong < 10 giây, không cần kill thủ công.

### Giai đoạn B: Trạng thái và lỗi rõ ràng
1. Chuẩn hóa response: `{ok, state, error_code, message, next_step, data}`. Xóa mọi nhánh trả rỗng.
2. Tách rõ `paired`, `discovered_addr`, `connected`, `project_open`, `project_ready`.
3. Viết `bridge doctor`.

**Kiểm thu B:** Tắt Windows bridge → `bridge_status` trả `connected: false, error_code: HOST_UNREACHABLE, next_step: "Mở Bridge trên Windows"`. Không có output rỗng trong bất kỳ trường hợp nào.

### Giai đoạn C: Kết nối tự động
1. Lưu fingerprint của host. Khi kết nối thất bại, quét `/24` của IP cũ và thử cổng 54823 song song, xác minh fingerprint.
2. Cập nhật `last_connected_addr` khi tìm thấy host.
3. Trong lúc chờ, trạng thái là `searching`, không phải `error`.

**Kiểm thu C:** Đổi IP Windows (hoặc giả lập bằng cách sửa `last_connected_addr`) → trong vòng 30 giây tự kết nối lại.

### Giai đoạn D: Project và quyền
1. Hợp nhất nguồn project id: một file duy nhất, một ID, không có trường hợp `CAI` và `cadgpt-wip-vision` cùng tồn tại mà không rõ.
2. `project_create` idempotent. Trả `already_exists` với dữ liệu hiện có, không phải lỗi.
3. Approve từ Windows tạo project và set open ngay.
4. Kiểm tra `OUTSIDE_SCOPE` chỉ áp dụng khi chưa có approve cho thư mục đó.

**Kiểm thu D:** Từ Mac: `project_create` thư mục ngoài scope → Windows approve → `run_command` chạy được ngay, không có `CONFIG_NOT_OPEN`.

### Giai đoạn E: Job bền và chat hai chiều
1. Job lưu trên Windows dưới `%APPDATA%\LanBridge\jobs\<id>\` gồm `status.json` và `output.log`.
2. `command_result` đọc từ file, dùng được sau khi MCP khởi động lại.
3. `chat_read` với cursor, `message_id` cho `chat_send`.

**Kiểm thu E:** Chạy `npm ci` (> 5 phút) → kill MCP giữa chừng → phiên mới → `command_result` trả log đầy đủ và exit code.

### Giai đoạn F: Tự động hóa thao tác tay
1. Nhớ cặp ghép nối và approval vĩnh viễn, có thể thu hồi trong Settings.
2. Thông báo trên Windows khi có yêu cầu mới, không chỉ dựa vào cửa sổ đang mở.

## 6. Ràng buộc bảo mật (giữ nguyên)
- Không đưa token, pairing code hoặc URL có token vào log, chat, hoặc file docs.
- Mọi quyền ghi/chạy lệnh trên Windows phải được người dùng duyệt trên Windows. Không có bypass.
- Không ghi cert/private key vào file cấu hình được commit.

## 7. Việc cần tránh
- Không thêm tool mới trước khi giai đoạn A–B xong.
- Không để MCP tự động kill tiến trình khác. Chỉ báo cáo và để người dùng quyết định.
- Không dùng `perl alarm` hoặc timeout cứng trong script ngoài để cắt lệnh dài.

## 8. Câu hỏi mở cho người dùng khi làm tiếp
- Có chấp nhận chạy bridge như một agent nền trên Windows (khởi động cùng Windows) không?
- Có cần hỗ trợ nhiều project đồng thời không?
- Ngưỡng dữ liệu tối đa cho `sync_files` là bao nhiêu?

## 9. Phụ lục: sự kiện tham chiếu
- Pid giữ khóa MCP: 25798, 57824, 57826, 64667, 11402 (đều là `bridge mcp` còn sống; cha là các phiên Claude đã chết hoặc đang chạy). Cần `ps` để xác nhận `ppid` và trạng thái stdin trước khi kết luận nguyên nhân.
- Windows địa chỉ: `.231` (cũ, không phản hồi), `.249` (mới, cổng 54823 mở).
- Lỗi Windows: `ERR_PROJECT`, `CONFIG_NOT_OPEN`, `OUTSIDE_SCOPE`, `CONFIG_ALREADY_EXISTS`, `ERR_REMOTE_CONFIG`.
- Test CadGPT cần kiểm chứng sau khi bridge hoạt động: npm ci, build, `node --test tests/*.test.mjs` bằng Node 22, và so với CI Windows (run 42244852421).

## 10. Trạng thái triển khai (2026-11-07)

Đã làm, có test tự động:
- **A (khóa tiến trình):** khóa ghi `{pid, ppid, started_at}`; khi bận báo pid và lệnh dừng; MCP tự thoát khi cha kết thúc (kiểm tra mỗi 2 giây, đã chạy end-to-end); `bridge mcp --status`.
- **B (trạng thái lỗi):** mọi lỗi có `error_code` và `next_step`; kết quả rỗng từ host được báo là `EMPTY_RESULT` kèm dữ liệu thô; `bridge_status` trả đủ `connected`, `paired`, `host`, `project_open`, `project_ready`, `state`; `bridge doctor` in PASS/FAIL với cách sửa.
- **C (tự tìm lại host):** khi host không kết nối được, quét /24 của địa chỉ cũ và chỉ kết nối tới địa chỉ có đúng chứng chỉ đã ghép nối; trạng thái `searching` trong lúc quét.
- **D (project):** `setup_project` và `bootstrap` coi "đã tồn tại" là thành công; `request_access` nhận `project_id` và Windows mở project khi user duyệt.
- **E (job và chat):** job hoàn thành được lưu tại `<config>/jobs/<id>/` và đọc lại được sau khi khởi động lại; job đang chạy khi host dừng được đánh dấu `interrupted`; `chat_send` trả `message_id`; tool mới `chat_read` đọc theo cursor và bỏ qua tin do agent gửi.
- **F (tự động hóa):** ghép nối và duyệt quyền đã được lưu bền từ trước (đã kiểm tra); thông báo Windows khi có yêu cầu quyền mới, giới hạn 1 thông báo mỗi 10 giây.

Chưa làm hoặc chưa kiểm chứng:
- `--takeover` (kill tiến trình khác) chưa làm, theo mục 7.
- Blacklist của Mac vẫn phải được mở trên Mac trước khi chạy lệnh. Đây là cơ chế bảo vệ có chủ đích, không bị bỏ qua.
- `wait_seconds` của `command_result` vẫn tối đa 600 (tài liệu đề xuất 60).
- Thông báo Windows (PowerShell toast) và toàn bộ luồng Windows chỉ được biên dịch chéo, chưa chạy trên Windows.
- Chưa kiểm chứng trên LAN thật: đổi IP thật, Windows thật, hoặc Mac và Windows cùng ghép nối.
- Mục 8: đã chọn tạm (1) không tự khởi động cùng Windows, (2) một project đang mở, (3) giới hạn sync vẫn 5 MB.
