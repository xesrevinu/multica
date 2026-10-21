---
name: plan-split
description: Break an epic into independently verifiable, staged sub-issues the orchestrator can schedule. Use for lead planning runs.
license: MIT
compatibility: multica
metadata:
  source: multica orchestrator (pstack swarm / architect)
---

# Plan split

1. One comment first: outcome, out of scope, the stages and why they are ordered.
2. One sub-issue per independently verifiable unit (a reviewer can check it on
   its own sha). Prefer more small issues over one large one.
3. Each body has: Goal, Acceptance criteria (observable), Files/area,
   Check command, Out of scope.
4. Parallel issues in one stage must touch disjoint files. Shared files go in
   separate stages.
5. Create: `multica issue create --parent KEY --project <project> --status todo
   --stage N --title "type(scope): subject" --description-file <file>`.
   No assignee: the orchestrator assigns owners and starts stage N+1 only when
   stage N is done. Never `backlog`: backlog means not planned and the
   orchestrator does not touch it.
6. Cross-stage needs: metadata `deps=KEY1,KEY2` on the dependent issue.
7. Human sign-off needed (GUI, UX, wording): label `needs-acceptance`.
8. `phase=planned` on the parent, then stop.
