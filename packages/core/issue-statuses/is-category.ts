import type { IssueStatusCategory } from "../types";
import { STATUS_ORDER } from "../issues/config/status";

const BUILT_IN = new Set<string>(STATUS_ORDER);

/** True when `value` is a lifecycle category (`unstarted` / `active` / `completed` / `cancelled`). */
export function isIssueStatusCategory(value: string): value is IssueStatusCategory {
  return BUILT_IN.has(value);
}
