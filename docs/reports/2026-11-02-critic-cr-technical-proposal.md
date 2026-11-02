# Goal-scoped review — Whole-Plan Technical Proposal (rev 2), LAN Test Bridge v1.0

- Target: PRE-CODE
- Approved goal and version: CRITERIA v1.0 / v1.0a (APPROVED 2026-11-02); IMPLEMENTATION_PLAN GOAL-1..GOAL-12; MILESTONES rev 2
- Inputs: docs/CRITERIA.md, docs/IMPLEMENTATION_PLAN.md, docs/MILESTONES.md, docs/TECHNICAL_PROPOSAL.md (rev 2, all Tasks 1.1–5.2), docs/ARCHITECTURE.md, docs/PROJECT_PROFILE.md. No accepted Decisions exist (docs/discussions/ has only README.md). No source code exists. Human context relayed by caller (not Criteria): main consumer is the CAI repo (AutoCAD add-in), later RAI/other Windows repos; install as little as possible on Windows; Human is non-technical.
- PRE-CODE independent Critic mode: PRE_CODE_TECHNICAL (final CR, no extra Reviewer/Human layout signoff)
- PRE-CODE Critic verdict: **CHANGES_REQUIRED**
- Review execution evidence: Critic run as a Claude Code subagent invoked by the main chat agent; GUIDED_ONLY (no native signed dispatch receipt).
- CODE/TEST outcome: UNVERIFIED (no code)
- Cross-Task duplicate gates/interfaces/state and missing callers: see F-10 (agent-facing MCP server placed in `mcpfwd/`, M3 module, but built in M2; JSON-RPC/MCP types needed by both sides), F-11 (Task 1.6 has no consumer in M1), F-14 (`store` described as writing user-owned `config.json`). Path gate has a single owner (`permissions.Resolve`) — good, no duplicate gate found.
- Proportionality (Small Function → Big Solution → Larger System Impact): Overall direction (one Go binary, three roles, embedded localhost HTML, JSON lines over mTLS, config.json + state.json) is proportionate and is the smallest credible design for the approved scope. No component reaches "Big Solution". Minor trims suggested (F-13 log rotation). PAKE is proportionate: a 6-digit code without a PAKE is offline-brute-forceable, and Criteria §4 forbids home-made crypto.
- Derived approved components (only after PRE-CODE PASS): none yet.
- Host/native-test limits: Library facts below were checked against upstream sources on 2026-11-02 (see "Verified facts"). Windows/macOS OS behaviors marked UNVERIFIED must be proven on the two real machines in M1/M3.

## Verified facts (2026-11-02)

| Item | Finding | Source |
|---|---|---|
| `fyne.io/systray` Windows | `systray_windows.go` imports only stdlib + `golang.org/x/sys/windows`, no `import "C"` → builds with `CGO_ENABLED=0`. README's generic "requires cgo" applies to macOS/other. | raw source, master |
| `fyne.io/systray` macOS | `systray_darwin.go` uses cgo + Cocoa → must be built natively on the Mac (matches proposal). Needs an .app bundle with `LSUIElement` (matches Task 5.1). Repo active (last commit 2026-10). | raw source, README |
| `github.com/grandcat/zeroconf` | Last commit 2023-01-19, no tagged releases, 35 open issues / 18 open PRs → effectively unmaintained. Works in practice, but Windows behaviour not documented upstream. | GitHub API |
| `filippo.io/cpace` | Only version `v0.0.0-20210101143347`, repo untouched since 2021. README: "experimental implementation", "weekend project", "loosely based on draft-haase-cpace-01" (not the current IRTF draft). **No key confirmation**: mismatched passwords yield different keys and "an error is NOT returned". Deps: `gtank/ristretto255`, `x/crypto`. | pkg.go.dev, module proxy, GitHub API |
| Alternative CPace | `rsc.io/cmd/mote/internal/cpace` (Russ Cox, BSD-3): draft-irtf-cfrg-cpace-21, CPACE-X25519-SHA512, includes `Tag`/`Verify` key confirmation, recent (2026). It is an `internal` package → cannot be imported, only vendored (copied with license). Imports not confirmed stdlib-only. | pkg.go.dev |
| Claude Code `notifications/tools/list_changed`, MCP tool-call timeout | UNVERIFIED in this review. | — |
| macOS 15+ Local Network privacy for a non-Apple .app doing mDNS/LAN TCP | UNVERIFIED on the target Mac; behaviour per Apple's local-network-privacy rules is that an app bundle needs `NSLocalNetworkUsageDescription` (+ `NSBonjourServices` for browsing) and a user Allow. | Apple platform knowledge, not re-fetched |
| Windows 11 default network profile | New Wi-Fi networks default to **Public**; firewall "Allow" on Private only does not open the port on a Public network. | Windows platform knowledge, not re-fetched |

## Findings

| Finding | Blocking? | Goal / condition | Reproducible evidence | Required action |
|---|---|---|---|---|
| F-01 Sync cannot reach "directly related" folders | YES | GOAL-3/GOAL-5, CRITERIA §5 Sync ("thư mục repo dự án **và thư mục liên quan trực tiếp**") | Proposal Task 2.1: "`sync` chỉ cấp cho thư mục nằm trong `root` của một project". Milestones M2: "Sync … chỉ trong project đã khai báo". For an AutoCAD add-in the deployed DLL folder (e.g. a `.bundle` under ApplicationPlugins) is normally outside the repo. Missing approved capability. | Add optional `"related_dirs": [...]` to each project entry in `config.json`; `sync` may be granted on paths under `root` or any `related_dirs` entry. Same `permissions.Resolve`. Update Plan GOAL-3/Milestone M2 wording. |
| F-02 CPace library precondition already fails; key confirmation and binding source unspecified | YES | GOAL-2, CRITERIA §4 ("thư viện crypto chuẩn, không tự chế") | Proposal Task 1.4 / deps list says "Kiểm tra thư viện còn bảo trì trước M1; nếu không đạt, báo lại". Verified now: `filippo.io/cpace` is self-declared experimental, unmaintained since 2021, old draft, no key confirmation. Proposal also does not say where the fingerprints come from or how "xác nhận được nhau" happens. | Decide now in the proposal (ordinary engineering choice, not a Human question): **preferred** vendor `rsc.io/cmd/mote/internal/cpace` (current draft-21, X25519, built-in `Tag/Verify` confirmation) into `internal/third_party/cpace` with its LICENSE; fallback `filippo.io/cpace` pinned + explicit HMAC-SHA256 key confirmation both ways. State: (a) pairing TLS is mutual — both sides present their long-term cert, (b) each side takes **both fingerprints from the TLS connection state** (never from message fields) and feeds them as CPace ids/AD, (c) explicit confirmation in both directions before saving any fingerprint, (d) each failed confirmation counts toward the 3-try limit. Update PROJECT_PROFILE dependency line. |
| F-03 macOS Local Network privacy not addressed | YES | GOAL-2/GOAL-11, AC 1–2 | Task 5.1 Info.plist lists only `LSUIElement`. On macOS 15+, an app bundle that browses Bonjour / connects to LAN IPs without `NSLocalNetworkUsageDescription` (and `NSBonjourServices` = `_lanbridge._tcp`) is blocked or silently fails discovery. Unsigned/ad-hoc rebuilds may re-prompt. Behaviour UNVERIFIED on target Mac. | Add both keys to the Info.plist written by `install.sh`; add to M1 verification "first launch from the .app shows the Local Network prompt; Allow → discovery and connect work; manual IP also works". Note in Task 1.3 that testing from Terminal does not prove the .app path. |
| F-04 Windows firewall instruction fails on default Public network | YES | GOAL-11, AC 2; Human is non-technical | Task 5.1: "Windows hỏi tường lửa → chọn Allow cho mạng **Private**". Windows 11 marks new Wi-Fi as Public by default, so the port (TCP 54823 and UDP 5353) stays closed with no visible error on either side. | `install.ps1` reads `Get-NetConnectionProfile` (no admin needed) and, if Public, prints a plain-language instruction to switch the home Wi-Fi to Private (or tick both boxes in the prompt). Host UI shows its listening IP:port so manual-IP can be tested. |
| F-05 Project MCP session never recovers after the server dies | YES | GOAL-7, AC 8; CRITERIA §2 priority #2 reliability | Task 3.2: "mở một phiên MCP … initialize một lần, giữ phiên đến khi Thoát". The CAI loop (test in AutoCAD → crash/restart, or `app_stop`/`app_start`) will kill a stdio child or an in-AutoCAD HTTP server; all forwarded calls then fail until Bridge restarts. | One rule: on transport error or child exit, drop the session; the next `tools/list`/`tools/call` re-launches/re-connects and re-`initialize`s; send `tools/list_changed` to the agent when tool availability changes. Return a clear error ("CAI MCP server không chạy") if re-connect fails. |
| F-06 `run_command` completion and console windows under a GUI-subsystem host | YES | GOAL-6, AC 7, AC 3 | Task 1.1 builds `bridge.exe` with `-H windowsgui`; Task 3.1 launches `powershell …` with piped output. (a) A console child of a GUI parent gets a **new visible console window** unless created with `CREATE_NO_WINDOW`. (b) .NET builds spawn long-lived helpers (MSBuild node reuse, VBCSCompiler, build servers) that inherit the stdout/stderr pipe handles, so waiting for pipe EOF (`cmd.Wait` default) can hang long after the script exited. Directly relevant to CAI (C#). | State in Task 3.1: start children with `CREATE_NO_WINDOW` (stdio MCP child too); completion = main process exit, then bounded pipe drain (`exec.Cmd.WaitDelay`, Go ≥1.20); then terminate the Job to clear leftovers. Note the small assign-after-start race for the Job Object as accepted. |
| F-07 Mac-side Exit vs. Wi-Fi drop: running jobs undefined | YES | GOAL-2/GOAL-6, AC 3 ("Exit → ngắt kết nối, hủy tác vụ dở nếu khả thi") | Proposal defines cancel only on **host** Exit (Task 1.5). Nothing says what the host does when the Mac exits, nor that a Wi-Fi blip must not kill a 10-minute build; `job_id` survival across reconnect is unstated, but `command_result` depends on it. | Mac Exit sends an explicit `bye` → host cancels that client's running jobs and pending permission requests. Heartbeat loss → jobs keep running; `job_id` and its output stay on the host until fetched or the next job for that project; `command_result` works after reconnect. |
| F-08 Where full command output lives is unclear | NO | GOAL-6 | Task 3.1: tool returns the last 200 KB, but "toàn bộ output được `bridge mcp` lưu vào file tạm trên Mac" means the host must stream or retain everything; for async jobs retention and limits are unspecified. | Host keeps each job's full output in memory with a cap (e.g. 20 MB, oldest dropped and marked truncated) until fetched or replaced; `bridge mcp` writes what it receives to a Mac temp file. |
| F-09 Agent-side naming / list_changed / timeout assumptions unverified | NO | GOAL-7, GOAL-12 | `<project>__<tool>` becomes `mcp__lanbridge__CAI__<tool>` in Claude Code; full name must fit the 64-char tool-name limit and `[A-Za-z0-9_-]`. Project tools appear only after the `mcp` grant, which relies on Claude Code honouring `tools/list_changed` (UNVERIFIED). Default `wait_seconds = 600` assumes Claude Code's MCP call timeout is longer (UNVERIFIED). | Validate project names at config load (`[A-Za-z0-9-]`, short); truncate/skip tools whose full name is too long, with a log line; declare `capabilities.tools.listChanged`. In M2, check on the real Mac that list_changed refreshes tools and measure the call timeout; if lower, reduce the default `wait_seconds` (e.g. 120) — `command_result` already covers longer runs. |
| F-10 Module ownership: agent-facing MCP server inside `mcpfwd/` | NO | GOAL-12 vs GOAL-7 | ARCHITECTURE puts both the "host-side session to project server" and the "stdio MCP server for agent" in `mcpfwd/`, but the latter is delivered in M2 (Task 2.4) and serves read/sync/commands/apps, not only forwarding. | Split: `internal/mcp` (JSON-RPC 2.0 + MCP message types, shared), `internal/agentmcp` (`bridge mcp`, M2), `internal/mcpfwd` (host-side forwarding, M3). Update ARCHITECTURE. |
| F-11 Task 1.6 has no consumer in M1 | NO | GOAL-12 | Task 1.6 (client ↔ `bridge mcp` local channel) is in M1, but its only caller (`bridge mcp`, Task 2.4) arrives in M2, and M1 verification never exercises it. | Merge Task 1.6 into Task 2.4 and move the deliverable line from Milestone 1 to Milestone 2. |
| F-12 grandcat/zeroconf maintenance and Windows port 5353 | NO | GOAL-2 | Upstream unmaintained since 2023-01. On Windows the built-in DNS Client also uses UDP 5353; whether the responder binds/shares the port is UNVERIFIED. Manual IP fallback already exists. | Keep the library, but put "Windows host is discoverable from the Mac" as the first M1 spike; if it fails, fall back to last-known/manual address (already designed) rather than adding another dependency. Record result in PROJECT_PROFILE. |
| F-13 Size-based log rotation is more than needed | NO | CRITERIA §2 (#1 lightweight, no over-engineering) | Shared modules: "`log/slog` ra file …, xoay vòng theo kích thước" — stdlib has no rotation, so this means hand-written code. Sessions are short and attended. | Truncate the log file at each start (keep one previous file at most). |
| F-14 `store` described as writing `config.json` | NO | Consistency | Shared modules: "`internal/store` … cho `config.json`/`state.json`", but `config.json` is user-authored and the app never writes it. | Say `store` reads `config.json` and atomically writes only `state.json`. |
| F-15 Minor doc consistency | NO | Consistency | Plan GOAL-7 says "(stdio/HTTP)" while proposal implements only CAI's real transport; Proposal says "tối đa 3 nhóm" deps but the list has 4 modules plus transitive (`miekg/dns`, `x/net`, `gtank/ristretto255`); PROJECT_PROFILE lists `crypto/ecdh` (not used by the current design unless the vendored CPace is chosen) and "Antigravity IDE" as runtime-native host while this run is Claude Code. | Align wording after F-02 is settled: one dependency list in PROJECT_PROFILE and the proposal, GOAL-7 says "transport thật của CAI". |
| F-16 WM_CLOSE target selection | NO | GOAL-8 | Task 4.1: "gửi `WM_CLOSE` tới cửa sổ của các process có đúng đường dẫn exe". AutoCAD owns many top-level windows (palettes, hidden windows); posting to all can be odd. `PostMessageW` is not in `x/sys/windows`, needs a `user32` lazy proc (fine). | Post `WM_CLOSE` only to visible, unowned top-level windows of matching PIDs (normally the main frame). |

## Scope check against CRITERIA

- Overengineering: none material. Localhost UI token and the Mac local-channel token are the minimum to keep the allow/deny gate meaningful against other local pages/processes; not extra security. Only F-13 trims scope.
- Missing required capability: F-01 (related sync folders), F-07 (Mac Exit cancels tasks — AC 3).
- Non-goals respected: no service/autostart, no DB, no watcher, no shell, no per-tool policy, no sandbox/audit, no resources/prompts forwarding, Windows needs only `bridge.exe` (matches the Human's "install as little as possible").

## Verdict

**CHANGES_REQUIRED.** Blocking: F-01, F-02, F-03, F-04, F-05, F-06, F-07 — each fixable with a few lines in the descriptive proposal (plus matching Plan/Milestones/Architecture/Profile wording); no direction change and no Human escalation needed. Non-blocking F-08..F-16 should be folded in on the same revision. Coder revises `docs/TECHNICAL_PROPOSAL.md` (and aligned docs) and resubmits the whole proposal to this Critic; no source edits before CR PASS.

---

## Re-review (rev 3)

- Target: PRE-CODE — docs/TECHNICAL_PROPOSAL.md rev 3 plus aligned IMPLEMENTATION_PLAN.md, MILESTONES.md, ARCHITECTURE.md, PROJECT_PROFILE.md (read in full, 2026-11-02).
- Mode: PRE_CODE_TECHNICAL, whole-Plan, top-down. Review run as a Claude Code subagent invoked by the main chat agent; GUIDED_ONLY.
- Extra verification this round: `rsc.io/cmd/mote/internal/cpace` imports **only stdlib** (`bytes`, `crypto/ecdh`, `crypto/hmac`, `crypto/rand`, `crypto/sha512`, `errors`, `io`, `math/bits`). `Config` has `Role`, `Password`, `ChannelID` (both sides must use the same value; it is not sent on the wire), `SessionID`, `AD` (sent in clear, authenticated), and `Rand`. `Tag`/`Verify` provide explicit key confirmation (pkg.go.dev).

### Finding status

| Finding | Blocking? | Status in rev 3 | Evidence | Remaining action |
|---|---|---|---|---|
| F-01 related_dirs | was YES → resolved | RESOLVED | Proposal config `related_dirs`, Task 2.1 sync scope, Task 2.3 `target`, M2 verification syncs to a related dir; Milestones M2, Architecture aligned. | See N-01 (example path). |
| F-02 CPace / binding / confirmation | was YES → resolved | RESOLVED | Deps list: vendored mote cpace (draft-21) with LICENSE plus source and commit; Task 1.4: mutual TLS, fingerprints taken from TLS connection state, 2-way Tag/Verify, failed confirmation counts toward the 3-try limit. Plan, Milestones, Architecture aligned. | See N-02 (bind through `ChannelID`), N-03 (dependency note). |
| F-03 macOS Local Network | was YES → resolved | RESOLVED | Task 1.1 Info.plist adds `NSLocalNetworkUsageDescription` and `NSBonjourServices`; M1 verification runs from `.app`; Task 5.1 says to allow the prompt. | — |
| F-04 Public network profile | was YES → resolved | RESOLVED | Task 5.1 `Get-NetConnectionProfile` check with plain-language steps; host UI shows IP:port (Tasks 1.5, 4.3); Milestones M5 aligned. | — |
| F-05 MCP session re-create | was YES → resolved | RESOLVED | Task 3.2 drops the session on error, re-`initialize`s on the next call, and sends `list_changed`; M3 verification restarts the server; Plan GOAL-7 and Architecture aligned. | — |
| F-06 console windows / pipe hang | was YES → resolved | RESOLVED | Task 3.1 `CREATE_NO_WINDOW`; done = main process exit, then `cmd.WaitDelay`, then terminate the Job; M3 verification checks that no console appears. | — |
| F-07 `bye` vs heartbeat loss | was YES → resolved | RESOLVED | Task 1.3 defines both paths; `job_id` survives reconnect; M3 verification fetches `command_result` after a Wi-Fi drop. | See N-04 (a Mac-Exit-during-build check). |
| F-08 output retention | NO | RESOLVED | Task 3.1: ~20 MB/job in memory, keeps the tail and marks it truncated, kept until the next job. | — |
| F-09 naming / list_changed / timeout | NO | RESOLVED | Name rule `[A-Za-z0-9_-]{1,16}`; over-long tools reported on UI; `listChanged` declared; M2 measures list_changed and the timeout, with `/mcp` fallback; `wait_seconds` default tied to that measurement. | — |
| F-10 module split | NO | RESOLVED | `internal/mcp`, `internal/agentmcp`, `internal/mcpfwd` in Proposal and Architecture. | — |
| F-11 Task 1.6 consumer | NO | RESOLVED | Local channel merged into Task 2.4. | — |
| F-12 zeroconf spike | NO | RESOLVED | Task 1.2 spike runs first; manual IP is the fallback; no second mDNS library. | — |
| F-13 log rotation | NO | RESOLVED | One log file, truncated on each start. | — |
| F-14 store / config | NO | RESOLVED | `store` reads config.json only and writes state.json atomically. | — |
| F-15 doc consistency | NO | MOSTLY RESOLVED | Single dependency list; GOAL-7 wording fixed. Small leftovers in N-03 and N-05. | Optional. |
| F-16 WM_CLOSE target | NO | RESOLVED | Task 4.1: visible, unowned main window only. | — |

### New observations (all non-blocking; may be folded in during implementation)

| Finding | Blocking? | Goal / condition | Evidence | Suggested action |
|---|---|---|---|---|
| N-01 Example related dir may not be writable | NO | GOAL-5 | The config example uses `C:\ProgramData\Autodesk\ApplicationPlugins\CAI.bundle`. Folders there are usually created by an admin installer, so a non-admin `bridge.exe` may get "access denied". | Use the per-user `%APPDATA%\Autodesk\ApplicationPlugins\CAI.bundle` in the example, and make sure access-denied is reported with the file name (it is already covered by "báo lỗi rõ"). |
| N-02 How fingerprints enter CPace | NO | GOAL-2 | Task 1.4 says "ràng buộc vào hai fingerprint" but does not name the field. In the vendored API, `ChannelID` must match on both sides and is not sent; `AD` is per-party and sent in clear. | Put a canonical `host_fp ‖ client_fp` (always host first) into `ChannelID`; use fixed Initiator (Mac) / Responder (Windows) roles. |
| N-03 Dependency note inaccurate | NO | Consistency | Proposal: "`golang.org/x/crypto` cho X25519". The vendored package uses stdlib `crypto/ecdh`, which PROJECT_PROFILE's allowed-stdlib line omits. | Drop x/crypto from the note; add `crypto/ecdh` to the profile's stdlib list. |
| N-04 Milestone verification lags the proposal | NO | GOAL-6/7, AC 3 | MILESTONES M3 verification lacks the Wi-Fi-drop + `command_result`, no-console and MCP-restart checks that the proposal M3 has. Neither file explicitly tests "Mac Exit during build → no build process on Windows". M1 lists the `bye` job-cancel deliverable, but jobs only exist from M3. MILESTONES still says "rev 2". | Copy the proposal M3 checks into MILESTONES M3 and add "Mac Thoát giữa lúc build → Windows không còn process build"; keep only the `bye` message in M1. |
| N-05 Profile host line | NO | Consistency | PROJECT_PROFILE "Runtime-native agent…: Antigravity IDE" while this workflow runs in Claude Code. | Correct when the Environment Architect next updates the profile. |

### Scope check (rev 3)

No new feature beyond CRITERIA. The additions (`related_dirs`, the config "Tải lại" button, the network-profile hint, the IP:port display) serve approved §4, §5 and §11 items. They add no new security mechanism and nothing new installed on Windows. The direction is still the smallest credible design.

### Verdict (rev 3)

**Critic CR: PASS.** All blocking findings F-01..F-07 are resolved in the proposal and aligned docs. F-08..F-16 are resolved, F-15 mostly. N-01..N-05 are non-blocking clarifications that Coder may fold into the proposal/Milestones or handle during implementation without another CR round. `docs/TECHNICAL_PROPOSAL.md` rev 3 is the final proposal; implementation may proceed after the setup lock (main path step 7). OS behaviours still UNVERIFIED (macOS Local Network prompt, Windows UDP 5353 sharing, Claude Code list_changed/timeout, CAI MCP transport) are scheduled as real-machine checks in M1–M3.

---

## Delta review — status icons

- Scope: only the uncommitted icon delta. That is TECHNICAL_PROPOSAL (the Icon entry in the dependency list, the Task 1.5 icon state table, and the `internal/ui` line), MILESTONES M1 (deliverable and verification) and ARCHITECTURE (key decision line). Mode PRE_CODE_TECHNICAL; GUIDED_ONLY subagent run.
- Verified 2026-11-02:
  - Lucide `icons/cable.svg` and `icons/unplug.svg` exist on lucide-icons/lucide `main` (HTTP 200). They are 24×24 stroke icons using `stroke="currentColor"`, width 2, round caps and joins, paths only.
  - fyne.io/systray Windows `SetIcon`: the doc says "content of .ico for windows". The bytes are written to a temp file and loaded with `LoadImageW(IMAGE_ICON, cx=cy=0, LR_LOADFROMFILE|LR_DEFAULTSIZE)`. That loads the frame closest to the system *large* icon size (SM_CXICON: 32 px at 100 %, 48 px at 150 %), which the shell then scales down.
  - fyne.io/systray macOS `setIcon`: a single byte slice goes through `NSImage initWithData` and then `setSize:NSMakeSize(16, 16)`, with `image.template` set only by `SetTemplateIcon`. A plain `SetIcon` PNG is therefore drawn in its own colours at a fixed 16 pt. Separate `@2x` files are not supported.
  - `srwiley/oksvg`: last commit 2022-10. It is a dev-time tool only, so this is acceptable.
- Proportionality: proportionate. The icon work is one SVG→PNG/ICO step at development time, the output files are committed, and nothing new ships in the binary or is installed on Windows. A tooltip per state and a different icon shape for "disconnected" are cheap and help a non-technical user. The delta adds no notifications, animation or new UI framework.
- Is a dev-only rasterizer the simplest option? Close to it. The alternatives are:
  - Download PNGs by hand from lucide.dev at a chosen colour and size. That avoids oksvg, but it is manual and still leaves the ICO to be built.
  - Use `rsvg-convert` or ImageMagick. Both need a Homebrew install on the Mac.

  ICO needs an encoder either way. A small Go tool is repeatable and needs no extra install, so keep it. Keep its dependency out of the product module (N-08).

| Finding | Blocking? | Goal / condition | Evidence | Smallest fix |
|---|---|---|---|---|
| N-06 macOS icon size wording does not match systray | NO | GOAL-10 | Proposal says "macOS 18 px (+ bản @2x)". systray accepts one image and always sizes it to 16×16 pt. | macOS: one PNG of 32×32 px (sharp on Retina, shown at 16 pt), passed to `SetIcon`, not `SetTemplateIcon`. |
| N-07 Windows ICO frames | NO | GOAL-10 | Proposal ICO frames are 16/24/32. `LR_DEFAULTSIZE` picks the large-icon size, so at 125–150 % scaling Windows upsamples 32 px or downsamples blurry. The 2 px strokes suffer. | ICO frames 16, 20, 24, 32, 40, 48 (PNG-compressed frames are fine). Check sharpness at 100 % and 150 % in M1. |
| N-08 Keep dev-only dependency out of the product module | NO | CRITERIA §2 #1 (few dependencies) | If `tools/genicons` lives in the main module, oksvg/rasterx appear in the main `go.mod` even though the binary does not link them. | Give `tools/genicons` its own `go.mod` (or `//go:build ignore` + `go run` with a separate module). Note it under the dependency list as "dev-only, separate module". |
| N-09 Colour/contrast and licence notes | NO | GOAL-10 | A non-template coloured icon is not tinted by macOS, so it keeps the same colours on light and dark menu bars. Grey `#8E8E93` is mid-contrast on both; the other three states share one shape. Lucide's LICENSE is ISC with an MIT notice for Feather-derived icons. | Add to M1 verification: "nhìn rõ trên thanh menu sáng, tối và taskbar Windows sáng/tối". Commit Lucide's full LICENSE file unchanged (already intended). No other change. |
| N-10 Consistency | NO | Consistency | Proposal, Milestones M1 and Architecture agree. The blue "Đang chạy lệnh" state can only be verified from M3, when commands exist. M1 verification correctly lists only connected / disconnected / waiting for pairing. | Optionally add "icon xanh dương khi đang build" to M3 verification. |

**Verdict (delta): PASS.** The delta is proportionate and stays inside CRITERIA. Its basis is correct: the Lucide icons `cable` and `unplug` exist, the licence is permissive, and systray takes ICO on Windows and a coloured PNG on macOS. N-06..N-10 are non-blocking corrections that Coder may fold into the proposal text or apply during M1 without another CR round.
