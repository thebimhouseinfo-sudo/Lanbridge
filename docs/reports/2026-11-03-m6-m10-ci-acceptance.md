# TestBridge — M6–M10 GitHub CI acceptance and native handoff (2026-11-03)

**Scope and decision:** Automated-host acceptance for reinstall test builds, **not** final native two-machine acceptance. No test on a physical Mac/Windows pair has been executed for this revision. When GitHub Actions for the final PR head and merged main are successful, this report authorizes **CI-verified test-release packaging and merge** as requested by the human. Native cases remain explicitly pending and cannot be labeled PASS.

- Repository: `thebimhouseinfo-sudo/testbridge`
- Implementation branch: `feature/criteria-v13-implementation`
- Criteria: `docs/CRITERIA.md` (40 acceptance criteria, unchanged)
- Reviewed Plan: `docs/IMPLEMENTATION_PLAN.md`
- New Proposal: `docs/TECHNICAL_PROPOSAL_V2.md`
- Evidence before final report commit: [GitHub Actions — 218e3ec2, success](https://github.com/thebimhouseinfo-sudo/testbridge/actions/runs/41583936763) and [previous full success — 1faf80c7](https://github.com/thebimhouseinfo-sudo/testbridge/actions/runs/41578631186).
- **Mandatory recheck:** This file's commit and PR merge change the head. Verify **newest** branch/main Actions results independently before publishing or claiming new SHA PASS; no stale success reuse.

### Actual CI observed

On previously completed Windows/macOS GitHub Actions runs: `go test -count=1 ./...`, `go vet ./...`, Node embedded UI JavaScript syntax check, platform build/package, and `go test -race -count=1 ./...` on macOS. CI does **not** execute real Windows CAD application/MCP, physical LAN connectivity, native UI mouse flows, installer on a user's machine or idle measurements.

### Criteria §11 evidence matrix

Notation: **CI_VERIFIED** means automated source/unit checks exercise the stated rule; **PARTIAL** means some checks exist but real-host behavior still pending; **NATIVE_PENDING** means no claim that scenario has been verified on the physical hosts.

| # | Acceptance | Assessment | Evidence / remaining test |
|---:|---|---|---|
| 1 | Installers và native tray/menu | NATIVE_PENDING | CI build/package trên Windows/macOS PASS; cài/khởi chạy trên máy thật chưa thực hiện |
| 2 | Pairing LAN và reconnect | NATIVE_PENDING | Transport unit tests hiện có; cần Mac↔Windows LAN thật |
| 3 | Exit ngắt kết nối/hủy job | PARTIAL | Executor CancelAll + transport regressions; descendant/native Close chưa xác minh |
| 4 | Request, accept/reject, edit/revoke | PARTIAL | Gatekeeper test + UI route; native UI chưa kiểm |
| 5 | Read files/logs/ranges | CI_VERIFIED | Fileops/permissions unit + existing regression |
| 6 | Sync folder đã grant, hạn mức 5 MB | CI_VERIFIED | Gatekeeper Resolve + fileops Sync và traversal tests |
| 7 | Named build + Shell Exec, output/exit | CI_VERIFIED | TestRunShellGenericWindowsOrMacCommandAndWorkdir; existing executor tests |
| 8 | MCP tool thật forward/invoke | NATIVE_PENDING | Forwarder tests; cần MCP server Windows thật |
| 9 | Start/Stop đúng app được grant | PARTIAL | TestProjectSwitchReplacesAppsAndGranularRights; Windows process thật chưa kiểm |
| 10 | Chat text hai chiều | PARTIAL | Agent chat/protocol tests; hai máy thật chưa kiểm |
| 11 | Không có unattended/overnight runner | PARTIAL | Không phát triển scheduler mới; runtime/Exit thực tế chưa kiểm |
| 12 | Idle CPU/RAM thấp | NATIVE_PENDING | Cần đo trên Mac/Windows thật |
| 13 | Windows Add Folder/Actual Grant độc lập request | PARTIAL | TestWindowsActualGrantMayExceedRequestAndSyncOutsideProject; native Windows picker chưa kiểm |
| 14 | Edit/Revoke có hiệu lực active session | PARTIAL | Revoke permission tests + CancelAll wiring; thực tế Windows chưa kiểm |
| 15 | Read/Sync ngoài projects/related_dirs | CI_VERIFIED | TestGatekeeperConstraints + Resolve; no root eligibility |
| 16 | Save/SaveAs/Rename/Delete/Load Config cục bộ | CI_VERIFIED | TestWindowsProfileSaveAsRenameDeleteAreLocal; Mac Locked tests |
| 17 | Windows manual Open; Mac agent existing-only Open | PARTIAL | TestMacProjectUIRequiresExistingFileAndManualUnlock; host UI LAN path pending |
| 18 | Windows Add Application/Exec restart | PARTIAL | App permissions + Shell tests; restart ứng dụng Windows thật pending |
| 19 | CAI Active/New/Test DWG workflow | NATIVE_PENDING | CAI repo/runtime bên ngoài Bridge; kiểm trên máy thật ở phase cuối |
| 20 | Blacklist empty mặc định, AutoCAD chỉ opt-in | CI_VERIFIED | TestMatcherStartsEmptyAndDeniesOnlySelectedTools; không hardcode CAI/RAI |
| 21 | Bridge không parse toàn bộ CAD Shell | CI_VERIFIED | Source scope verified; nested CAD deny không tuyên bố verified |
| 22 | Hai config trên hai máy, không Sync | CI_VERIFIED | TestWindowsPermissionsIndependentAndProjectIDValidation; TestPrivateBridgeConfigPaths |
| 23 | Windows Edit/Revoke dù Mac Locked | PARTIAL | Hai UI/code path độc lập; native UI chưa xác minh |
| 24 | Agent sửa blacklist khi manual Unlock | PARTIAL | Store lock/UI tests; user-directed intent là workflow requirement |
| 25 | Locked read-only + persisted | CI_VERIFIED | TestLockPreventsEditAndPersists; TestMacLockedCannotRenameSaveAsDelete |
| 26 | Chỉ Mac UI có Lock/Unlock; không có MCP Unlock | PARTIAL | Tool list/HTTP UI tests; native UI human interaction pending |
| 27 | Blacklist dự án độc lập; không bypass bằng Shell | PARTIAL | Session/matcher/config tests; arbitrary opaque Shell không phải sandbox |
| 28 | Mac blacklist UI, Windows không blacklist UI | PARTIAL | Embedded HTML JS syntax PASS; native UI pending |
| 29 | Agent chỉ edit qua workflow riêng | PARTIAL | config_load read-only, blacklist_edit locked tests; operator intent pending |
| 30 | Sau kết nối Windows manual Open + Mac existing-only Open | PARTIAL | Local profile tests + code guards; LAN handshake pending |
| 31 | Không sync/copy config qua LAN | CI_VERIFIED | TestPrivateConfigThroughSymlink; readLocalSyncFile guarded |
| 32 | PROJECT_READY chỉ khi hai file load cùng ID | CI_VERIFIED | TestSessionRequiresBothLoadedAndMatchingID; TestHostProjectOperationGuardCoversEveryExecutionSurface |
| 33 | Restart giữ file, không auto-open | PARTIAL | Store persists, session clear; native restart pending |
| 34 | First-time Mac file blacklist rỗng | CI_VERIFIED | TestFirstTimeEmptyAndExistingNeverOverwritten |
| 35 | Loaded empty => all MCP project tools | PARTIAL | TestMacProjectDispatchFailsClosedWithoutLoadedBlacklist + MCP test; real server pending |
| 36 | Missing/invalid/unloaded => block toàn bộ project ops | CI_VERIFIED | TestMissingAndInvalidAreNeverTreatedAsEmpty; TestHostProjectOperationGuardCoversEveryExecutionSurface |
| 37 | Agent load existing-only; no overwrite | CI_VERIFIED | TestFirstTimeEmptyAndExistingNeverOverwritten; TestMacProjectUIRequiresExistingFileAndManualUnlock |
| 38 | Blacklist rỗng tồn tại qua restart | CI_VERIFIED | Projectconfig store persistence tests |
| 39 | User Unlock, agent Edit, Lock giữ entries | PARTIAL | TestMacProjectUIRequiresExistingFileAndManualUnlock; native manual interaction pending |
| 40 | Generic Windows repository, no CAI-specific default deny | CI_VERIFIED | TestMatcherStartsEmptyAndDeniesOnlySelectedTools; generic non-CAD shell test |

**Totals:** NATIVE_PENDING=5, PARTIAL=19, CI_VERIFIED=16; 40 criteria accounted for. Do not treat this count as 40 PASS.

### Reviewer findings / scope reconciled

- A new Windows project cannot inherit another project's actual folder/program grants implicitly (first-time profile starts empty).
- Sync outside `projects/related_dirs` follows Windows's actual grants; the Bridge project config files are excluded from generic read/sync transfer.
- Windows is sole owner of Permission Config / folder and app rights; Mac owns blacklist / human manual Lock/Unlock.
- Mac agent `config_load` reads only an existing file; first-time app setup creates an empty file; absent/unloaded file prevents `PROJECT_READY`.
- Two files stay **local and distinct**, sharing only Project ID/readiness metadata.
- AutoCAD OPEN/COM restrictions are opt-in user-configured examples and are **not** default rules for new apps. Opaque nested command matching that cannot be identified at Mac boundary is **UNVERIFIED** until integration; do not claim app-specific enforcement.
- Versioned release tags `test-<main commit SHA first 12 chars>` supersede the old fixed tag `test-2026-11-03`; each release includes Mac/Windows packages and `SHA256SUMS.txt` for correct reinstall identification.
- No new GSA Project/Job created. Direct harness/skills used for planning/coding/review/test guidance.

### Human local retest handoff — must perform **after** release build

1. Download and install **both packages from the same newest `test-<main SHA>` release**; verify SHA256, app launches, tray/menu behavior, old App Data unaffected, and firewall permissions.
2. Connect on LAN; pair or reconnect; Windows UI create a new profile with **zero inherited grants**, then explicitly Grant Folder/Program and manually Open it.
3. On Mac create first-time empty blacklist with **same Project ID**; agent `config_load` existing file only; verify all generic MCP project tools listed and callable.
4. Test wrong ID, absent/corrupt/unloaded Mac blacklist; verify project tools/Read/Sync/Exec/App are blocked. Verify neither project config transfers across machines.
5. Mac manual Unlock, ask agent to add one exact tool deny, verify blocked, Lock, then edit attempt rejected and persisted after restart.
6. Test Windows Grant bigger/smaller than Mac request, edit/revoke, Read logs, Sync ≤ 5 MB within granted arbitrary folder, denied Sync outside grant, named and Shell builds.
7. Test Start/Stop or restart on a throwaway Windows program; avoid destructive tests on production drawings. Optional CAI sample only with an explicitly designated test drawing and user-defined blacklist.
8. Verify Exit/cancellation, idle CPU/RAM and reconnect. Capture exact version, host OS, results and logs; mark each NATIVE_PENDING/PARTIAL case PASS/FAIL based on actual results.

**Merge/release gate:** successful *latest* PR CI on `macos-15` and `windows-latest`, clean independent review of code/document changes, no known failing automated regression, then merge with expected head SHA. After merge, require **main CI** and `publish-test-packages` success and verify **v2 release assets**; only then tell the user the installers are ready. The package source SHA must match merged `main`; the old fixed-tag releases are not installation candidates. If any gate fails, stop and report the blocker instead of pretending completed.
