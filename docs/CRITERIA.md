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
Windows là Permission Authority; Mac chỉ là Permission Consumer.
**Requested capabilities KHÔNG đồng nhất với Actual Granted capabilities.**
Windows có thể cấp ít hơn, nhiều hơn, hoặc khác quyền Mac yêu cầu
(ví dụ xin Read nhưng được Read + Sync; xin Read + Sync nhưng chỉ được Read).
Windows cũng được tự tạo Grant khi chưa có yêu cầu từ Mac, bằng UI Add Folder.
Mac phải nhận và sử dụng đúng quyền thực tế đã được Windows cấp.
Windows có thể xem/sửa/thu hồi Grant đang hoạt động; thay đổi có hiệu lực
đối với thao tác tiếp theo, thu hồi dừng tác vụ tương ứng đang chạy khi khả thi.

- **Read**: yêu cầu đọc bất kỳ thư mục nào mà user duyệt
  (ví dụ: log AutoCAD khi addin crash).
- **Sync**: ~~chỉ vào thư mục repo dự án và thư mục liên quan trực tiếp.~~
  on-demand vào bất kỳ thư mục nào Windows user đã cấp Sync, không phụ thuộc
  việc thư mục đó có trong `projects` / `related_dirs` hay không.
- **Execute**: ~~chạy command từ thư mục chính của repo — các command an toàn
  thường dùng khi build dự án.~~
  cho phép Shell Command/script trên Windows theo quyền Exec Windows cấp;
  các lệnh build đặt tên sẵn vẫn dùng được để thao tác nhanh.
- **MCP Access**: gọi tool mà repo đang phát triển, thông qua MCP server
  sẵn có của dự án.

Các quyền có thể kết hợp. Không xây thêm cơ chế bảo mật nào khác
ngoài gate cho phép/không cho phép. Không sandbox, không audit log,
không per-tool policy.

Folder Grant là nguồn quyết định quyền Read/Sync, không phải danh sách
`projects` hoặc `related_dirs`. Project/workspace chỉ là metadata cấu hình
và điểm làm việc thuận tiện, không phải điều kiện cấp quyền.
Windows UI phải có Pending Requests (Accept / Reject / chỉnh quyền trước khi
Accept) và Granted Folders (Add Folder / Edit / Revoke / Remove).
Windows chọn folder bằng hộp thoại chọn thư mục của OS; không bắt buộc Mac
gửi request trước và không bắt buộc chỉnh JSON.
Grant cho phép agent thực hiện nhiều tác vụ theo quyền đã cấp mà không
phải xin lại Approval từng thao tác. Không thêm security gate khác.

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
~~Chạy từ thư mục chính của repo dự án.~~
Lệnh build đặt tên sẵn có thể dùng repo/workspace root làm working directory
mặc định. Shell Command có thể chạy theo working directory chỉ định khi
Windows đã cấp Exec.
Trả về exit code, stdout, stderr.
~~Không cho phép shell tùy ý.~~
Cho phép Shell Command/script tùy ý theo quyền Exec Windows đã cấp;
không giới hạn vào registry 8 lệnh build và không tạo shell whitelist riêng.

## 8. Existing MCP forwarding

Proxy mỏng kết nối Mac agent tới MCP server sẵn có trên Windows.
Khi MCP Access được cấp, forward toàn bộ tools của server đó.
~~Bridge không filter hay thêm policy lên tools.~~
Giữ MCP proxy mỏng, nhưng Command Blacklist của từng Project Config
phải được áp dụng tại điểm gọi command/tool có thể xác định rõ.
Khi command AutoCAD nằm bên trong payload của CAI/MCP, CAI command
dispatcher dùng blacklist được cấu hình để từ chối command tương ứng.
Không thêm hệ thống per-tool security policy hay sandbox tổng quát.

## 9. Optional software Start/Stop

Windows user chọn executable cụ thể và cấp quyền Start/Stop.
~~Bridge chỉ launch hoặc graceful close. Không force-kill,
không mở project files,~~
Bridge có thể Start/Stop ứng dụng bằng Windows Shell Command theo Grant,
ưu tiên graceful close khi phù hợp; Exec đã cấp có thể chạy các lệnh đóng
ứng dụng cần thiết trong vòng build/test. Không biến danh sách app thành
shell sandbox/whitelist mới. Bridge không điều khiển GUI hoặc nghiệp vụ
bên trong ứng dụng; các thao tác COM/MCP/Dynamo/LISP thuộc repo CAI/RAI.
Windows UI có Add Application (chọn EXE bằng file picker), quyền Start/Stop
từng app, Edit/Remove và lưu lại trong Config Profile.
Start/Stop độc lập trong UI để dễ quản lý, nhưng không được giả định
chúng ngăn được các lệnh tương đương khi Exec tùy ý đã được cấp.
Load Config không tự chạy Start/Stop.

## 10. Chat

Text panel tối giản 2 chiều: gửi/nhận text, script snippets,
pasted logs, Copy. Không chat AI, không auto-execute.

## 11. Acceptance criteria

1. Cài Mac + Windows bằng shell installer; icon mở app không cần terminal.
2. Pair trên Wi-Fi LAN; mở lại cả 2 → auto reconnect.
3. Exit → ngắt kết nối, hủy tác vụ dở nếu khả thi.
4. Mac request folder → Windows Accept/Reject, edit/revoke permissions.
5. Read works cho files/logs/ranges từ thư mục được duyệt.
6. File nhỏ sync chính xác ~~vào thư mục dự án~~ vào thư mục Windows cấp Sync
   (không bắt buộc `projects`/`related_dirs`); vượt 5 MB → từ chối.
7. ~~Lệnh dev cố định chạy từ repo root,~~ Lệnh dev đặt tên sẵn vẫn hoạt động;
   Shell Command theo Exec Grant chạy được, trả stdout/stderr/exit code.
8. MCP tool thật forward và invoke được.
9. Start/Stop chỉ cho app được cấp phép.
10. Chat gửi text 2 chiều.
11. Không chạy ngầm/qua đêm.
12. Idle tiêu tốn ít CPU/RAM.
13. Windows tự Add Folder bằng UI, chỉnh quyền trước/sau Accept; Mac nhận
    Actual Grant, kể cả trường hợp Windows cấp thừa/cấp thiếu Requested.
14. Windows Edit/Revoke Grant có hiệu lực với phiên đang hoạt động.
15. Read/Sync đã Grant hoạt động cả ngoài `projects`/`related_dirs`,
    không phải khai báo project hay sửa JSON trước.
16. Save/Save As/Rename/Delete/Load Config Profile có tên trong App Data;
    đóng/mở Bridge không làm mất profile.
17. User chọn Load Profile bằng UI hoặc yêu cầu agent `config.load`;
    nạp profile không tự Sync/Build/Exec/Start/Stop.
18. Windows Add Application bằng UI, cho phép sửa quyền Start/Stop;
    Mac được dùng Shell/Exec để đóng/mở AutoCAD khi rebuild DLL.
19. Agent được test liên tục trên Active Drawing, New Drawing hoặc Saved
    Test DWG đã biết đường dẫn, mở trực tiếp qua OS/Shell trong folder được cấp.
20. ~~CAI/MCP blacklist AutoCAD `Documents.Open`, `OPEN` và biến thể qua
    COM/SendCommand/LISP; các COM/MCP thao tác CAD khác không bị chặn.~~
    Project CAI có file blacklist gồm `Documents.Open`, `OPEN` và
    biến thể COM/SendCommand/LISP; CAI/MCP dispatcher áp dụng blacklist,
    không chặn các COM/MCP CAD khác.
21. Bridge không implement blacklist CAD bằng cách phân tích mọi Shell Command;
    CAI/RAI vẫn sở hữu nghiệp vụ test/drawing.
22. Mỗi dự án có đúng hai file config riêng trong App Data: Permission
    Config (folders/programs/grants) và Command Blacklist Config.
23. Windows user Edit/Revoke Permission Config dù blacklist Locked;
    các nút Grant/Edit/Revoke Folder/Program nằm ở Windows UI.
24. Agent đọc repo, theo yêu cầu user được thêm/bớt/sửa blacklist
    khi ~~Windows user đã Unlock thủ công~~ **người dùng đã Unlock
    thủ công trên Mac UI**; Locked thì bị từ chối.
25. File blacklist Locked là read-only qua UI/agent/config API, vẫn
    đọc/load/áp dụng được; Lock state tồn tại qua restart.
26. ~~Chỉ Windows user Unlock thủ công~~ Chỉ **người dùng trực tiếp
    trên Mac UI** được Lock/Unlock; agent không tự Unlock.
27. Project CAI/RAI dùng blacklist riêng; agent không được dùng Shell
    để vòng qua trạng thái Locked của blacklist.
28. Windows UI không quản lý Command Blacklist; Mac UI hiển thị
    danh sách blacklist theo project và nút Lock/Unlock thủ công.
29. Từ Mac UI agent chỉ sửa blacklist khi Unlocked và user yêu cầu;
    không cho Mac UI cấp/sửa/thu hồi Windows Folder/Program Grants.

## 12. Explicit NON-goals

Không xây: Bluetooth, cloud relay, remote desktop, ~~generic shell~~,
OS sandbox, process surveillance, database, multi-user auth,
CI/CD, scheduler, file watcher, large-file sync, per-tool policy,
audit platform, coding agent trên Windows, framework UI nặng,
bất kỳ cơ chế bảo mật thừa nào ngoài gate cho phép/không cho phép.
Generic Shell/Exec theo Grant Windows là mục tiêu được phép, không còn là
NON-goal. Không xây CAD-specific controller hay sandbox trong Bridge.

## 13. Implementation rule

Chọn stack cross-platform đơn giản nhất, ít phụ thuộc nhất.
Tránh over-engineering. Inspect runtime/MCP transports thực tế
trước khi chọn thư viện.

## 14. Windows Permission UI and Actual Grant

- Windows user có thể tự Add Folder mà không chờ request từ Mac, chọn bằng
  OS folder picker; có thể Edit/Revoke/Delete grant bất cứ lúc nào.
- Mac Request và Windows Grant là hai dữ liệu độc lập. User có thể
  Accept/Reject, hoặc sửa quyền theo ý mình trước khi Accept.
- UI hiển thị rõ Pending Requests và Active/Granted Permissions. Mac nhận
  Actual Grant (Read, Sync, Exec, MCP Access và các mục Software Start/Stop
  tương ứng); không coi Accept là đồng ý chính xác request ban đầu.
- Folder Grant không cần khai báo `projects` hoặc `related_dirs`. Windows
  có quyền tự cấp Read Only hoặc Read & Sync cho folder do mình chọn.
- Bridge kiểm tra quyền thực tế đã cấp; không tự đặt thêm gate sau Grant.
  Revoke/Edit cập nhật trạng thái hiệu lực khi đang kết nối.
- Sửa lỗi `ERR_PERMISSION: sync capability is restricted only to project
  root or related_dirs`: nếu Windows đã Grant Sync đúng folder thì không
  trả lỗi chỉ vì folder chưa thuộc `projects`/`related_dirs`.
- Nếu client đã yêu cầu Read Only mà nhận lỗi kiểm tra Sync, xác minh
  requested capability, actual grant và dispatcher, không kết luận thiếu
  project config khi chưa kiểm tra.

## 15. Named Config Profiles in App Data

- Bridge phải có **Config Profiles có tên do user đặt** để phát triển
  nhiều app, ví dụ CAI, RAI, AssetGen hoặc TestBridge.
- **Save, Save As, List, Load/Open, Rename, Delete, Current**; có thể chọn
  khôi phục profile dùng gần nhất khi khởi động.
- Profile lưu lâu dài trong **App Data của chính Bridge** trên máy liên quan,
  không nằm bắt buộc trong repo và không cần chỉnh JSON thủ công.
- UI trên Windows/Mac phải cho phép chọn lại profile phù hợp. **Windows UI**
  quản lý file Permission Config (Folder/Program Grant/Edit/Revoke);
  **Mac UI** quản lý file Command Blacklist (xem/sửa khi Unlocked,
  Lock/Unlock thủ công). Agent trên Mac
  có thể nhận lệnh từ user để load profile đã lưu, ví dụ
  `config.list()`, `config.current()`, `config.save(name)`,
  `config.load(name_or_id)`, `config.delete(name_or_id)`.
  Đây là API logic đề xuất, không khóa chặt tên wire protocol.
- ~~Profile chứa các thiết lập Connection, Folder Grants, Workspace/Project,
  Exec/Build, Software Control và thông tin MCP khi áp dụng.~~
  Mỗi dự án có **đúng hai file config** riêng trong App Data của Bridge:
  (1) **Permission Config**, chứa folders, programs, Actual Granted
  Permissions và tham chiếu thực thi/MCP nếu cần, do **Windows UI** quản lý;
  (2) **Command Blacklist Config**, chứa danh sách lệnh/API bị cấm riêng
  cho dự án, do **Mac UI** quản lý. Connection/pairing chung có thể ở
  Bridge global settings, không thêm config thứ ba cho riêng dự án.
  Quy tắc chỉnh sửa/Lock xem §19.
- **Windows là chủ thể quyết định Grant**: Mac không thể sửa file profile
  phía Mac để tạo/nâng quyền trên Windows. Load profile Windows đã lưu
  có thể khôi phục Grant do user đã cấp, không phải xin duyệt lại chỉ
  vì chọn lại profile.
- Load Config chỉ phục hồi cấu hình; không tự Build, Sync, chạy Shell,
  Start/Stop App hay bắt đầu test.
- Profile persistence khác runtime session: chỉ kết nối/thực thi khi cả
  hai Bridge app đang mở. Exit ngắt kết nối/hủy thao tác phiên theo §3.

## 16. Shell, Application Lifecycle, and Project Boundaries

- Windows user có thể cấp Exec để Mac agent chạy Windows Shell Command
  hoặc script phục vụ development; registry 8 lệnh build vẫn là shortcut
  tiện dụng, **không giới hạn** khả năng Exec được cấp.
- Windows UI chọn executable và Start/Stop permissions từng app.
  Có thể lưu/reuse thông tin này trong Config Profile.
- Ví dụ CAI loop: Stop AutoCAD qua Shell → Rebuild DLL → Sync DLL →
  Start AutoCAD qua Shell → chuẩn bị drawing → Load/Test DLL qua CAI MCP.
  Tương tự cho RAI/Revit khi được cấp quyền.
- Bridge làm connection, permissions, read/sync, shell, software lifecycle,
  MCP forwarding. **CAI/RAI** làm COM/MCP/LISP/Dynamo và mọi thao tác
  bên trong CAD/Revit. Bridge không tự implement lại CAD command tool.
- Exec là quyền mạnh. Không thêm các approval gates hoặc shell whitelist
  mang tính sandbox giả tạo ngoài Criteria.

## 17. AutoCAD Drawing Test Policy and Command Blacklist

**Mục đích:** Tránh agent vô tình dùng AutoCAD OPEN/COM Open để chọn nhầm
working drawing của user khi test; không phải cấm mở tất cả DWG có sẵn.

Khi Windows user đã Grant quyền test phù hợp, agent được thực hiện
Build → Test → Fix → Retest liên tục **không phải xin Approval lại từng
thao tác**. Nguồn drawing được dùng:

1. **Active Drawing** — drawing đang mở, kể cả working drawing hiện tại.
2. **New Drawing** — tạo mới, bao gồm COM `Documents.Add()`.
3. **Saved Test Drawing** — file DWG test có đường dẫn đã biết, nằm trong
   folder được Windows cấp quyền, mở **trực tiếp bằng Windows OS/Shell**
   (file association hoặc Start-Process, kể cả khởi động AutoCAD với
   test DWG được chỉ rõ). Đây không phải AutoCAD in-app OPEN.

~~**CAI/MCP command blacklist** (không phải security sandbox của Bridge):~~
**Command Blacklist được lưu trong file config thứ hai của từng dự án**
(xem §19), không hardcode chung cho tất cả projects. Agent đọc repo để
đề xuất và, theo yêu cầu user, thêm/bớt/sửa entries **chỉ khi Windows
user đã Unlock thủ công**. CAI/MCP áp dụng blacklist khi dispatch
AutoCAD command/COM. Các mục sau là blacklist CAI đã được yêu cầu:

- COM `Application.Documents.Open()`, `AcadDocuments.Open()`,
  `Documents.Open()` và API Open tương đương để chọn một DWG khác.
- AutoCAD in-app commands `OPEN`, `_.OPEN`, `-OPEN` và các biến thể
  cùng ý nghĩa, kể cả gọi qua `SendCommand` hoặc LISP `(command ...)`.
- Không cho phép agent tự chọn working DWG bất kỳ để mở cho test.
- Khi CAI/MCP từ chối lệnh trong blacklist, trả lỗi rõ
  `ERR_COMMAND_BLACKLISTED` cùng tên command/API.
- **Không blacklist** COM `ActiveDocument`, `Documents.Add()`, các thao tác
  vẽ/sửa/đọc drawing đã được phép, hay Shell mở trực tiếp known test DWG
  trong folder đã Grant.
- ~~Bridge vẫn forward MCP theo §8, không filter tools của CAI, không cố
  đọc/phân tích tùy ý mọi shell command để áp đặt blacklist CAD.
  Blacklist là chính sách workflow tại CAI/MCP tool/command dispatch,
  không phải rào cản bảo mật tuyệt đối đối với Exec tùy ý.~~
  Bridge lưu/nạp blacklist riêng của project; dispatcher tương ứng
  kiểm tra lệnh nhận diện được. CAI/MCP chịu trách nhiệm command nằm
  trong payload, không yêu cầu Bridge parse tùy ý mọi Shell Command.
  Blacklist là quy tắc workflow, không phải sandbox chống Exec tùy ý.

## 18. End-to-end Development / Testing

1. Mac và Windows chạy Bridge; pair/reconnect theo §4.
2. User Windows cấp Grant hoặc load lại Named Config Profile đã lưu.
3. Mac agent có thể request thêm quyền; Windows được cấp khác với request.
4. Mac theo Actual Grant: Read logs/source → Exec build/script → Sync small
   source/DLL → Shell Stop/Start ứng dụng → gọi CAI/RAI MCP để test.
5. Khi test AutoCAD chỉ dùng Active Drawing / New Drawing / OS-shell direct
   open Saved Test Drawing theo §17; AutoCAD OPEN/COM Open bị blacklist
   theo Command Blacklist Config của project CAI đang nạp.
6. Lặp lại build/test trong phiên đã Grant, không thêm human gate.
7. Thoát một trong hai Bridge app → kết thúc phiên, giữ nguyên các
   Config Profiles đã lưu để sử dụng trong lần sau.

Không phát triển CI/CD, background runner, scheduler, general remote
desktop, OS sandbox hay control-plane phức tạp ngoài mục tiêu Bridge.

## 19. Exactly Two Per-Project Config Files and Manual Blacklist Lock

Mỗi dự án (CAI, RAI, AssetGen, ...) thuộc một Named Config Profile và có
**đúng hai file config riêng** trong App Data của Bridge. Tên file dưới
đây chỉ là ví dụ; không cố định schema hoặc filename trong Criteria:

```text
App Data/TestBridge/profiles/<project-id>/
  permissions.json        # Windows-owned folders/apps/capability grants
  command-blacklist.json  # Project commands/APIs to avoid
```

**File 1 — Permission Config (do Windows user quản lý):**

- Lưu folders, programs/executables, Read/Sync/Exec, MCP Access,
  Start/Stop và các Actual Grants. Lưu workspace/project reference
  nếu cần vận hành.
- Windows user tự Add Folder/App, Accept/Reject/Edit/Revoke qua
  **Windows UI**; không bắt buộc Mac yêu cầu trước hoặc sửa JSON.
  Permission UI và mọi thay đổi Grant/Permission được thực hiện
  trên máy Windows, không chuyển sang Mac UI.
- Agent chỉ Request quyền, **không tự sửa file permission** nhằm cấp
  hoặc nâng quyền cho mình.
- **Lock/Unlock của blacklist KHÔNG khóa file này**. Windows vẫn
  được chỉnh sửa quyền hoặc Revoke bất cứ lúc nào.

**File 2 — Command Blacklist Config (agent-assisted):**

- Chứa danh sách command/API blacklist riêng của dự án. Agent
  **đọc/khảo sát repo** trước khi đề xuất cấu hình và chỉ chỉnh
  (thêm/bớt/sửa) theo yêu cầu của user.
- Agent **chỉ được phép sửa file này khi ~~Windows user đã Unlock thủ công~~
  người dùng đã Unlock thủ công trên Mac UI**.
  ~~Windows UI phải có nút **Lock / Unlock**~~
  **Mac UI có nút Lock / Unlock và danh sách Command Blacklist theo
  project**, thể hiện rõ trạng thái.
- Khi **Locked**: nội dung blacklist là read-only; cấm ghi đè,
  thêm/bớt/sửa/xóa bằng UI hoặc API agent; vẫn được xem, load và áp dụng
  để kiểm thử. Agent không được dùng Shell/Sync để vòng qua Lock.
- Khi **Unlocked**: Agent được user yêu cầu có thể cập nhật blacklist.
  ~~Chỉ user Windows được bấm Lock/Unlock~~
  **Chỉ người dùng thao tác trực tiếp trên Mac UI được bấm Lock/Unlock**;
  không có auto-unlock hoặc API Lock/Unlock dành cho agent.
- Lock state được lưu qua restart (có thể là metadata chung của Bridge,
  không tạo file config thứ ba cho dự án).
- Lock chỉ khóa **chỉnh sửa file blacklist**, không tạo thêm gate
  approval trước từng lần Build/Test/Read/Sync/Exec/MCP.

**Profile và Dispatch:**

- Save/Load profile sử dụng đúng cặp file của dự án; không đưa blacklist
  của CAI sang RAI, không overwrite file blacklist đang Locked.
  **Windows UI chỉ quản lý Permissions; Mac UI chỉ quản lý Blacklist.**
  Vị trí UI không tự thay đổi quy tắc lưu file trong Bridge App Data.
  Trạng thái Locked được đồng bộ để cấm chỉnh blacklist ở mọi đường ghi.
- Bridge nạp blacklist của dự án; dispatcher đủ thông tin để nhận
  diện command/API có trách nhiệm kiểm tra nó. Với AutoCAD in-app
  OPEN và COM Open trong MCP payload, CAI/MCP dispatcher kiểm tra.
  Bridge không parse mọi Shell Command hay xây thêm security sandbox.
- Connection/pairing global settings của Bridge có thể lưu riêng;
  yêu cầu **hai file** áp dụng cho config **mỗi dự án**, không cấm
  Bridge có các file dữ liệu toàn cục phục vụ hoạt động.

## Technical Advisor decision checkpoint

Advisor đã đánh giá: khả thi cao, Go native binary đáp ứng
tiêu chí nhẹ + tin cậy. Phạm vi cá nhân → gate đơn giản là đủ.

## Human approvals and versions

- v1.0 APPROVED: 2026-11-02 — Duyệt ban đầu.
- v1.0a APPROVED: 2026-11-02 — Human nhấn mạnh: tránh over-engineering,
  không thêm bảo mật thừa, gate đơn giản cho phép/không cho phép là đủ.
  Bridge chỉ gate command từ repo root, command build thường dùng,
  và quyền gọi tool MCP của dự án.
- v1.1 APPROVED: 2026-11-03 — Bổ sung UI Windows quản lý grant/folder/app,
  Requested ≠ Actual Grant, Named Config Profiles trong App Data,
  Shell/Exec phục vụ rebuild/restart app, CAI Drawing Test Policy và
  AutoCAD OPEN/COM blacklist. Ý Criteria cũ mâu thuẫn được giữ nguyên
  bằng Markdown ~~gạch bỏ~~, không xóa nội dung lịch sử.
- v1.2 HUMAN REQUIREMENT: 2026-11-03 — Mỗi project có đúng hai
  file: Permission Config do Windows quản lý và Command Blacklist
  Config được AI chỉnh theo yêu cầu user chỉ khi manual Unlock.
  Lock blacklist không ảnh hưởng Edit/Revoke Permission Config;
  nội dung cũ xung đột tiếp tục giữ lại bằng gạch bỏ.
- v1.2a HUMAN CLARIFICATION: 2026-11-03 — Windows UI độc quyền
  Grant/Edit/Revoke Folder và Program. **Mac UI** sở hữu Command
  Blacklist và nút Lock/Unlock thủ công; agent chỉ chỉnh blacklist
  theo yêu cầu user khi đã Unlocked. Chỉ thay UI authority, không
  tự đặt thêm gate hay thay nơi lưu dữ liệu chỉ vì đổi UI.
