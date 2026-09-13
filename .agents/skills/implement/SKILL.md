---
name: implement
description: "Implement a piece of work based on a spec or set of tickets."
disable-model-invocation: true
---

Implement the work described by the user in the spec or tickets.

Use /tdd for behavior changes where useful, and whenever the user requests test-first work.

Run affected checks and those required by the repo or ticket. Broaden testing when failures or the change's impact warrant it. Fix failures caused by this change and rerun affected checks; repeat passing checks only after further changes or new evidence of a problem.

After implementation and verification, use /code-review. Resolve in-scope findings and verify any fixes before declaring the work complete.

Commit your work to the current branch.
