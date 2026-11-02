# Discussions — decisions settled with Human

Written by the Secretary in Discussion mode (`.agents/skills/record-decisions/SKILL.md`). Each discussion gets its own folder:

```
docs/discussions/<YYYY-MM-DD>-<topic>/
  INDEX.md              topic, status OPEN/CLOSED, list of DEC records and their status, UNRESOLVED points
  DEC-001-<slug>.md     one settled point, exactly as Human confirmed it in chat
```

A DEC file holds the **full confirmed solution**, not a summary. ACCEPTED records are never edited in place; a change is a new DEC that supersedes the old one.

Plan items and Tasks link the DEC files they implement (`Decisions:`). Coder implements the linked decision as written and asks Human instead of inventing a different solution.
