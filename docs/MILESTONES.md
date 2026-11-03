# Milestones — LAN Test Bridge v1.3 (legacy M1–5 + Criteria-aligned M6–10)

## Amendment milestones 11–14 — PLANNED, not implemented

### Milestone 11: Generic project MCP

**GOAL:** GOAL-19; Criteria §§8, 20–22. Windows UI edits and tests a project's stdio/Streamable HTTP connection independently of MCP grant, preserving the existing profile and grants. Mac discovers and calls permitted tools, receives list changes, and gets bounded, redacted errors. Server sessions do not cross distinct agent conversations.

**Verification:** generic stdio and HTTP fixture covering initialize, session IDs, notifications, refresh, timeout, malformed/failed server, reconnect and redaction; isolate two clients; exercise absent/mismatched config and blacklist/grants; unchanged stdio regression. CadGPT outer MCP and its drawing/admission behavior require real two-machine evidence, with no live-drawing mutation. Exit: tests and review pass; external server identity limitations recorded explicitly.

### Milestone 12: Maintain Bridge source from Mac

**GOAL:** GOAL-20; Criteria §22. Mac is the source of changes and its agent can discover the permitted Windows source and relevant non-secret configuration facts. Windows user can see/revoke default paired-device Read+Sync access even without project configs. Config/private paths and running binaries are excluded. Routine sync needs no user notification or confirmation; Exec remains separately granted.

**Verification:** discover permitted source and non-secret setup facts, independent access with configs missing, paired-only behavior, root and path traversal/junction boundaries, private paths, revoked rights, 5 MB and atomic write, locked-file errors, no routine change notification, preservation of existing project grants and lack of Exec; run build/test using explicitly granted Exec without a Windows coding agent. Native two-machine and Windows file-sharing tests are separate. Exit: tests and review pass and OS-dependent limitations are recorded.

### Milestone 13: Awake while Bridge runs

**GOAL:** GOAL-21; Criteria §23. Both apps inhibit automatic idle system sleep while running and release inhibition on Exit; screen may sleep. Active project remains available during idle; real disconnection uses current reconnect/config-open rules.

**Verification:** lifecycle test for acquire/release and failure reporting; idle and network disconnect tests; physical Windows and Mac sleep/release checks reported separately. Explicit sleep, closing lid and depleted battery are not guaranteed. Exit: tests and review pass with native evidence or UNVERIFIED.

### Milestone 14: Integrated validation and matching packages

**GOAL:** GOAL-22; Criteria §§11, 22–23. Validate generic MCP and CadGPT outer MCP, maintenance, idle behavior and install/reinstall on the same candidate SHA. Windows/macOS CI produces matching packages; preserve pairing, grants, configs and blacklist.

**Verification:** GitHub Windows/macOS tests and artifacts for the candidate SHA; two-machine test with named test drawing only, generic MCP and no Windows agent; reinstall data preservation and safe update while Bridge is exited; secret scan of errors/logs. Report PASS/FAIL/BLOCKED/UNVERIFIED separately. Release only once applicable checks pass; never equate CI with physical acceptance.

Historical status of legacy M1–10 follows; it does not override amendment milestones 11–14 or assert their completion. Real-host acceptance remains pending (2026-11-03).

Legacy technical detail: docs/TECHNICAL_PROPOSAL.md. Revised approved-scope planning: docs/IMPLEMENTATION_PLAN.md (reviewed). New proposal: docs/TECHNICAL_PROPOSAL_V2.md (next step).

**Historical baseline:** M1–5 below preserve the old execution record, NOT currently binding requirements where they conflict with latest Criteria. M6–10 below own the required delta. Do not claim new functions are CODE COMPLETE.
Local Go tests, `go vet ./...`, and the Windows GUI build pass. **- test required:** native macOS packaging; native tray icon rendering/contrast on macOS and Windows (including Windows 100%/150% scaling); CAI MCP transport/tool behavior; and the 12-item two-machine acceptance remain deferred until after push; see `TASKS.md`.

## Milestone 1: Nền tảng — kết nối, ghép nối, UI tối thiểu
**GOALs:** GOAL-1, GOAL-2, GOAL-10 (phần khung)

**Deliverables:**
- Go module, Makefile build trên Mac cho cả 2 nền tảng (Windows: CGO tắt, 1 file exe).
- mDNS discovery + nhập IP thủ công.
- Ghép nối mã 6 số bằng PAKE chuẩn (CPace) + xác nhận khóa 2 chiều → pin fingerprint → mTLS; Unpair.
- Spike tìm máy trên 2 máy thật trước; `.app` macOS khai báo quyền Mạng cục bộ.
- Thông điệp `bye` khi Mac Thoát (dùng để hủy lệnh từ M3).
- Heartbeat, tự kết nối lại, Thoát dừng hẳn process.
- Khung UI: icon tray/menu bar (Lucide, flat) đổi màu theo trạng thái + trang HTML localhost, hiện IP:port của host.

**Verification (2 máy thật, chạy từ `.app` và `bridge.exe`):**
- Tìm thấy nhau, ghép nối đúng mã; mã sai bị từ chối; sai 3 lần → mã hủy.
- Tắt/mở lại cả 2 → tự kết nối; mất Wi-Fi rồi có lại → tự nối lại.
- Icon đổi màu đúng khi: kết nối, mất kết nối, chờ ghép nối; nhìn rõ trên thanh menu macOS sáng/tối và taskbar Windows sáng/tối, Windows nét ở scale 100% và 150%.
- Thoát → process không còn.

---

## Milestone 2: Quyền, Read, Sync, giao diện agent
**GOALs:** GOAL-3, GOAL-4, GOAL-5, GOAL-12

**Deliverables:**
- Yêu cầu quyền → Accept/Reject trên Windows; sửa/thu hồi; ~~Sync chỉ vào repo hoặc thư mục liên quan đã khai báo (`related_dirs`)~~; replaced by actual Windows Sync Grant under M7; Execute/MCP theo project.
- Một hàm kiểm tra đường dẫn dùng chung (chặn `..`, symlink/junction ra ngoài).
- Read: list, metadata, đọc theo đoạn, đọc N dòng cuối.
- Sync nhiều file/lần ≤ 5 MB, ghi tạm → rename, trả SHA-256.
- `bridge mcp`: MCP server stdio cho Claude Code hoặc Codex; Settings tự quản lý đúng một đăng ký user-scope, đọc hai cấu hình agent và giữ nguyên MCP server không liên quan.
- Khóa liên-process giữ độc quyền trong suốt vòng đời `bridge mcp`; instance thứ hai và đổi agent khi đang có phiên phải báo bận rõ.

**Verification:**
- Agent xin quyền → Accept → đọc log thành công.
- Đọc ngoài phạm vi, `..\`, junction ra ngoài → từ chối. ~~Sync ngoài project → từ chối.~~ Only ungranted folder must be denied (M7).
- > 5 MB → từ chối, không file nào bị ghi. Thu hồi → lần gọi sau bị từ chối.
- Chuyển Claude ↔ Codex chỉ để lại đăng ký Bridge của agent được chọn; các MCP entry khác được giữ nguyên; lỗi CLI/config được báo rõ.
- Hai tiến trình `bridge mcp` cùng lúc: tiến trình đầu giữ khóa, tiến trình sau nhận lỗi bận; khóa được giải phóng sau khi tiến trình đầu thoát.
- **- test required:** xác nhận Codex CLI/Claude Code thực trên macOS nhận cấu hình, khởi chạy và ngắt MCP như dự kiến.

---

## Milestone 3: Lệnh build & MCP dự án
**GOALs:** GOAL-6, GOAL-7

**Deliverables:**
- ~~8 lệnh cố định ánh xạ theo từng project trong `config.json`, chạy từ repo root.~~ Keep named commands as shortcuts; extend granted arbitrary Shell/Exec (M7).
- Job Object dọn process con, không bật cửa sổ console, không treo vì tiến trình phụ .NET; timeout; lệnh dài lấy kết quả tiếp bằng `command_result`.
- Chuyển tiếp tools của MCP server dự án (transport thật của CAI, kiểm tra trước khi code); tự mở lại phiên khi server khởi động lại.

**Verification:**
- `build` của CAI chạy thật, trả exit code + output, không bật cửa sổ console; ID lạ → từ chối.
- Icon chuyển xanh dương khi build đang chạy.
- Lệnh dài lấy tiếp bằng `command_result`, kể cả sau khi Wi-Fi rớt rồi nối lại.
- Thoát Windows hoặc Thoát Mac giữa lúc build → không còn process build trên Windows.
- Gọi 1 tool thật của CAI MCP từ Claude Code trên Mac; tắt/mở lại CAI MCP server → gọi lại được không cần khởi động lại Bridge.

---

## Milestone 4: Start/Stop, Chat & hoàn thiện UI
**GOALs:** GOAL-8, GOAL-9, GOAL-10

**Deliverables:**
- Start/Stop app được phép (WM_CLOSE, không force-kill, không gắn vào Job Object).
- Chat hai chiều là trang chính trên cả host và client; composer và lịch sử chat vẫn giữ nguyên hành vi.
- Kết nối `bridge mcp` vào chat Bridge hai chiều hiện có; khi không chạy được lệnh trên Windows hoặc cần thao tác người dùng, agent gửi hướng dẫn cùng script nhiều dòng qua cùng kênh text. Tin nhắn gắn nhãn `Agent (Mac)`, hiện trong chat Bridge trên cả Mac lẫn Windows và có thể copy ở Windows. Không tạo chat subsystem hay định dạng script riêng; Bridge không chạy text từ chat.
- Tất cả trạng thái/cấu hình kết nối, pairing, yêu cầu quyền/thông báo, grants, project/app, lỗi cấu hình và MCP registration/warnings nằm trong Settings; badge Settings báo số yêu cầu/thông báo đang chờ.
- Client hiển thị tên gọn của thao tác MCP Bridge gần nhất/đang chạy: `run_command` hiển thị `<project> · Chạy lệnh` (hoặc `Chạy lệnh` nếu thiếu project), không phản chiếu command arguments, output, log hay nội dung file; không suy đoán task title nội bộ của agent.
- Dùng trang HTML nhúng và JS/CSS hiện có; không thêm UI framework, desktop notification hay theo dõi ngoài MCP calls đi qua Bridge.

**Verification:**
- App được phép Start/Stop OK; app không được phép → từ chối; Thoát Bridge không tắt AutoCAD.
- Chat gửi/nhận 2 chiều. Mọi thao tác §11 làm được trên UI; người dùng tìm thấy trạng thái/kết nối và thông báo trong Settings.
- Activity của `run_command` với project `CAI` và command argument đặc trưng phải là `CAI · Chạy lệnh`, không chứa command argument; thiếu project phải dùng `Chạy lệnh`. Project MCP tool tiếp tục hiển thị `<project> · <tool>`.
- Agent gửi text/script nhiều dòng qua MCP → tin nhắn có nhãn `Agent (Mac)` xuất hiện trên Mac và Windows; code/line breaks giữ nguyên; Windows copy được nguyên văn; text rỗng hoặc quá giới hạn bị báo lỗi rõ. Thành công chỉ sau xác nhận từ host; timeout sau gửi báo "chưa rõ đã giao tới host hay chưa", không tự retry. Không có thao tác nào tự chạy nội dung chat.
- MCP call với project/command hiển thị nhãn đúng trong activity; call thành công/thất bại cập nhật trạng thái; idle hiển thị rõ không có thao tác Bridge quan sát được. Không lộ args/nội dung tool trong nhãn.
- **- test required:** ghép nối và gửi/nhận chat giữa Mac/Windows thật; kiểm tra MCP `chat_send` từ agent Claude Code/CAI, nhãn sender và activity trên Mac; xác nhận Windows copy/paste nguyên văn script. Các lần chạy/build/test mô phỏng không thay thế xác minh này.

---

## Milestone 5: Cài đặt & nghiệm thu
**GOALs:** GOAL-11

**Deliverables:**
- `install.sh` (Mac) và `install.ps1` (Windows, không admin, không cài thêm gì; cảnh báo nếu Wi-Fi đang là Public).
- ~~Nghiệm thu 12 mục §11 trên 2 máy thật với repo CAI.~~ Full 40-case Criteria acceptance including a non-CAD Windows project and CAI sample belongs to M10.

**Verification:**
- Cài → icon → mở app → pair → đọc log → sync → build → gọi MCP → start/stop → chat → Thoát.
- Idle mỗi bên < 30 MB RAM, CPU ~0%.
- ~~12 mục §11 PASS~~ (old scope); M10 must cover all 40 Criteria §11 cases and record PASS/FAIL/UNVERIFIED in docs/reports/.


---

## Milestone 6: Independent project configs and matching — PLANNED

### GOAL

Two locally stored, unsynced project config files: Windows permissions, Mac blacklist. Both must load successfully with matching stable Project ID before project operations. Criteria §§4, 15, 19–21; Plan Phase 1.

**Depends on:** legacy transport and Mac MCP scaffolding; inspect actual existing code first, do not rebuild unchanged modules.

**Deliverables:** Windows UI creates/saves and manually opens its Permission Config; Windows setup shows the stable Project ID. Mac first-time app setup selects the same Project ID (via permitted metadata or manual entry) and creates empty blacklist file. Mac agent may load existing file read-only but never create/edit during Open. Maintain global pairing state independently. Profiles persist without auto-activation. Explicit NOT_OPEN/NOT_FOUND/INVALID/MISMATCH states.

**Verification:** Empty loaded Mac file + matching Windows manual-open file => PROJECT_READY; missing/unloaded/corrupt file or wrong Project ID => no project Read/Sync/Exec/App/MCP; restarts and reconnects require re-opening; neither config syncs to the other host; existing files not overwritten on setup.

## Milestone 7: Windows grant authority, sync scope and Shell Exec — PLANNED

### GOAL

Windows controls actual grants for any folder/app independently of Mac request, while granted Shell/Exec supports attended development workflows. Criteria §§5–7, 9, 14, 16; Plan Phase 2.

**Depends on:** M6 readiness, existing fileops/permissions/executor.

**Deliverables:** Windows UI Pending Requests/Actual Grants, Add Folder OS chooser, Program EXE picker, Grant/Edit/Revoke, named profile Save/Load, manual Open. Windows may grant more/less/different than requested, or grant proactively. Read/Sync checks actual folder grants, not project root/related_dirs; per-request Sync limit 5 MB; never transfer config files. Shell command/script with approved Exec, selectable working directory, stdout/stderr/exit/timeout/cancel; eight named build IDs remain shortcuts. Start/Stop existing Windows app workflow; no fake additional shell sandbox.

**Verification:** Request Read→Grant Read+Sync; Request Read+Sync→Grant Read; proactive folder grant; revoke applies immediately; non-project but granted Sync succeeds; ungranted path and oversize denied; shell rebuild/restart and command output work.

## Milestone 8: Mac blacklist lock and generic MCP dispatch — PLANNED

### GOAL

All project MCP tools remain available by default when valid empty blacklist is loaded; explicit user-configured rules are applied on Mac; missing blacklist is never bypassed. Criteria §§8, 17, 19–21; Plan Phase 3.

**Depends on:** M6 matching and M7 actual-grant integration.

**Deliverables:** Blacklist schema, atomic save, first setup creates 0 entries and never overwrites existing; Mac UI displays list and manual Lock/Unlock. Agent may edit only on separate user request when unlocked; opening config remains read-only. Mac bridge MCP dispatch checks concrete configured tool/argument rules before forwarding; generic across any Windows repo. If a user-requested nested matcher cannot reliably be enforced at Mac boundary, reject enabling that rule with clear unsupported error and retain prior valid config; offer whole-tool deny where appropriate. No default CAI/RAI hardcoded commands.

**Verification:** Loaded empty list + MCP grant => all non-CAD project tools callable; explicitly configured rule => blocked with clear error; missing/unloaded/corrupt => not READY; lock prevents edits through UI/API; manual Unlock + user request enables edits; persisted lock survives restart; no config copied to Windows.

## Milestone 9: Existing UI and project-state visibility — PLANNED

### GOAL

Deliver the machine-specific Windows and Mac controls within existing lightweight chat/Settings UI and expose readiness without framework replacement. Criteria §§10–11, 14–15, 19–20; Plan Phase 4.

**Depends on:** M6–M8 APIs.

**Deliverables:** Windows Settings for permission profile manual Open, folder/app Grant/Edit/Revoke and pending requests. Mac Settings for project identity, blacklist list, errors and manual Lock/Unlock; agent Open existing file only. Separate network Connected from PROJECT_READY. Retain two-way chat, settings, activity labels without args/logs, provider management, one-agent lock, icons.

**Verification:** native Windows file/folder picker and edit; Mac lock/edit/read-only Open; project mismatch/absent file/error messages; profile switching; connection/permissions activity and chat regression.

## Milestone 10: Native two-machine acceptance — PLANNED

### GOAL

Verify old and new requirements on real Mac+Windows and a generic non-CAD project, with an optional CAI example, without claiming simulation is native PASS. Criteria §11 cases #1–40; Plan Phase 5.

**Depends on:** M6–M9 implemented with source-bound tests.

**Deliverables:** Go unit/integration and regression evidence for config migration, permissions, Scope Sync, Shell/Exec, blacklist, project match, MCP dynamic tools, UI and lifecycle. Native Mac and Windows UI tests. Real attended LAN end-to-end runs. Docs/reports evidence by revision, scenario, host and PASS/FAIL/UNVERIFIED. Update README/install notes according to actual behavior.

**Verification:** all 40 Acceptance Criteria explicitly mapped to evidence; unavailable native conditions remain UNVERIFIED, not PASS. No product code implementation or merge is approved merely because this plan document exists.

## New-scope dependency and status ledger

| Group | Depends on | State |
|---|---|---|
| Legacy M1–5 | earlier v1.0 work | Source previously labeled code complete; native acceptance pending |
| M6 | existing transport/MCP | PLANNED |
| M7 | M6 + permissions/fileops/executor | PLANNED |
| M8 | M6/M7 + MCP dispatch | PLANNED |
| M9 | M6–M8 APIs | PLANNED |
| M10 | M6–M9 | PLANNED / native tests NOT RUN |

Reviewed Plan blob: f821efded9fc5d6f7e7c0589908c63a4ca2bdbfc.
Direct-review evidence: docs/reports/2026-11-03-plan-v1.3-direct-review.md.
No new GSA Project/Job, no product source changes in this milestone-document update.
