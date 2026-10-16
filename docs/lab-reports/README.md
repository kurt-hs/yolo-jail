---
title: "Lab reports"
date: 2026-10-16
status: current
tags: [testing, process]
summary: "Where a session files what it observed, ran and could not settle, for another agent to pick up and decide the way forward."
---

# Lab reports

A **lab report** *(a term coined here, 2026-10-16)* is a dated record of one debugging or
testing session: what was observed, what was run, what was fixed and committed, and what was
left open. It is a hand-off and not a plan. It decides nothing, so the agent that picks it up
chooses the way forward, then moves what survives into the right home (a fix, a
[`../plans/`](../plans) row, a design doc's open question) and deletes the report.
`git log --follow -- <path>` recovers a deleted one.

- **Name:** `YYYY-MM-DD-<short-slug>.md`, dated the day of the session.
- **Shape:** Observed, Fixed and committed, Open (one section per thread, each ending in
  the decision it needs), and Evidence (commands and their output, with the commit they ran
  against).
- **Claims are checked before they are written**, as everywhere in this corpus: a commit, a
  test name or a `file:line` is named only once it has been looked up.
