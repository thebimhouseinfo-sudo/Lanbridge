# Tasks — LAN Test Bridge

## Amendment tasks — planning only, no source implementation yet

Plan: [Implementation Plan amendment](docs/IMPLEMENTATION_PLAN.md); milestones 11–14: [Milestones](docs/MILESTONES.md); descriptive design: [Technical Proposal V3](docs/TECHNICAL_PROPOSAL_V3.md). Human's full request and clarifications in this conversation govern the scope; no accepted DEC files are present. PRE_CODE_TECHNICAL review PASS after revision; implementation and native acceptance pending. The older Active Task below is historical and does not authorize this amendment's code.

- **TASK-11 — Generic MCP:** GOAL-19; implement and verify M11, including configuration, stdio/HTTP, identity/session, list changes, redacted errors, grants and blacklist. Prerequisites: V3 final Critic PASS; verify server identity and secret-store support. Evidence: generic independent server and Windows/macOS CI, with CadGPT outer MCP on two real machines separately recorded.
- **TASK-12 — Bridge source maintenance:** GOAL-20; implement and verify M12 scoped paired-device source Read/Sync, Mac agent discovery, visible revoke and protected paths without routine notifications. Prerequisites: V3 final Critic PASS; resolve source registration and credential boundary. Evidence: missing-project-config, deny/revoke, aliases, locked files, regressions and real Windows/Mac build without a Windows coding agent.
- **TASK-13 — Awake and idle:** GOAL-21; implement and verify M13 lifecycle and connection behavior on both OSs. Prerequisites: V3 final Critic PASS and OS API availability. Evidence: automated lifecycle tests and actual OS sleep and network tests, recorded separately.
- **TASK-14 — Integrated delivery:** GOAL-22; M14 candidate CI, same-SHA packages, installer preservation and physical acceptance. Prerequisites: tasks 11–13 and reviewer/test evidence; no main release before candidate checks. Evidence: workflow links and actual two-machine outcomes, with unsupported tests UNVERIFIED.

Status: CODE COMPLETE; REAL-HOST ACCEPTANCE DEFERRED

## Historical Active Task (prior scope; not active for this amendment)
- **ID:** TASK-2.5
- **Milestone:** Milestone 2: Permissions, Read, Sync and Mac agent interface
- **Goals:** GOAL-12 (CRITERIA §5, §6, §7, §8, §9, §10; CRITERIA unchanged)
- **Scope:** Add Codex alongside Claude Code for `bridge mcp`. Settings selects exactly one provider and manages its user-scope MCP registration; Bridge reads both provider config files and preserves unrelated MCP entries. A cross-process OS lock prevents concurrent `bridge mcp` instances and blocks provider changes while a session holds the lock. Add a Codex config example and setup guidance.
- **Decisions:** No accepted DEC records found under `docs/discussions/`; Human selected Bridge-managed provider switching, with only Claude Code or Codex registered at a time.
- **Planned Verification:** Test config inspection and provider switching, preservation/conflicts/error reporting, and lock exclusion/release. Run `go test ./...`, `go vet ./...`, and Windows GUI build; compile for macOS. Verify the supported Codex CLI configuration and live lifecycle on macOS.
- **Exit Evidence:** Local tests/builds; **- test required** for Claude Code and Codex CLI registration, connection, and disconnect on the target Mac.

## Next Tasks
- **ID:** TASK-5.2
- **Milestone:** Milestone 5: Installation and acceptance
- **Goal:** GOAL-11 (CRITERIA §11)
- **Scope:** Run the 12 acceptance items on the paired Mac/Windows machines with the CAI repository after the user pushes the completed code, including verification that a Windows user can receive/copy an agent-provided script via shared chat. Do not substitute mocks or local tests for the real-host run.
- **Planned Verification:** Install and launch both packages; verify pairing/reconnect, permissions, read/sync, build/output, CAI MCP forwarding/restart, Start/Stop, chat, exit cleanup, and idle CPU/RAM; record actual results in `docs/reports/`.
- **Exit Evidence:** **- test required** (awaiting push and access to both target machines).

## Done Tasks
- **TASK-4.2 / TASK-4.3:** Implemented chat-first UI, Settings grouping, shared agent-to-chat route, and Bridge-observed activity. **- test required:** verify real agent chat/activity and native UI on both target machines.
- **TASK-5.1:** Added Mac and Windows installers and packaging wiring. The Windows installer was exercised with isolated temporary directories; it copied the executable, preserved/created config, and created both shortcuts. `sh -n scripts/install.sh` passed. **- test required:** run the Mac installer and native `.app` launch on macOS.
- **TASK-4.3:** Added host/client project, app, permission, config-error, MCP-registration, and job status UI state/rendering. Reopened for chat-first layout, Settings grouping, and Bridge-observed activity. **- test required:** verify both live role views and all interactions on the target machines.
- **TASK-4.2:** Added token-gated UI-to-UI chat send endpoints, two-way TLS messages, in-memory session history, pasted text/log display, and per-message copy. MCP agent-to-chat sending is still missing and is part of the reopened TASK-4.2 / TASK-4.3 work. **- test required:** verify two-way delivery and clipboard behavior on both machines.
- **TASK-4.1:** Added configured-app Start/Stop with session-only permission (user confirmed it resets off on Bridge restart), separate process launch, exact executable matching, and graceful `WM_CLOSE` without force-kill. **- test required:** verify with a real configured Windows app, including a save prompt and Bridge exit.
- **TASK-3.2:** Added project MCP session recovery, tool forwarding, and tool-list change notifications. Focused and full Go tests pass. **- test required:** inspect the actual CAI MCP transport and invoke/restart a real CAI tool on macOS; add transport support only if that evidence requires it.
- **TASK-3.1:** Added fixed-command execution, Job Object lifecycle, `WaitDelay`, capped output buffering, chunk retrieval, and Mac temporary output files. Focused and full Go tests pass. **- test required:** run the configured CAI build on Windows and verify output, window behavior, reconnect, timeout, and process cleanup on the two machines.
- **TASK-2.4:** Completed the localhost `bridge mcp` interface and wired request, read, sync, project, and command tools. Focused and full Go tests pass.
- **TASK-1.4:** Host/client six-digit CPace pairing, confirmation tags, identity persistence, TLS pinning, and unpair/grant clearing. [Evidence: `internal/transport/pairing/`, `internal/transport/server.go`, `cmd/bridge/main.go`, `internal/ui/`; focused pairing/TLS/UI tests, `go vet ./...`, Windows build, and macOS ARM64 transport compile pass. Full Go suite now passes; two-machine macOS/Windows acceptance remains outstanding].
- **TASK-1.3:** Client TLS connection and 3–30 second reconnect loop, persistent last-host address, mDNS advertise/browse with manual IP[:port] fallback, LAN IP display, and pre-pair operational-request denial. [Evidence: `cmd/bridge/main.go`, `internal/ui/web.go`, `internal/ui/embed/index.html`, `internal/transport/discovery.go`; focused tests, live local mDNS test, `go vet ./...`, and Windows build pass. Two-machine macOS/Windows acceptance remains outstanding].
- **TASK-2.3:** Host `sync_files` handler, project root/related-dir mapping, permission and 5 MB checks, atomic replacement, SHA-256 results, and Windows locked-file detection. [Evidence: `internal/fileops/sync.go`, `internal/fileops/fileops_test.go`, `cmd/bridge/main.go`; Windows lock test and host handler tests pass. Full Go suite now passes].
- **TASK-2.2:** Read operations and host protocol handlers for list, metadata, chunked read and tail lines. [Evidence: `internal/fileops/read.go`, `internal/fileops/fileops_test.go`, `cmd/bridge/main.go`; focused tests, vet and Windows bridge build pass. End-to-end MCP delivery completed in TASK-2.4].
- **TASK-2.1:** Permission gate and request approval; canonical path checks, capability constraints, host Accept/Reject/edit/revoke UI. [Evidence: `internal/permissions/`, `internal/ui/`, `cmd/bridge/main.go`; focused tests, transport integration tests, and Windows bridge build pass. Full Go suite now passes].
- **TASK-1.1:** Khởi tạo Go module, Makefile cross-compile Windows/macOS, packaging Info.plist, entrypoint `cmd/bridge/main.go` với 3 vai (host, client, mcp). [Evidence: `cmd/bridge/main.go`, `cmd/bridge/main_test.go`, `Makefile`, `go.mod`].
- **TASK-1.5:** Khung UI tối thiểu: HTTP server localhost với token `t=<token>`, phục vụ HTML/CSS/JS thuần embed, SSE streaming, nút ghép nối/mở config/thoát, tích hợp tray icon với 4 màu trạng thái (`internal/ui/`). [Evidence: `internal/ui/embed/index.html`, `internal/ui/web.go`, `internal/ui/tray.go`].

## Retired / Blocked Tasks
*(None)*
