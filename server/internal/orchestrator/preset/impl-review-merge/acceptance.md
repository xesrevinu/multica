# Human acceptance gate (GUI-Master / sign-off)

An issue whose label is `needs-acceptance`, `needs-GUI-Master` or
`needs-Ray-signoff` (or metadata `acceptance_required=true`) stops after an
approved review and green checks. The orchestrator assigns it to the team's
human, writes an inbox item (action required) and waits.

## What the human checks
- The behavior in the real app or artifact (not only the diff): open the
  preview, the build, the screenshot attached by the owner.
- The acceptance criteria in the issue, literally.
- Anything the reviewer flagged as residual risk.

## Decide
- Accept: Flow card "Accept" or `multica flow accept {{KEY}}`. The engine
  merges on the same head (CI re-checked).
- Reject: Flow card reason + "Reject" or
  `multica flow reject {{KEY}} --reason "<what is wrong and what good looks like>"`.
  The reason goes to the owner verbatim (`blocked_reason`), so write it as a
  task: observable problem, expected result, how you will check it.

## Rules
- The gate holds the head: a new push re-reviews unless it is a pure rebase of
  the approved patch (T3c keeps the verdict).
- Agents can never write `acceptance*`. Only humans decide here.
