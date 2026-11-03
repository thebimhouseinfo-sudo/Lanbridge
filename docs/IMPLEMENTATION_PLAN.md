# Implementation Plan — LAN Test Bridge, Criteria-aligned v1.3

Status: REVISED — exact-review verdict recorded separately in docs/reports/ (2026-11-03)
Source of truth: [docs/CRITERIA.md](CRITERIA.md), Git blob `8a9e88c0bb3dca430c97fc57cc74b9406426390b`.
Baseline: existing v1.0 implementation and legacy Milestones 1–5; see [docs/ARCHITECTURE.md](ARCHITECTURE.md) and current Go code. No product source edits authorized by this document.
GSA execution mode: DIRECT_AGENT/ephemeral; **do not create any GSA Project, Job or Project Memory**. Reviewer feedback is advisory until independently verified against the actual Git revision.

## GOAL

Keep LAN Test Bridge a lightweight, generic, attended Mac↔Windows development bridge for **any** Windows software repository. Preserve pairing, Read/Sync, Shell/Build, MCP forwarding, Start/Stop, chat and minimal native UI; adapt the existing implementation to the **approved Criteria** without expanding into a security sandbox, unattended runner, CAD controller or CI/CD service.

Observable end state: Windows controls actual folder/program grants in Windows UI; Mac controls a locally stored, opt-in command blacklist in Mac UI; each project has **one Windows permission file and one Mac blacklist file** and these files never sync; Windows user opens its existing profile manually while Mac agent may read-only open its existing profile; both must load and match a stable Project ID before operations. First-time Mac setup creates an **empty blacklist file**, not default restrictions; a missing/unloaded blacklist **blocks** project operations; an empty but loaded blacklist **allows all project MCP tools** subject to Windows grant. A Mac user alone unlocks/locks blacklist editing. Shell Exec supports real build/restart workflows, and no software-specific commands are hardcoded as Bridge restrictions. Acceptance requires real evidence, not only document/code review.

### Scope and non-goals

- **In scope:** retrofit state/config architecture, Windows grants and UI, Mac blacklist and UI, profile activation and matching, generic command/operation forwarding, Exec shell, opt-in blacklist matching, tests, migration of existing host configuration, diagnostics, installer docs and real-host acceptance.
- **Out of scope:** new GSA project, changes to CAI/RAI repositories, default AutoCAD blacklist, general security sandbox, remote desktop, overnight runner, background file sync, cross-host config replication, large-file sync, and refactoring unrelated modules.
- **Trust limit:** Windows grant remains authoritative for host operations. Mac blacklist is a user-configurable workflow guard in the Mac dispatch path, not a cryptographic security boundary against arbitrary shell/program behavior. A nested application-specific command can only be blocked when a supported matcher can identify it; document/test unsupported cases instead of claiming universal interception.
- **Workflow classification:** UI_FIRST; reuse existing embedded HTML/SSE UI shell and Criteria-defined interactions. Do not add framework or redesigned navigation merely for the new controls.

## Existing baseline and change impact

Observed source surfaces (2026-11-03):

| Existing source | Relevant behavior / gap |
|---|---|
| `internal/store/config.go` | One Windows `config.json` holds multiple projects, `related_dirs`, fixed commands, apps; no paired per-project config. |
| `internal/store/state.go` | Windows `state.json` holds pairing plus grants; keep pairing/global state separate from new two-per-project files. |
| `internal/permissions/gate.go` | Host `RespondRequest` presently rejects capabilities not originally requested; must accept Windows-adjusted actual grants. |
| `internal/fileops/sync.go`, `internal/permissions/resolve.go` | Audit scope and remove project-root-only Sync restriction while retaining actual folder grants and atomic small-file write. |
| `internal/executor/executor.go` | Project command execution is fixed/registry-based; add granted shell/script operations without losing bounded output/cancel/timeout. |
| `internal/agentmcp/server.go`, `internal/mcpfwd/fwd.go` | Mac MCP gateway and Windows project MCP forwarder exist; Mac local blacklist/readiness must be checked **before forwarding**. |
| `internal/ui/web.go`, `internal/ui/embed/index.html` | Reuse Windows/Mac UI and SSE, add separate controls and display actual grants/readiness/lock. |
| `cmd/bridge/main.go` | Connect all runtime state and mutations. Preserve original pairing, chat, notifications and single-agent lock. |

**Precedence:** Criteria > reviewed Plan > revised Milestones > new Technical Proposal > legacy Technical Proposal/Architecture > current code. Treat legacy code as implementation evidence, not authority for new behavior. Do not silently change unrelated features or infer implementation PASS from current status "CODE COMPLETE."

### Retained GOALMAP

Keep original goal names and their intended outcomes; update acceptance to new Criteria:

- **GOAL-1: Project scaffold và build system** — keep lightweight Go binary/build.
- **GOAL-2: Transport — TLS connection, discovery, pairing** — retain LAN/pairing/reconnect; reconnect must not auto-open project configs.
- **GOAL-3: Folder permissions** — Windows actual grants independent of Mac request; UI Add/Edit/Revoke folder/program.
- **GOAL-4: Read** — within Windows-approved folders, including logs and ranges.
- **GOAL-5: Sync (Mac → Windows)** — on-demand ≤5 MB/request into any Windows-granted folder; never sync the two config files.
- **GOAL-6: Fixed dev commands** — preserve named build-command convenience and **also** support arbitrary Windows shell/script via granted Exec.
- **GOAL-7: MCP forwarding** — expose all project tools after readiness; apply only loaded explicit blacklist entries on Mac; no default CAD restrictions.
- **GOAL-8: Software Start/Stop** — Windows UI grants software rights; Shell can restart apps for rebuild; app list is not a pretend barrier when Exec is granted.
- **GOAL-9: Chat** — two-way text/script snippets, manual copy, no auto-execute.
- **GOAL-10: UI** — chat remains main surface; Windows Settings for grants/apps, Mac Settings for blacklist/lock and activity.
- **GOAL-11: Install scripts** — preserve installers; add local profile setup instructions.
- **GOAL-12: Giao diện cho Mac agent** — preserve Claude/Codex single MCP registration and lock; add existing-config-only read/load and separate unlocked blacklist edit.
- **GOAL-13: Paired project identity** — local Windows permission config + local Mac blacklist, project ID match, no cross-machine config sync.
- **GOAL-14: First-time empty blacklist** — app initializes file with 0 entries; open never creates/edits; missing/unloaded fails closed for project operations.
- **GOAL-15: User-owned blacklist editing** — Mac manual Lock/Unlock; agent reads repo and edits only when explicitly requested and unlocked; lock persists.
- **GOAL-16: Generic Windows development** — no CAI/RAI-specific restrictions; optional CAI OPEN/COM example only when user explicitly adds rule.
- **GOAL-17: Observable host permissions** — actual grant reporting, revocation, project mismatch/locked errors and no extra authorization gates.
- **GOAL-18: Real-host test evidence** — Mac/Windows end-to-end, unsupported matcher cases marked UNVERIFIED, no fictitious PASS.

## Phase 1 — Local per-project configuration and identity (Milestone 6)

### GOAL

Implement two distinct project-local configuration files and a reliable, explicit project activation handshake without transferring either file's contents across the LAN (GOAL-2, GOAL-12–14, GOAL-17).

Tasks (finite work pack M6 / local profiles):
1. Define one stable `project_id` and minimal schema/version for **Windows** local `permissions.json` and **Mac** local `command-blacklist.json`; keep global pairing/TLS and common settings independent. Do not create a third per-project config file. Windows's project-setup UI allocates/shows the stable ID; after connecting, Mac UI can select the ID received as **metadata only** (or user can enter the same ID), then its first-time Setup creates the corresponding empty local file. Mac agent cannot create IDs/files by pretending to Open.
2. Define project setup/UI ownership: Windows user creates/saves/selects Permission Config through Windows UI; **first-time Mac app setup** creates empty blacklist (0 entries); existing files never overwritten by setup. Agent `config.list/current/load` only read existing Mac configs, never create/edit/save as side effect.
3. Implement explicit Windows manual Open and Mac manual or agent read-only Open; project-scoped operations fail with specific `CONFIG_NOT_OPEN`, `CONFIG_NOT_FOUND`, `CONFIG_INVALID` or `CONFIG_MISMATCH` when appropriate.
4. Send only the stable Project ID and readiness state across transport (not file bytes, hash or blacklist entries); `PROJECT_READY` requires **both** configs loaded, matching ID and successful Mac blacklist parse (even when empty). Network reconnect does not silently open profiles.
5. Plan minimal migration from Windows legacy `config.json` and `state.json`: preserve old data/pairing, make user-visible migration to per-project Windows files, no auto-activation or accidental permission escalation; retain ability to roll back without corrupting old files.

Verification:
- Fresh project setup produces a Mac blacklist file with `[]` and correct ID; existing list not overwritten.
- Missing/corrupt/unloaded Mac blacklist always blocks project operations, **never** treated as `[]`; loaded-empty passes.
- Windows config only manually opened; Mac agent auto-opens a previously existing correct local config read-only; unequal/missing IDs are not ready.
- Reconnect and restart require explicit profile activation; both files remain local and are never copied through sync.

## Phase 2 — Windows actual grants, File Read/Sync and Shell Exec (Milestone 7)

### GOAL

Windows user retains independent authority over folder/program permissions, while Mac can use the exact approved capability set for genuine development operations (GOAL-3–6, GOAL-8, GOAL-17).

Tasks (finite work pack M7 / host grants and command execution):
1. Rework pending request + actual grant: Windows may approve, reject, **grant fewer/more/different capabilities** than requested, and Add Folder without Mac request; responses show effective grant. Edit/Revoke applies during active sessions. No dependency on `projects`/`related_dirs` for Sync.
2. Windows UI: Pending Requests; Granted Folders with Add (OS folder chooser), Edit/Revoke; Programs with EXE picker and Start/Stop rights; Save/Save As/Load/Rename/Delete relevant Windows profiles. Windows must manually Open saved profile after connection.
3. Gate Read and on-demand Sync (≤5 MB/request, atomic write) against Windows's **actual folder grants**; never allow Mac to sync either project config file, including direct targeting by explicit path. Retain clear traversal/junction/path errors.
4. Add Windows Shell/Exec for permitted working directory, build/test scripts and DLL workflows; preserve output caps, exit code/stderr, timeouts, cancellation on Bridge Exit, no background job persistence. Named eight commands stay supported as shortcuts. No redundant shell-command whitelist.
5. Reuse app Start/Stop controls and actual Windows approval; clarify in UI/docs that granted arbitrary Shell/Exec can also start/stop software, so app checkboxes are convenience controls not independent sandbox claims.

Verification:
- Request Read→Grant Read+Sync works; request Read+Sync→Grant Read works; self-added Windows folder works; revoke effective immediately.
- Sync succeeds outside `projects/related_dirs` if and only if granted, still rejects >5 MB; no config-file replication.
- Windows shell build/restart/app tests return real output; legacy named commands still work; denied Exec behaves clearly.
- Avoid changing fixed script executor behavior outside necessary scope; run existing regression.

## Phase 3 — Mac blacklist, generic MCP dispatch and editing lock (Milestone 8)

### GOAL

Preserve all project tools by default while ensuring a **loaded, project-specific** blacklist is not bypassed by omission and can be edited only under user's manual Mac Unlock (GOAL-7, GOAL-12, GOAL-14–16).

Tasks (finite work pack M8 / Mac blacklist dispatch):
1. Mac local blacklist store: `project_id`, `entries: []`, schema version, lock state (can reside in Mac global metadata to avoid a third project file); atomic updates, exact existing-file-only agent Load and error on malformed/missing.
2. Mac UI shows current project, count/list of entries and manual Lock/Unlock. Lock persists through restart and blocks edits via Mac UI/agent API/Save/overwrite; **view/load and dispatch remain permitted**. Agent cannot auto-unlock. Agent may edit only **after explicit user request and manual Unlock**, separate from `config.load`.
3. Define small generic matching contract from actual project MCP tool schemas: exact tool names plus simple, explicit argument selectors where sufficient. Apply locally at Mac `bridge mcp` dispatch **before host MCP tools/call**; allow all when loaded blacklist is empty. Project tools discovered dynamically, no default tool-specific restrictions.
4. For nested AutoCAD `OPEN`/COM examples, inspect the real CAI MCP tool schema before promising coverage. If a configured rule cannot be reliably matched on the Mac side, **reject enabling/saving that rule with a concrete unsupported-rule error and preserve the previous valid blacklist**; never accept a rule and silently allow forbidden calls. Record UNSUPPORTED/UNVERIFIED for the specific nested case; alternatively the user may choose a whole-tool deny rule. A cooperating app-specific dispatcher requires separate authorization; do not sync Mac config to Windows or silently claim blocked.
5. Keep generic works-with-any-repo behavior and MCP tools/list_changed. A read/load failure or project mismatch blocks forwarding regardless of Windows MCP grant; report error and don't pretend empty list. No ad hoc parsing of arbitrary shell scripts or CAD add-ins in Bridge.

Verification:
- Empty loaded list + Windows MCP grant exposes/invokes every project tool; the same project with a configured rule blocks only the matching call and returns `ERR_COMMAND_BLACKLISTED`.
- Missing/unloaded/corrupt file blocks all project operations; inability to parse a nested matcher is visible as evidence gap, not a false PASS.
- Agent Open/Load does not mutate bytes; locked edit rejected (including API overwrite); manually unlock + user-directed add/remove succeeds; lock persists and is independent of Windows Grant.
- Test with non-CAD dummy Windows MCP tool set to prove no CAI/RAI hardcoding.

## Phase 4 — UI integration, profile workflows and observability (Milestone 9)

### GOAL

Make the separate Windows and Mac controls usable from the existing lightweight UI and expose every necessary readiness/permission/lock state without a new application framework (GOAL-9–12, GOAL-17).

Tasks (finite work pack M9 / existing UI extensions):
1. Windows Settings: manual Open Permission Config, named profiles, request/actual grants, Add/Edit/Revoke Folder, Add/Edit/Revoke Program/Start/Stop. Use native picker routes where available and test actual Windows UI behavior.
2. Mac Settings: current Project ID, project blacklist file, Loaded/Missing/Invalid, list/edit controls and **manual Lock/Unlock**. Agent opens only local existing file; user can manage creation during first-time Setup. No Windows grant editing on Mac.
3. Show connection vs `PROJECT_READY` separately; failed project matching, missing blacklist, revoked grant, locked edit and config-not-open each get concrete actionable errors. No auto-start or auto-build on Load.
4. Preserve chat as main screen, Settings navigation, MCP activity label redaction, Claude/Codex provider switching and existing one-agent lock; do not add a scheduler, central config replication or a second chat system.

Verification:
- Native Windows and Mac UI scenario tests cover Save/Open/Load/Lock/unlock, correct machine ownership, profile switching and errors.
- No file content, shell arguments or logs exposed in routine MCP activity labels; manual chat Copy intact.
- Component/UI regression and session restart proof.

## Phase 5 — Compatibility, tests and release evidence (Milestone 10)

### GOAL

Demonstrate actual end-to-end completion across **one generic non-CAD Windows project** and the CAI example without claiming native results where no native runner exists (GOAL-1, GOAL-2, GOAL-11, GOAL-18).

Tasks (finite work pack M10 / acceptance and regression):
1. Add independent Go unit/integration tests by subsystem: config migration, project ID matching, permission edit/grant/revoke, small-file Sync + prohibited config target, shell Exec, command output/cancellation, Mac blacklist/missing/load/lock semantics, dynamic MCP forwarding and no-default restrictions.
2. Run `go test ./...`, `go vet ./...` and existing Makefile build targets on authorized matching hosts. Inspect runner logs on failure; report exact performed vs unavailable tests.
3. Native Mac app: real menu bar, first setup, empty file, Mac manual Lock/Unlock, agent read-only load, Claude/Codex registration. Native Windows: folder/EXE pickers, UI profile Open, actual grants, Shell Exec, app restart.
4. Two-machine attended LAN scenarios: pair/reconnect, manual Windows profile Open, agent Mac blacklist load, matching ID→READY; mismatch/missing→BLOCK; all MCP tools with empty blacklist; one configured deny; proper revocation; file Sync restrictions; DLL build/restart, logs and chat.
5. Update `docs/reports/` with per-scenario PASS/FAIL/UNVERIFIED, commit SHA/platform, and observed constraints. Update README and installer docs only as required by implemented behavior. Don't merge/release on simulated-only PASS.

Verification: full Criteria §11 (#1–40) mapped to concrete tests, independent review of scope and regression; explicit unresolved native/hardware gaps if any. **Do not infer PASS from the legacy "CODE COMPLETE" label.**

## Planning/review and implementation gates

1. Planner edits this Plan first, obtains readback, Reviewer evaluates **the exact Plan revision** against approved Criteria and observed code. On CHANGES_REQUIRED, repair and re-review until PASS or evidenced blocker; maintain goal anchors and unchanged approved scope.
2. Only **after Plan review PASS** revise `docs/MILESTONES.md` for the delta scope, retaining legacy Milestones 1–5 as historical code-complete-but-native-unverified baseline. Do not silently mark new milestones complete.
3. Then **create a NEW** technical proposal (e.g. `docs/TECHNICAL_PROPOSAL_V2.md`), preserving the old `TECHNICAL_PROPOSAL.md` as legacy record. The new document must ground implementation surfaces in observed code and trace each new milestone to acceptance evidence, without assuming new technical behavior has been tested.
4. Product implementation (Go/source/UI) is a **separate subsequent approval**, not authorized by this planning/document update. No GSA Project/Job/Memory creation; only GitHub repository planning documents are modified here.

## Review checklist for each Plan revision

- [ ] Original GOAL-1–12 intents preserved; new GOAL-13–18 map every Criteria delta.
- [ ] Every Phase has an immediately following `### GOAL`; no phase task absent an acceptance condition.
- [ ] Windows permission file / Mac blacklist file separate, local, no config sync; Project ID match.
- [ ] Windows creates/displays stable ID and Mac selects matching metadata during first setup; First-time empty file vs missing file; missing/unloaded never bypassed; agent Open does not write.
- [ ] Windows manual Open; Mac agent auto-load existing file only; no background auto-activation.
- [ ] Windows requested != actual grants; Windows UI manages folders/apps; Mac UI blacklist lock.
- [ ] Generic all-tools default; opt-in rules only; no hardcoded AutoCAD blacklist.
- [ ] Shell Exec + named commands coexist; no fake sandbox claim.
- [ ] Blacklist matching on Mac where source exists; unsupported rules fail validation rather than being saved as fake protection; cannot claim nested Windows-side blocking without real mechanism.
- [ ] Real-host and negative/regression scenarios with evidence; legacy code-complete status not used as evidence for new scope.
