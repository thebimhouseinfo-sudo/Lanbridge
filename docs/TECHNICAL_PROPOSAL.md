# Technical Proposal — Coder-owned, whole Plan

Status: NOT_CONFIGURED. Coder writes this after Plan, milestones and Coder/Tester specialization (AGENTS.md main path, step 5).
Critic CR: NOT_REVIEWED

**Descriptive only.** No source files, stubs or test files are created at this stage. The first proposal covers **every milestone and Task in the Plan**, written bottom-up from Tasks so Critic can review it top-down. Later revisions cover only materially changed scope and go back to Critic.

## Proposal by milestone

### MILESTONE-<ID> — <goal>

#### TASK-<ID> — <observable result>
- **Plan / Criteria / decision refs:** <PLAN-ID, CRITERIA IDs, linked DEC files under docs/discussions/ — the design follows them as written>
- **Files:** <CREATE / MODIFY / REUSE_AS_IS / REMOVE with paths; VERIFIED existing vs PROPOSED>
- **Functions / interfaces:** <names, inputs/outputs, callers and consumers>
- **Shared dependencies / state / host constraints:** <verified facts; UNVERIFIED marked>
- **Tests:** <unit / integration / real host; positive, negative, edge>
- **Exclusions:** <what this Task deliberately does not build>

## Cross-milestone view
<Shared modules and interfaces, duplicated ownership avoided, ordering, risks across milestones.>

## Critic CR record
<Each round: verdict (PASS / CHANGES_REQUIRED / HUMAN_ESCALATION), findings with evidence, how the proposal changed. When Critic returns PASS, set the `Critic CR:` line above to `PASS`.>
