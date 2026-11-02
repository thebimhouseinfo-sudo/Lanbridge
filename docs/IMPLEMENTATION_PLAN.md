# Implementation Plan — Planner-owned draft

Status: NOT_CONFIGURED. Requires confirmed `docs/CRITERIA.md`. Planner sets this to `Status: ACTIVE` when the Plan is written.

**GOALMAP is inside this Plan**, not in CRITERIA. Each Plan item must **start with GOAL**: what tangible behavior or result does the code accomplish for the user? A technology task such as “write persistence.py” is not a GOAL.

## GOALMAP

| Goal ID | Observable result | Approved CRITERIA IDs | Dependencies | Plan section | State |
|---|---|---|---|---|---|
| <!-- Planner populates after approved CRITERIA; never assume G-1 --> | | | | | NOT_CONFIGURED |

## Plan items

Planner reads and links full current accepted Decision sources before writing Goals/Tasks. A short summary is not a replacement for an operational solution. Decisions settled in Discussion mode live in `docs/discussions/<folder>/DEC-*.md`; link each one from the Plan item and Task it governs so work there follows it instead of re-thinking it. Planning does not require a discussion to exist.

### PLAN-<ID> — GOAL: <observable result to achieve>

- **CRITERIA references:** <approved requirement IDs>
- **Accepted Decision references (when applicable):** `Decisions:` <paths such as docs/discussions/<folder>/DEC-001-<slug>.md — links to FULL active Human-chosen domain/operational solution; include supersession status and source provenance, not a lossy paraphrase>
- **Decision coverage/conflicts/feasibility:** <which accepted steps each Task implements; contradictory active decisions; verified vs unverified host dependencies>
- **Current state / gap:** <verified evidence; existing projects>
- **Implementation:** <specific bounded technical approach from approved ARCHITECTURE>
- **Dependencies:** <goal IDs required first>
- **Revised-scope impact (only when approved Plan changes):** <replaced Phase or whole Plan; completed work eligible for evidence-based reuse; all unfinished old Milestones/Jobs/Tasks RETIRED after actual settlement; new IDs and unchanged work identified; no automatic Git deletion>
- **Acceptance:** <observable product behavior and relevant negative cases>
- **Verification:** <test approach, expected real-host evidence and regression coverage>
- **Limitations / risk / non-goals:** <approved tradeoffs>

After writing this Plan, Planner continues to milestones without a blanket approval stop; Human is asked only about unsettled professional/domain solutions or Small Function → Big Solution cases (see AGENTS.md main path). Approved CRITERIA changes require independent Advisor consultation at the prescribed checkpoint. Do not claim authenticated gate approval when only normal chat/manual coordination exists.
