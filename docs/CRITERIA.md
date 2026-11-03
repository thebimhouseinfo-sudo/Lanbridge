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
  Auto-reconnect chỉ khôi phục kết nối transport, **không tự mở
  Config Profile**. Chỉ được dùng các chức năng project khi Windows user
  đã mở thủ công Permission Config và Mac đã mở đúng Command Blacklist
  Config của **cùng project**.
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
~~Giữ MCP proxy mỏng, nhưng Command Blacklist của từng Project Config
phải được áp dụng tại điểm gọi command/tool có thể xác định rõ.
Khi command AutoCAD nằm bên trong payload của CAI/MCP, CAI command
dispatcher dùng blacklist được cấu hình để từ chối command tương ứng.~~
Bridge dùng chung cho **mọi repository và ứng dụng Windows**, không chỉ
CAI/RAI. Mặc định không cấm tool nào của MCP server dự án. Khi Windows
đã Grant MCP Access, hai config đã Open cùng Project ID (§20), và
blacklist Mac đã load thành công, Mac được dùng **tất cả tools mà project
cung cấp**, trừ những entry thực tế có trong blacklist của riêng project.
Blacklist rỗng là danh sách hợp lệ, không chặn tool nào. **Blacklist
thiếu/chưa load không đồng nghĩa với blacklist rỗng**: cấm project
operations, không được tự bỏ qua blacklist để thực thi.
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
16. Save/Save As/Rename/Delete/Load Config Profile có tên trong App Data
    **cục bộ từng máy**; đóng/mở Bridge không làm mất profile.
17. ~~User chọn Load Profile bằng UI hoặc yêu cầu agent `config.load`;~~
    Windows user phải **tự Open** Permission Config đã lưu trên Windows UI.
    Mac user có thể Open blacklist bằng Mac UI, hoặc Mac agent **tự Open
    config dự án đã tồn tại** bằng read-only `config.load`; thao tác Open
    không được create, edit, overwrite hoặc save file.
    Nạp profile không tự Sync/Build/Exec/Start/Stop.
18. Windows Add Application bằng UI, cho phép sửa quyền Start/Stop;
    Mac được dùng Shell/Exec để đóng/mở AutoCAD khi rebuild DLL.
19. Agent được test liên tục trên Active Drawing, New Drawing hoặc Saved
    Test DWG đã biết đường dẫn, mở trực tiếp qua OS/Shell trong folder được cấp.
20. ~~CAI/MCP blacklist AutoCAD `Documents.Open`, `OPEN` và biến thể qua
    COM/SendCommand/LISP; các COM/MCP thao tác CAD khác không bị chặn.~~
    ~~Project CAI có file blacklist gồm `Documents.Open`, `OPEN` và
    biến thể COM/SendCommand/LISP; CAI/MCP dispatcher áp dụng blacklist,
    không chặn các COM/MCP CAD khác.~~
    Mac First-time Setup tạo file blacklist **rỗng** cho mọi project;
    AutoCAD OPEN/COM là ví dụ chỉ thêm nếu user yêu cầu và đã Unlock.
    Không áp mặc định các lệnh này vào CAI/RAI hay ứng dụng Windows khác.
21. Bridge không implement blacklist CAD bằng cách phân tích mọi Shell Command;
    CAI/RAI vẫn sở hữu nghiệp vụ test/drawing.
22. Mỗi dự án có đúng hai file config riêng: **Windows App Data**
    lưu Permission Config (folders/programs/grants), **Mac App Data**
    lưu Command Blacklist Config. Không Sync/copy config qua Bridge.
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
29. Từ Mac UI agent chỉ sửa blacklist khi Unlocked và user yêu cầu
    **qua thao tác Edit Blacklist riêng**, không thông qua Config Open;
    không cho Mac UI cấp/sửa/thu hồi Windows Folder/Program Grants.
30. Sau khi kết nối, Windows user phải tự Open Permission Config đã lưu;
    Mac agent được tự Open **chỉ file blacklist hiện có**, không tạo mới
    hoặc chỉnh sửa file trong lúc Open.
31. Không có chức năng Sync/copy Config Profile giữa Mac và Windows,
    kể cả khi hai bên có quyền File Sync thông thường.
32. Chỉ khi cả hai bên đã Open config **cùng project ID** thì các lệnh
    Read/Sync/Exec/App/MCP theo project mới được dùng; lệch project hoặc
    thiếu config → hiển thị mismatch/not ready, không thực hiện lệnh.
33. Đóng/mở lại Bridge giữ các file config, nhưng không tự mở chúng:
    Windows mở manual; Mac agent có thể tự load đúng file hiện có.
34. First-time Setup project mới trên Mac tạo **file blacklist rỗng**
    (0 entry) với Project ID; không chờ user yêu cầu tạo blacklist.
35. Mac đã load blacklist rỗng hợp lệ + Windows đã manual Open
    Permission Config cùng Project ID + MCP Grant → Mac gọi được
    **tất cả tools** MCP project cung cấp.
36. Blacklist file **thiếu, lỗi đọc, hoặc chưa load** → không
    PROJECT_READY, không được Read/Sync/Exec/App/MCP của project,
    kể cả khi Permission Config Windows đã Open và Grant đầy đủ.
37. Agent chỉ được Open/Load **file đã tồn tại**, tuyệt đối không tạo
    blacklist qua config.load; First-time Setup của Mac app mới tạo
    file rỗng. Không overwrite blacklist có sẵn.
38. User không yêu cầu blacklist thì file **tồn tại nhưng rỗng** qua
    Save/Load/restart; không chặn tools, vẫn cần load trước khi làm việc.
39. Agent chỉ Edit blacklist khi user yêu cầu và Mac user đã Unlock
    thủ công; Lock trở lại giữ nguyên entries.
40. Không áp AutoCAD-specific blacklist mặc định vào phần mềm Windows
    mới hay mọi project khác; Bridge dùng chung cho nhiều repository.

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
- **Save, Save As, List, Load/Open, Rename, Delete, Current**;
  ~~có thể chọn khôi phục profile dùng gần nhất khi khởi động.~~
  Khi kết nối, không auto-activate config cũ: Windows user phải tự Open
  profile đã lưu, Mac user hoặc agent có thể Open profile Mac có sẵn.
  Các thao tác Save/Save As/Rename/Delete là quản lý config bằng UI,
  không phải quyền của agent khi gọi Open.
- Profile lưu lâu dài trong **App Data của chính Bridge** trên từng máy,
  không nằm bắt buộc trong repo và không cần chỉnh JSON thủ công.
  **Không Sync config giữa hai máy**; mỗi máy chỉ lưu file config mà
  mình quản lý (Windows Permission, Mac Blacklist).
- UI trên Windows/Mac phải cho phép chọn lại profile phù hợp. **Windows UI**
  quản lý file Permission Config (Folder/Program Grant/Edit/Revoke);
  **Mac UI** quản lý file Command Blacklist (xem/sửa khi Unlocked,
  Lock/Unlock thủ công).
  ~~Agent trên Mac có thể nhận lệnh từ user để load profile đã lưu, ví dụ
  `config.list()`, `config.current()`, `config.save(name)`,
  `config.load(name_or_id)`, `config.delete(name_or_id)`.~~
  **Windows user tự Open Permission Config bằng UI. Mac agent được
  tự nhận diện dự án rồi Open/Load một Command Blacklist Config đã lưu
  cục bộ trên Mac mà không cần user thao tác thêm.**
  Agent-side Config Open chỉ dùng read-only `config.list()`,
  `config.current()`, `config.load(name_or_id)` (API logic đề xuất).
  Không tạo, ghi đè, rename, save, delete hay chỉnh config trong
  thao tác Open. Việc chỉnh Blacklist theo yêu cầu user khi đã Unlock
  là **workflow Edit riêng** của §19, không bị cấm bởi quy tắc read-only
  dành riêng cho Open. Đây không khóa tên wire protocol.
- ~~Profile chứa các thiết lập Connection, Folder Grants, Workspace/Project,
  Exec/Build, Software Control và thông tin MCP khi áp dụng.~~
  ~~Mỗi dự án có **đúng hai file config** riêng trong App Data của Bridge:~~
  Mỗi dự án có **đúng hai file config, phân tán ở hai máy**:
  (1) **Permission Config** nằm trong **Windows App Data**, chứa folders,
  programs, Actual Granted Permissions và tham chiếu thực thi/MCP nếu cần,
  do **Windows UI** quản lý;
  (2) **Command Blacklist Config** nằm trong **Mac App Data**, chứa danh
  sách lệnh/API bị cấm riêng cho dự án, do **Mac UI** quản lý.
  Không đồng bộ hai file cho nhau. Connection/pairing chung có thể ở
  Bridge global settings, không thêm config thứ ba cho riêng dự án.
  Quy tắc chỉnh sửa/Lock xem §19 và kích hoạt profile xem §20.
- **Windows là chủ thể quyết định Grant**: Mac không thể sửa file profile
  phía Mac để tạo/nâng quyền trên Windows. Load profile Windows đã lưu
  có thể khôi phục Grant do user đã cấp, không phải xin duyệt lại chỉ
  vì chọn lại profile; **chính Windows user phải thực hiện Open thủ công**.
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
(xem §19), không hardcode chung cho tất cả projects.
~~Các mục sau là blacklist CAI đã được yêu cầu:~~
Các mục dưới đây là **ví dụ blacklist riêng cho CAI**, chỉ được thêm
khi user yêu cầu và Mac đã manual Unlock sau khi agent đọc repo.
Chúng không nằm mặc định trong file khi First-time Setup. Blacklist rỗng
thì mọi tool của CAI vẫn hoạt động theo Windows Grant.
~~Agent đọc repo để đề xuất và, theo yêu cầu user, thêm/bớt/sửa entries
chỉ khi Windows user đã Unlock thủ công.~~
Agent sửa danh sách theo yêu cầu user khi Mac UI đã manual Unlock.
Bridge không tự chặn AutoCAD OPEN hoặc bất cứ chức năng ứng dụng khác
chỉ vì ví dụ nêu trong Criteria:

- COM `Application.Documents.Open()`, `AcadDocuments.Open()`,
  `Documents.Open()` và API Open tương đương để chọn một DWG khác.
- AutoCAD in-app commands `OPEN`, `_.OPEN`, `-OPEN` và các biến thể
  cùng ý nghĩa, kể cả gọi qua `SendCommand` hoặc LISP `(command ...)`.
- ~~Không cho phép agent tự chọn working DWG bất kỳ để mở cho test.~~
  Đó là mục đích sử dụng blacklist **khi user chủ động cấu hình** cho
  CAI để tránh mở nhầm working drawing; không phải lệnh cấm mặc định
  của Bridge cho tất cả ứng dụng.
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
  **chỉ** kiểm tra entry thực tế có trong file của project đã load.
  CAI/MCP chịu trách nhiệm command nằm trong payload nếu entry
  tương ứng đã được user cấu hình. Không bắt Bridge hiểu mọi chương
  trình hoặc parse tùy ý Shell Command. File rỗng thì không cấm
  bất kỳ lệnh nào; file thiếu/chưa load thì không thực thi project.
  Blacklist là workflow tùy chọn, không phải sandbox chống Exec tùy ý.

## 18. End-to-end Development / Testing

1. Mac và Windows chạy Bridge; pair/reconnect theo §4.
2. Windows user cấp Grant hoặc **tự Open** Permission Config đã lưu bằng
   Windows UI. Mac agent có thể tự Open/Load **Command Blacklist Config
   đã tồn tại trên Mac** theo project, read-only và không tạo/chỉnh file.
3. Hai bên phải Open config có project ID khớp nhau; nếu chưa khớp
   thì chưa thực hiện project operations. Mac agent có thể request thêm
   quyền; Windows được cấp khác với request.
4. Mac theo Actual Grant: Read logs/source → Exec build/script → Sync small
   source/DLL → Shell Stop/Start ứng dụng → gọi CAI/RAI MCP để test.
5. ~~Khi test AutoCAD chỉ dùng Active Drawing / New Drawing / OS-shell direct
   open Saved Test Drawing theo §17; AutoCAD OPEN/COM Open bị blacklist
   theo Command Blacklist Config của project CAI đang nạp.~~
   Với CAI, Active/New Drawing hoặc OS/Shell direct-open test DWG là
   workflow test được khuyến nghị; OPEN/COM Open chỉ bị cấm nếu user
   thực sự thêm vào blacklist CAI. Với các ứng dụng Windows khác,
   không áp quy định AutoCAD/drawing lên các tool của chúng.
6. Lặp lại build/test trong phiên đã Grant, không thêm human gate.
7. Thoát một trong hai Bridge app → kết thúc phiên, giữ nguyên các
   Config Profiles đã lưu để sử dụng trong lần sau.

Không phát triển CI/CD, background runner, scheduler, general remote
desktop, OS sandbox hay control-plane phức tạp ngoài mục tiêu Bridge.

## 19. Exactly Two Per-Project Config Files and Manual Blacklist Lock

Mỗi dự án (CAI, RAI, AssetGen, ...) thuộc một Named Config Profile.
~~**đúng hai file config riêng** trong App Data của Bridge.~~
**Có đúng hai file config của dự án trên hai máy khác nhau**. Mỗi file
lưu trong **App Data cục bộ của máy sở hữu**, không tự sync hay copy:

```text
Windows App Data/TestBridge/profiles/<project-id>/
  permissions.json          # Chỉ Windows giữ và user mở bằng Windows UI

Mac App Data/TestBridge/profiles/<project-id>/
  command-blacklist.json    # Chỉ Mac giữ, agent có thể Open read-only
```

Tên file và bố cục là ví dụ, không cố định schema. Hai file có nội dung
khác nhau và không cần giống nhau về bytes; **phải cùng Project ID**.

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

- Chứa danh sách command/API blacklist riêng của dự án. **Mac app
  tự tạo file blacklist rỗng (0 entry) khi First-time Setup một
  project mới**; không tự thêm các command bị cấm vì ví dụ CAI/RAI.
  Không tự tạo lại/overwrite file blacklist đang có.
- Agent **đọc/khảo sát repo** trước khi đề xuất cấu hình và chỉ chỉnh
  (thêm/bớt/sửa) theo yêu cầu của user. Nếu user không yêu cầu,
  file rỗng được giữ nguyên và Mac có toàn quyền tools của project
  theo MCP Grant Windows, sau khi load thành công.
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

- ~~Save/Load profile sử dụng đúng cặp file của dự án;~~
  Mỗi máy Open **file config cục bộ** tương ứng cùng Project ID;
  không sync/copy config giữa hai máy. Không đưa blacklist CAI sang RAI,
  không overwrite blacklist đang Locked.
  **Windows UI chỉ quản lý Permissions; Mac UI chỉ quản lý Blacklist.**
  Không đồng bộ Lock state giữa hai máy: Lock là trạng thái cục bộ của
  blacklist trên Mac, được giữ lại qua restart.
- Bridge nạp blacklist của dự án; dispatcher đủ thông tin để nhận
  diện command/API có trách nhiệm kiểm tra nó. Với AutoCAD in-app
  OPEN và COM Open trong MCP payload, CAI/MCP dispatcher kiểm tra.
  Bridge không parse mọi Shell Command hay xây thêm security sandbox.
- Connection/pairing global settings của Bridge có thể lưu riêng;
  yêu cầu **hai file** áp dụng cho config **mỗi dự án**, không cấm
  Bridge có các file dữ liệu toàn cục phục vụ hoạt động.

## 20. Local Config Open and Project Match Before Use

- **Hai config được lưu độc lập**: Windows App Data giữ Permission Config,
  Mac App Data giữ Command Blacklist Config. Bridge **không Sync, replicate,
  import từ máy kia hoặc tự tạo file đối ứng**, kể cả khi File Sync đang Grant.
- Sau khi kết nối/pairing xong, **Windows user phải tự chọn Open
  Permission Config đã lưu trên Windows UI**. Không tự Open profile lần
  trước, không để agent thao tác Open trên Windows thay user.
- Trên Mac, user có thể tự Open hoặc **agent có thể tự xác định project và
  Open một Command Blacklist Config đã lưu sẵn** trên Mac. Thao tác
  Open/Load của agent chỉ **read-only, existing-file-only**: không
  create, edit, append, rename, delete, overwrite, save hoặc Sync config.
  ~~Không thấy config thì báo rõ `CONFIG_NOT_FOUND` và chờ user tạo/lưu
  bằng UI ở máy tương ứng; agent không tự tạo placeholder.~~
  **Ngoại lệ duy nhất là lúc First-time Setup project mới:** Mac app
  khởi tạo blacklist file rỗng. Agent config.load vẫn chỉ đọc file
  đã tồn tại; nếu file thiếu sau setup, báo `CONFIG_NOT_FOUND`,
  không tạo lại ngầm, **không PROJECT_READY và không thực thi tools**.
- Quy tắc agent không sửa config **trong thao tác Open** khác với workflow
  **Edit Command Blacklist** ở §19: Agent chỉ được Edit khi người dùng yêu
  cầu riêng và đã Unlock thủ công trên Mac UI. Việc Auto-Open không cần
  Unlock và không được tự Unlock.
- Hai máy trao đổi **metadata lựa chọn project** qua kết nối, tối thiểu
  Project ID ổn định, **không trao đổi nội dung config**. Không so sánh
  file bytes/hash vì Permission Config và Blacklist có chức năng khác nhau.
- Chỉ khi **Windows đã manual Open Permission Config + Mac đã Open
  Command Blacklist Config + Project ID hai bên khớp**, phiên mới ở
  trạng thái `PROJECT_READY`. Không khớp hoặc một bên chưa Open thì
  hiển thị trạng thái `CONFIG_MISMATCH` / `CONFIG_NOT_OPEN` và
  không chạy Read/Sync/Exec/App/MCP cho project; không phải approval
  hoặc security gate bổ sung, chỉ tránh vận hành nhầm cấu hình dự án.
- Auto-reconnect chỉ khôi phục network connection; project phải trải qua
  quy trình Open config trên hai máy cho phiên mới. Config vẫn lưu qua
  restart. Load không tự kích hoạt Sync/Build/Test/Start/Stop.
- Windows tiếp tục Grant/Edit/Revoke folder/program qua Windows UI theo
  §14; Mac tiếp tục quản lý blacklist/Lock/Unlock theo §19.

## 21. Generic Bridge First-time Setup and Mandatory Blacklist Load

Bridge là công cụ **dùng chung cho mọi repository và loại phần mềm Windows**.
CAI/RAI chỉ là ví dụ. Không gắn các quy tắc AutoCAD hay tool blacklist
cố định vào hệ thống Bridge.

**First-time Setup (Mac app):**

- Khi user thiết lập **project mới lần đầu**, ứng dụng Bridge trên Mac
  tự tạo file Command Blacklist Config **rỗng với Project ID** trong
  Mac App Data, ví dụ `blacklist: []`. Đây là công việc khởi tạo
  dự án, không phải quyền create của agent qua `config.load`.
- Không tự khảo sát repo để tạo blacklist, không tự thêm các entry
  AutoCAD OPEN hoặc tool nào, không yêu cầu user cấu hình blacklist.
- Nếu file đã tồn tại, không reset/ghi đè/xóa entries dù Setup/Open
  được gọi lại.

**Quy tắc Open và readiness cho mọi phiên:**

- Windows user phải tự **Open Permission Config đã lưu** qua Windows UI.
  Mac agent có thể tự **Open/Load Command Blacklist Config đã tồn tại**
  trên Mac, thao tác read-only, không Create/Edit/Save.
- Hai máy chỉ trao đổi **Project ID** để so khớp, không sync nội dung
  hai file config. Không yêu cầu hai file có nội dung giống nhau.
- Chỉ `PROJECT_READY` khi **cả hai file thực sự tồn tại, đã load
  thành công, và Project ID khớp**. Không bỏ qua việc load blacklist
  dù file dự kiến rỗng. Nếu Mac blacklist **thiếu**, **chưa load**
  hoặc **lỗi đọc**, dự án không được vận hành; trả
  `CONFIG_NOT_FOUND` / `CONFIG_NOT_OPEN` hoặc lỗi load phù hợp.
- Phân biệt bắt buộc:
  - **File tồn tại + load thành công + danh sách rỗng:** được hoạt
    động; không chặn tool nào do blacklist.
  - **Không có file, chưa load hoặc load lỗi:** không được hoạt
    động, tránh bỏ qua blacklist người dùng đã cấu hình.

**Phạm vi tools và sửa blacklist:**

- Windows MCP Access Grant và `PROJECT_READY` thì **mặc định Mac
  được dùng tất cả tools MCP mà repo/phần mềm dự án cung cấp**.
  Chỉ những entry thật sự có trong blacklist đã load mới bị từ chối.
- User có thể không bao giờ yêu cầu blacklist: vẫn tạo và lưu file
  rỗng từ Setup để đáp ứng quy tắc load, không hạn chế tính năng.
- Nếu user yêu cầu xây blacklist, agent đọc repo và đề xuất danh sách
  project-specific. Agent chỉ thêm/bớt/sửa khi Mac user đã **Unlock
  thủ công** trên Mac UI, rồi user có thể Lock lại.
- Blacklist là workflow tùy chọn theo dự án; không thêm gate phê
  duyệt từng thao tác, không chặn cứng chức năng phần mềm đang dev.

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
  theo yêu cầu user khi đã Unlocked. ~~Chỉ thay UI authority, không
  tự đặt thêm gate hay thay nơi lưu dữ liệu chỉ vì đổi UI.~~
- v1.2b HUMAN CLARIFICATION: 2026-11-03 — Hai config dự án là file
  **cục bộ riêng** (Windows Permission, Mac Blacklist), không sync.
  Windows user **manual Open** file đã lưu; Mac agent **auto Open
  existing blacklist read-only** theo dự án (không create/edit/save).
  Project operations chỉ chạy khi hai máy đã Open cùng Project ID.
  Agent Edit Blacklist khi user yêu cầu và Mac manual Unlock là luồng
  riêng, không đồng nhất với Open.
- v1.2c HUMAN CLARIFICATION: 2026-11-03 — Bridge là generic bridge cho
  mọi repo/phần mềm Windows. Mac app tạo **file blacklist rỗng** trong
  First-time Setup; agent chỉ Open file tồn tại và không tạo/sửa khi Open.
  Blacklist rỗng + load thành công = không chặn tools; blacklist thiếu
  hoặc chưa load = không PROJECT_READY, cấm dùng tools để tránh bypass.
  Chỉ user yêu cầu và Mac manual Unlock mới được thêm/bớt blacklist.
  AutoCAD OPEN/COM chỉ là ví dụ, không hardcode làm blacklist mặc định.


## 22. Bridge source maintenance from Mac — human-approved exception

- Khi hai Bridge đã kết nối, Mac mặc định được Read và Sync vào đúng thư mục source LAN Bridge trên Windows đã xác định, kể cả khi chưa mở config project. Quyền này hiển thị rõ và Windows user có thể thu hồi. Đây là ngoại lệ riêng cho source Bridge đối với yêu cầu PROJECT_READY; các thao tác project khác vẫn giữ nguyên điều kiện hiện có.
- Không cho Sync thư mục config của Bridge; quyền source không mặc nhiên cho đọc credential, .env, token, pairing hay config riêng tư. Không tự thay binary Bridge đang chạy; tránh ghi đè thay đổi của user.
- Quyền Read/Sync mặc định không tự cấp Exec hoặc quyền project khác. Build/test dùng quyền thực thi được Windows cấp. Không yêu cầu Codex hoặc Claude Code trên Windows.
- MCP forwarding là generic theo project, hỗ trợ MCP server stdio và Streamable HTTP phù hợp; CadGPT chỉ là trường hợp kiểm thử. Giữ admission/session và các quy tắc của server; không hardcode CAI/AutoCAD hoặc bỏ qua blacklist. Cấu hình MCP tách biệt với MCP grant, bảo vệ secret và giữ profile/grant khi chỉnh sửa.
- Xác minh với MCP generic và CadGPT, test quyền, lỗi, kết nối lại, tính riêng biệt của session và không lộ secret. Test hai máy thực tế báo riêng với CI. Cài lại giữ config, grants, pairing và blacklist.

Human đã xác nhận sau tư vấn kỹ thuật ngày 2026-11-03.


## 23. Keep connected Bridge awake — human-approved

- Khi Bridge đang chạy trên Mac hoặc Windows, ngăn máy tự sleep vì nhàn rỗi; màn hình vẫn có thể tắt hoặc khóa. Khi Exit Bridge, trả lại hoạt động tiết kiệm điện bình thường, không đổi cài đặt OS lâu dài.
- Project đã kết nối không tự timeout chỉ vì nhàn rỗi. Tiếp tục phát hiện ngắt mạng thực, kết nối lại và giữ quy tắc mở config hiện hành; timeout của từng lệnh độc lập với timeout phiên. Không hứa ngăn sleep do user chủ động, đóng nắp, pin cạn hoặc chính sách hệ điều hành.

Human xác nhận sau tư vấn kỹ thuật ngày 2026-11-03.
