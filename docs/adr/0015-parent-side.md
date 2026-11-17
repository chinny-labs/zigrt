# 15. The parent side: notifications, overrides and assigned drills

Date: 2026-11-17 · Status: accepted

## Context

Plan section 6.3 and boards P2 to P6 describe what a parent sees and changes. Building them needed a few choices the plan leaves open, and one where the board and the earlier build disagreed.

## Decision

- **Notifications are per parent and deduplicated.** Each piece of news is one row per parent, with a dedupe key (a goal met is news once a day; a lockout once per lock), so a retry or a catch-up job never repeats it, and one parent reading does not mark another's.
- **The news:** a student locked out, a lesson stopped twice (with the blocks being mended under it), the daily goal met, a streak of two days or more lost, a tier or face finished, and the lessons updated to a newer pack.
- **Streaks lost are found by a day-rollover job** that runs once for each household-local day that has ended, catching up at most a month after downtime and starting from the present on its first run.
- **A newer pack in the content folder still runs automatically at start** (ADR 12), where board P6 had it wait for a parent; parents are told instead.
- **Overrides are events.** Marking a block placed or not is a `parent_override` mastery event, so history and rebuilds keep it. Running the placement check again puts a paused placement test at the top of the student's list.
- **Assigned drills** are stored as settings; the student's set is made when they start it. A drill is done when finished, a worksheet when made.
- **Deleting a student** needs their name typed; it removes all their rows, the one exception to append-only (ADR 3).
- **A week's goal** is the daily goal on each day of the week (Monday to Sunday) that is not a rest day.
- **Tasks left on screen for a week** are set aside by housekeeping: a lesson or placement test keeps its place, anything else is abandoned.
- Email (weekly summary, password reset) and API tokens wait for phase 6.

## Consequences

- The parent's bell is cheap to read and never floods.
- Every parent action that changes learning is in the event log or the audit log.
