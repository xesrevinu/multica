/**
 * TUI façade over the shared orchestrator domain in `@multica/core`.
 *
 * Transport lives in `data.ts` (Effect-generated Multica client). This module
 * only re-exports the UI-agnostic types and helpers so TUI screens stay
 * aligned with Web without duplicating attention/flow rules.
 */
export {
  availablePROps,
  bucketLabel,
  flowCardMode,
  shouldOfferResolveConflict,
  type OrchFlowAction,
  type OrchFlowCardInput,
  type OrchFlowCardMode,
  type OrchPRAbilities,
  type OrchPROp,
} from "@multica/core/orchestrator/flow"
export {
  groupOrchIssues,
  applyOrchView,
  filterOrchIssues,
  ORCH_BUCKETS,
  DEFAULT_ORCH_VIEW,
  type OrchIssueGroup,
  type OrchIssueNode,
  type OrchViewOptions,
} from "@multica/core/orchestrator/tree"
export {
  parseOrchIssueList,
  parseOrchIssues,
  parseOrchPRCard,
  parseOrchTimeline,
  parseOrchWorktrees,
  type OrchIssueList,
} from "@multica/core/orchestrator/parse"
export type {
  OrchAttentionBucket,
  OrchIssue,
  OrchPRCard,
  OrchTimeline,
  OrchRun,
  OrchTransition,
  OrchWorktrees,
} from "@multica/core/orchestrator/schemas"
