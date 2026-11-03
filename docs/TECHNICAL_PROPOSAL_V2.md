# Technical Proposal V2 — Generic Mac–Windows LAN Test Bridge

Status: NEW TECHNICAL PROPOSAL / NOT IMPLEMENTED — 2026-11-03
Criteria source: docs/CRITERIA.md, blob 8a9e88c0bb3dca430c97fc57cc74b9406426390b
Reviewed Plan: docs/IMPLEMENTATION_PLAN.md, blob f821efded9fc5d6f7e7c0589908c63a4ca2bdbfc
Review evidence: docs/reports/2026-11-03-plan-v1.3-direct-review.md
Milestones: docs/MILESTONES.md, legacy M1–5 and planned M6–10
Legacy proposal: docs/TECHNICAL_PROPOSAL.md (retained unchanged as historical v1.0 design)
Mode: GSA Direct Agent harness/skills, no new GSA Project/Job/Memory. This file documents a proposed implementation, not a code or native-test PASS.

## 1. Objective, scope and precedence

A single lightweight Go bridge serves **multiple unrelated Windows software repositories**, not specifically AutoCAD or Revit. Mac hosts coding agent/IDE; Windows runs the app and test tools. The bridge is an attended LAN gate: connect, pair, grant, local Read/Sync, Shell, start/stop, project MCP, two-way text and per-project config selection. It does not become a CAD controller, sandbox, CI service or background scheduler.

The two hard product invariants are:

1. **Per-project two-file contract** — Windows owns one permission config; Mac owns one command-blacklist config. Each saved in the owning machine's App Data. They are NOT synced, replicated or copied over the Bridge; only Project ID/readiness metadata is exchanged. Global pairing/state files are not counted as extra project configs.
2. **Default tools unblocked, missing blacklist blocked** — Mac app first-time setup creates a valid empty blacklist for a new project. Mac agent's ordinary Open can read only an existing blacklist. To enter PROJECT_READY, both local project files must be successfully opened and their Project IDs must match. Empty loaded list means no tool restriction. Missing/unopened/malformed blacklist prevents all project Read/Sync/Exec/App/MCP calls.

The independent **Windows grant** remains authoritative for host operation permissions. Mac blacklist can further restrict eligible project tool calls when the user explicitly configures it, but can never grant host capabilities.

Do not replace other existing code merely for architectural taste. Preserve existing Go binaries, mTLS pairing, mDNS/manual IP, tray/menu bar, HTML/SSE UI, chat, fileops, MCP bridge process/runtime lock and process-output handling where reusable.

## 2. Observed legacy implementation

Source inspected in TestBridge main on 2026-11-03:

| Existing file | Observed status | Required adaptation |
|---|---|---|
| internal/store/config.go | Windows HostConfig loads global config.json; Projects include Root, RelatedDirs, fixed Commands, MCP; Apps separate | Introduce per-project Windows permission file and profile store; retain global connection settings; explicit migration |
| internal/store/state.go | state.json stores paired fingerprint and PermissionGrant list | Preserve global pairing; migrate grants only with Windows user's selection; do not erase paired identity |
| internal/permissions/gate.go | RespondRequest rejects any capability not in original request; ValidateGrantConstraints restricts Sync to project roots/related_dirs | Distinguish Requested and Actual Grant; eliminate artificial project-root Sync constraint |
| internal/permissions/resolve.go | Shared path resolution helpers | Reuse for actual granted path and config-file exclusion |
| internal/fileops/read.go, sync.go | Folder file read and on-demand Sync | Keep size cap, atomic write, error reporting, add deny for config files |
| internal/executor/executor.go | Project registry commands, bounded outputs, cancellation and timeout | Retain convenient named commands; add Shell Exec path with actual Windows approval |
| internal/agentmcp/server.go | Mac MCP stdio, local sync source read, forwarder, tool list and command output | Enforce PROJECT_READY and Mac blacklist before any project forwarding |
| internal/mcpfwd/fwd.go | Windows MCP session manager using real project MCP tool list/call | Remain generic, no baked-in CAI/RAI deny list |
| internal/protocol/message.go | Existing message kinds for permissions, file sync, exec and MCP | Extend minimally with Project ID/readiness metadata; no config file contents |
| internal/ui/web.go, internal/ui/embed/index.html | Shared localhost UI, callback-based Settings and SSE state | Add machine-specific profile/permissions/blacklist controls and errors |
| cmd/bridge/main.go | Integrates global config, permission Gatekeeper, sync handler, exec and MCP | Bind project selection to one loaded local config and active connection |

These are live-source observations, NOT proof that new Criteria are already implemented. Old docs/ARCHITECTURE.md and original Technical Proposal reflect the previous fixed-commands/project-root design and are historical, not new requirements.

## 3. High-level runtime arrangement

~~~text
Mac machine                                  Windows machine
---------------------------------------------------------------
Bridge app UI / App Data                    Bridge host UI / App Data
  profiles/<project-id>/                     profiles/<project-id>/
    command-blacklist.json                    permissions.json
    [Local ONLY]                              [Local ONLY]
  manual Lock/Unlock                         manual Open, Grant/Edit/Revoke
      |                                           |
  bridge client -- authenticated LAN transport -- bridge host
      |       Project ID/readiness metadata only  |
  bridge mcp                                   fileops/executor/apps
      |                                        project MCP forwarder
  Coding agent                                 Windows app being developed
~~~

Global config/pairing state may still exist at both ends; do not count as project config #3. The existing transport's pairing and credentials remain in their own global state.

**Important separation:** Mac must never receive Windows permission file contents; Windows must never receive Mac blacklist file contents. The runtime may report **effective capabilities, currently selected Project ID, readiness and errors**, because these are necessary operation results/metadata, not file replication. No JSON blob/hashes of counterpart config needed.

## 4. Data model and local storage (M6)

### 4.1. Windows permission profile (proposed logical JSON)

~~~json
{
  "schema_version": 1,
  "project_id": "my-windows-app",
  "display_name": "My Windows App",
  "workspace": "D:\\Dev\\MyWindowsApp",
  "folders": [
    {"path": "D:\\Dev\\MyWindowsApp", "capabilities": ["read", "sync"]}
  ],
  "capabilities": ["execute", "mcp"],
  "applications": [
    {"name": "MyApp", "executable": "C:\\Program Files\\MyApp\\MyApp.exe",
     "start": true, "stop": true}
  ],
  "named_commands": {
    "build": ["powershell", "-NoProfile", "-File", "scripts\\build.ps1"]
  },
  "mcp": {"transport": "stdio", "command": ["D:\\Dev\\MyWindowsApp\\mcp.exe"]}
}
~~~

This is a **logical proposal**, not a finalized wire schema. Normalize existing grant storage without silently changing what Windows approved. Keep per-folder Read/Sync separate from application metadata and project-wide Exec/MCP. Avoid treating the presence of an executable on the app list as proof that arbitrary Windows Exec has been safely sandboxed.

Creation and Save happen via **Windows user UI**. After each connection/restart the user **manually opens** a saved profile. Windows can edit, add or revoke folder/program grants while connected; effective permissions refresh immediately. Mac requests may ask for different capabilities; Windows's actual selections are authoritative, even if not a subset of Mac's request. Load should not start any executable or build.

### 4.2. Mac command blacklist profile (proposed logical JSON)

~~~json
{
  "schema_version": 1,
  "project_id": "my-windows-app",
  "entries": []
}
~~~

On **First-time Setup of a new project**, the **Mac app**, after user chooses a project ID, creates this empty file atomically if absent. An existing file is never reset or overwritten by setup, Profile Open or reconnect. An empty entries array is a required legitimate config state, not a missing config or a deny-all state.

Mac agent **config.list / config.current / config.load(existing-id)** only select and read already-existing local blacklist config; they must not create, edit, append, save, rename, delete or overwrite it as part of Open. A separate Blacklist Edit operation exists, but requires both an explicit user request and a **manual Mac UI Unlock**.

Lock state is kept across restart in Mac-owned global Bridge metadata keyed by project ID (or similarly small implementation that does not create a third per-project config file). Locked prevents writes via agent tools/UI Save/overwrite while allowing read/load and enforcement. Agent cannot invoke Unlock. This is an application workflow lock, not an OS-level sandbox claim.

### 4.3. File ownership and I/O rules

| Artifact | Owner and storage | Create / Open | Write authority |
|---|---|---|---|
| Permission Config | Windows App Data per project | Windows user creates/saves and manually opens | Windows user via Windows UI |
| Command Blacklist Config | Mac App Data per project | Mac app first setup creates empty; Mac user or agent opens existing | Mac user or agent after manual Unlock + user instruction |
| Pairing and global state | Per-host App Data, outside per-project profile files | Existing Bridge lifecycle | Existing Bridge/system rules |

Bridge File Sync is for source/script/DLL/test data. It **must not** copy either project config even if the same directory is otherwise granted. The feature should exclude direct canonical config file targets and the Mac blacklist source file. As Shell Exec is a powerful Windows capability, this is a **Bridge file-sync contract** rather than a claim that arbitrary Windows shell could not touch its own filesystem.

### 4.4. Config migration

Legacy Windows config.json projects, apps, state.json pairing and grants exist. Provide a Windows UI-assisted path:

1. Present existing projects and active grant scopes in UI as migration candidates (read-only preview).
2. User chooses project and confirms new permission profile's folders/capabilities/apps. Do not silently upgrade grant or auto-select a project on reconnect.
3. Save one Windows per-project profile; leave legacy config.json and state.json intact for rollback until new behavior is verified.
4. On Mac, user selects the same Project ID; first-time setup creates local empty blacklist. No legacy CAI command blacklist gets silently imported.
5. Both explicitly Open after connect. Only then permit project operation.

Migration errors must leave old source files intact; no automatic profile activation to "repair" missing config.

## 5. Project selection, handshake and readiness (M6)

The **transport is not the project session**. Network can connect before a profile is loaded. Pairing, chat and setup UI remain available to help the user configure profiles.

Proposed minimal project activation sequence:

1. Both apps running and paired; Windows connects as host; status = CONNECTED / PROJECT_NOT_READY.
2. Windows user creates/opens permission profile via Windows Settings. Host exposes **Project ID and selection metadata only** to Mac. The Windows UI remains the sole actor that Opens Windows permission file.
3. Mac UI allows initial setup/selecting the same Project ID; Mac first-time setup may create an empty file; Mac agent can thereafter load that existing local file read-only (or Mac user can open it).
4. Mac successfully validates file schema, Project ID and entries (empty array permitted); reports **local readiness metadata only**.
5. Both selected IDs equal and both local files loaded => PROJECT_READY. Otherwise report CONFIG_NOT_OPEN, CONFIG_NOT_FOUND, CONFIG_INVALID or CONFIG_MISMATCH. Do not dispatch project operations when not READY.
6. On exit/restart/reconnect: cached file contents remain, but per-session activation must be repeated. Reconnect is transport only. No spontaneous read/load on Windows and no auto-execution.

**Host operation guard:** Before Read/Sync/Exec/App/MCP request is dispatched, check correct active Project ID and both-side loaded states, then Windows grant for requested operation. Mac must not "assume empty" when blacklist load fails. Project-scoped tools list may be withheld or calls rejected while not READY; explicit error should explain exactly which side has not opened its profile. Management, pairing and setup may still function before READY.

No separate approval loop after one valid grant and matching activation. If a user changes the active project, invalidate old project readiness/tool capability snapshots; refuse stale calls rather than accidentally using previous project's grant.

## 6. Permission and Windows UI implementation (M7)

### 6.1. Requested vs Actual Granted

Current RespondRequest subset-check is incompatible with the Criteria. Refactor:

- Keep PendingRequest data (requested path/caps) for display only.
- Windows user can choose different capabilities independent of Mac request or add a folder proactively from UI.
- On Accept store **ActualGrant** from Windows UI, return actual capabilities to Mac, not merely "accepted".
- Request Read → Grant Read+Sync works; Request Read+Sync → Grant Read works; Reject stores nothing.
- Change/revoke grant updates active authorization. Abort in-flight affected operations where feasible and deny subsequent operations immediately.
- Keep **Windows path canonicalization** and protection against escaping granted roots. Do not add a second project-root/related_dirs eligibility gate after Windows has authorized a folder.

### 6.2. Folder & software UI on Windows

Under existing Settings, show two bounded groups:

**Granted Folders:** Pending requests (Accept/Reject/change caps), proactive Add Folder using Windows-native folder picker, Edit Read/Sync and Revoke/Remove. Path and effective capabilities must be visible. This is Windows user action; Mac UI cannot edit Windows grants.

**Applications:** Add Program with OS EXE picker, Edit/Remove, Start/Stop options and user permission to use them. Reuse current appcontrol behavior where appropriate; granted Exec may already run a shell that starts/stops a process. Don't duplicate shell authorization logic or create an implied strong sandbox.

**Profiles:** Save, Save As, Load/Open manually, Rename, Delete, selected Project ID and explicit "loaded" indicator. User Opens permission profile after each new connection/session, even though files persist.

### 6.3. Read/Sync behavior

Reuse existing fileops and path resolution, but change the effective target selection:

- Read/list/metadata/ranges allowed under actual Windows Read folder grant, regardless of legacy project declarations.
- On-demand Mac→Windows Sync ≤ 5 MB/request, temp→rename, SHA confirmation. Target is a canonical path under actual Windows Sync Grant, including folders outside project root.
- Refuse source or target that points to a Bridge-owned project config file; do not create config replicas on opposite hosts.
- Keep malicious path/symlink/junction checks and handle file lock/DLL replacement errors clearly.
- No continuous watch, background sync, deletion or Git replacement.

### 6.4. Shell/Exec and software restart

Existing executor can execute configured named commands. Add direct script/command tool behind Windows Actual Exec:

- Inputs: Project ID, command/argument representation, optional working directory, timeout/wait options. Avoid implicit user shell expansion if a structured argv form is safer; support an explicit shell invocation when required (PowerShell/cmd). Execute on **Windows**.
- Preserve stdout/stderr/exit code, bounded output, output poll for longer jobs, timeout and cancellation when Bridge Exit closes the session. Do not keep unattended jobs after Exit.
- Named eight command IDs remain convenient shortcuts, not the only permitted operations.
- Windows may open/close a configured EXE from App Control; shell can stop/restart applications for DLL rebuild (for example Close AutoCAD → build DLL → Sync → reopen → test via CAI MCP).
- The Bridge never implements AutoCAD COM/Revit API logic itself.

## 7. Generic Mac blacklist and MCP forwarding (M8)

### 7.1. Principle

Once the project is READY and Windows MCP Access is granted, **allow all tools the project's MCP server actually provides**, subject only to entries in the **loaded** Mac blacklist. No default rules for CAI, RAI, or newly created Windows software. An empty file creates **zero denied tools**, not "not configured".

Missing file, failed parse, unloaded file, or mismatched Project ID creates **NO project execution** — never an implicit empty blacklist.

### 7.2. Minimal matcher contract

Avoid a large rule engine. Support two progressively concrete rule types:

- **Tool-level deny:** exact project/tool identifier from the live MCP tool list. One rule can deny an entire named tool.
- **Argument-level deny (only if reliably inspectable):** exact tool identifier + explicitly selected JSON argument field/path + narrowly defined match on a string or value. The agent inspects the target project's actual MCP tool schema and tests an example of matching/nonmatching payload before proposing a rule.

Illustrative format (not a finalized API schema):

~~~json
{
  "schema_version": 1,
  "project_id": "my-windows-app",
  "entries": [
    {"kind": "tool", "tool": "my-windows-app__delete_project"},
    {"kind": "argument", "tool": "my-windows-app__run_action",
     "path": "action", "equals": "open_working_document"}
  ]
}
~~~

Match against stable actual project tool identity and parsed MCP arguments at **Mac bridge mcp** call boundary, *before forwarding* to the Windows host. Do not use an artificial global deny list of Windows commands. When the tool list changes, evaluate against the current stable project/tool mapping.

For complex nested CAD commands, real CAI schema might hide "OPEN" inside an opaque LISP or command string. Unless a **tested** matcher can reliably identify it, the UI/agent **must reject Save/Enable of that rule as unsupported**, retaining the previous valid config; alternative is an explicit whole-tool deny if user chooses. Never pretend a block is enforced on Windows by a Mac-only file the host never received. Future project-owned cooperating dispatchers require their own approval and implementation, not a hidden generic Bridge feature.

### 7.3. Mac editing UI and lock state

- Mac Settings shows project, file status (Loaded/Empty/Rules N/Missing/Invalid), command entries and manual **Lock/Unlock**.
- **Only Mac human UI** invokes Lock/Unlock; no agent-visible unlock command/API. Lock state persists in Mac global metadata.
- Agent may read project, recommend entries and edit blacklist only **after explicit user request and Mac manual Unlock**, using a separate update action with atomic write and reload. Open/read is always read-only.
- Locked rejects edit/append/overwrite/delete/Save through UI and agent routes; loading existing file and enforcing rules continues to work.
- Apply edits atomically and update the in-memory matcher; do not allow a stale unvalidated rule to become silently permissive. Show unsupported-rule error visibly.
- Mac UI cannot Grant/Edit/Revoke Windows folder/application permissions.

### 7.4. Example: CAI-specific policy is **opt-in**

If the user specifically wants to avoid accidentally opening a working DWG when testing CAI, the agent may **propose** blocking AutoCAD command OPEN variants and COM Documents.Open equivalents. User must request the rule and manually Unlock on Mac before agent edits. A rule only becomes effective if the actual CAI tool/argument path is identifiable and verified.

The default CAI project, like any other project, starts with an **empty** file and no command restrictions. If the user never asks, leave it empty. Do not force AutoCAD restrictions onto a new Windows desktop application.

## 8. UI behavior and diagnostics (M9)

Use existing embedded HTML/JS/SSE shell; chat remains home view and Settings contains config/grants/blacklist.

| UI / runtime state | Host UI | Mac UI / agent |
|---|---|---|
| Connected, no profile opened | Prompt to manually Open permission profile | Show pending project selection, no project tool use |
| Windows profile loaded, Mac file absent | Display active project ID; don't auto-create counterpart | CONFIG_NOT_FOUND; only first-time Mac Setup UI may create new empty file |
| Wrong Project ID | Show current host project | CONFIG_MISMATCH, no project dispatch |
| Both config files loaded, empty blacklist | Grant/permission state normal | PROJECT_READY; all project tools usable subject to grants |
| Loaded blacklist with entries | No transfer of blacklist body | Entries enforced locally before forward |
| Mac blacklist Locked | No effect on folder/program grants | List visible; edit disabled; human can Unlock |
| Windows Grant revoked | Host denies and refreshes effective grants | Show updated capability error, stop affected activity |
| Unsupported rule | No fake Windows enforcement | Reject rule edit; keep previous valid file and show reason |

Keep activity labels without leaking arguments/file content. Preserve chat script Copy and no auto-run behavior, Claude/Codex provider registration, single active MCP runtime lock, and tray state indicators.

## 9. Suggested API and protocol deltas

These are **technical proposal names**, not existing validated endpoints. Prefer extending current callbacks over new transport frameworks:

| Surface | Logical operation | Critical contract |
|---|---|---|
| Windows UI | profile.list / profile.open / profile.save / profile.delete | Manual user Open; Windows authoritative file; no remote Mac create |
| Windows UI | permission.request / grant.add / grant.edit / grant.revoke | Actual capabilities independent from Request |
| Windows UI | application.add / update / remove | Native EXE selection and rights |
| Mac local API | config.list / config.current / config.load | Agent existing-only read, no file creation or write |
| Mac UI only | blacklist.lock / blacklist.unlock | Human-triggered UI, no MCP-agent unlock tool |
| Mac local edit API | blacklist.entries.update | Requires unlocked state + user-directed flow; atomically validate and write |
| Mac mcp dispatch | project.tools.list / project.tools.call | READY check + exact project/tool identity + blacklist before forwarding |
| LAN transport | project.selection-status | Project ID and readiness/capability metadata only |
| Windows host | shell.exec and existing run-command aliases | Project ready + Windows actual Exec Grant; bounded output |
| Mac/Windows | project.status | Explain NOT_OPEN/MISSING/MISMATCH/READY without config content |

Use typed request/response and clear errors (for example ERR_COMMAND_BLACKLISTED, CONFIG_NOT_OPEN, CONFIG_NOT_FOUND, CONFIG_INVALID, CONFIG_MISMATCH, ERR_RULE_UNSUPPORTED, ERR_PERMISSION). No invented extra approval gate.

## 10. Migration, failure handling and compatibility

**Migration safety:** Old global host config and app grants may coexist during migration. Do not silently import/upgrade Windows permissions or overwrite a Mac blacklist; preserve pairing and rollback path. Explicit first-time setup creates only the Mac empty file for the new project.

**Readiness atomicity:** Snapshot Project ID and loaded-state when dispatching a request; on profile change/mismatch, deny stale project calls. A valid empty blacklist must be differentiated from a null/missing result at every layer. Editing a loaded unlocked blacklist should atomically replace in-memory matcher only after validation/writing succeeds; failure leaves old matcher intact.

**Locked-state enforcement:** App-level UI/MCP API guard, not an unbreakable OS lock. No agent-facing Unlock. A user-granted arbitrary Windows Shell capability does not magically grant Mac-side editing permissions.

**No hidden config synchronization:** Windows receives no Mac blacklist JSON; Mac receives no Windows permission JSON. Project ID, readiness, actual permissions and results are allowed as protocol metadata. No background syncing, even if File Sync is granted.

**Unexpected disconnection/Exit:** Existing network recovery must not auto-activate project config. User-attended Exit cancels Bridge-launched operations per Criteria; distinguish unexpected Wi-Fi break from an explicit app Exit and report real cancellation status, never promise a process is stopped without evidence.

**Failure evidence:** If platform-specific APIs, pickers, MCP transports or app command schemas cannot be tested on the appropriate host, record UNVERIFIED and a concrete test requirement. Do not claim Go unit tests replace a live two-machine result.

## 11. Implementation work packs, precise order

| Work pack | Milestone | Key source surfaces (target, not yet modified) | Observable outcome |
|---|---|---|---|
| Identity and local profiles | M6 | internal/store, internal/protocol, cmd/bridge/main.go | Windows manual profile open; Mac setup empty file/agent read-only load; correct ID handshake |
| Actual grants and Shell | M7 | internal/permissions, internal/fileops, internal/executor, cmd/bridge/main.go | Independent grant, non-project Sync, shell build/restart |
| Mac blacklist and dispatch | M8 | internal/agentmcp/server.go, Mac local store, MCP-forward path | Empty→all tools, missing→deny all project operations, locked edit denied |
| UI wiring | M9 | internal/ui/web.go and embed/index.html | Separate Windows grants vs Mac blacklist manual lock |
| Verification and migration docs | M10 | existing *_test.go, native apps, docs/reports, install docs | 40 Criteria scenarios measured and reported honestly |

Do not split the project into unnecessary infrastructure/services. Plan technical order can run independent test substeps in parallel; consequential writes/reviews are sequential by dependency.

## 12. Tests and evidence matrix

| Test group | Expected coverage |
|---|---|
| Config lifecycle | New Mac setup creates empty file; opening existing doesn't write; no overwrite/reset; Windows manual Open; paired Project ID; restart/reconnect |
| Missing config guard | Absent, unreadable, malformed, not-loaded, mismatch; every project Read/Sync/Exec/App/MCP denies |
| Grant request/actual | Windows proactively grants folder; requested≠actual both directions; edits/revokes; denied capability |
| Fileops | Allowed outside project root; forbidden outside grant; <=5 MB accepted, >5 MB denied; no per-project config file sync |
| Shell and app | Named shortcut and generic granted Windows shell; output/timeout/cancel; app graceful/explicit stop/restart |
| MCP generic | Real non-CAD MCP server all tools present with empty loaded blacklist; configured tool deny; dynamic tools/list changes |
| Blacklist matcher | Matching and nonmatching arguments; unsupported nested rule rejected without changing valid file |
| Lock/edit | Human Mac Lock/Unlock, agent edit only unlocked + user request, locked save/reload bypass denied; persists |
| UI | Windows folder/EXE pickers, manual profile Open; Mac blacklist list and Lock/Unlock; exact error messages and activity privacy |
| LAN | Pair, reconnect, project mismatch, capability update, chat Copy, Exit behavior |
| CAI sample | Windows shell rebuild/restart; CAI MCP loads test tool; AutoCAD OPEN/COM rule tested only when explicitly requested and matchable |
| Native acceptance | Both physical host OSes; all Criteria §11 #1–40 mapped to PASS/FAIL/UNVERIFIED with source revision |

**Mandatory negative proof:** A Mac project with a blacklist file that exists but **has not been loaded** must **not** get MCP tools just because the Windows permission file says MCP allowed. Conversely, a loaded *empty* blacklist must not deny project tools. These are distinct cases, not interchangeable.

## 13. Open technical facts to validate during implementation

1. What exact tool names and structured arguments do actual new target Windows MCP servers expose? No generic parser can promise to inspect arbitrary application-specific command languages.
2. What is the reliable native UI path for Windows folder/EXE pickers in the current lightweight embedded-browser UI, and how are paths returned to the host process without arbitrary Web permission?
3. How does the existing launcher handle Mac app first-time setup while the MCP process may already be starting? Ensure the app creates an empty file **before** agent existing-only Open.
4. How to migrate existing Windows global permissions to per-project Actual Grants without implicit escalation? Require a Windows UI preview/confirmation.
5. Does the current long-command cancellation mechanism stop launched descendants on both native host lifecycle paths? Validate with real host evidence.
6. Are the existing Claude/Codex MCP registration, native tray, Windows scaling and local network discovery behaviors working after the new readiness gating? Test rather than assume.

These are verification work items, **not** permission to expand scope or invent blocking policies. If facts remain unknown, report UNVERIFIED.

## 14. Non-goals and approval

No new GSA Project, Job or Memory. No CAI/RAI repository edits, no hardcoded default AutoCAD restrictions, no OS sandbox, no generic remote desktop, no multiuser management, no hidden config sync, no persistent background runner, no chat auto-execution.

This Technical Proposal follows a reviewed Plan and updated Milestones. It is the **new implementation guidance** for future separately approved product work, not an implementation approval by itself. The old docs/TECHNICAL_PROPOSAL.md remains intact as legacy historical evidence.
