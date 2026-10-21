import { useT } from "../i18n";

export const PHASE_KEYS = [
  "implementing",
  "review_requested",
  "reviewing",
  "changes_requested",
  "awaiting_acceptance",
  "blocked",
  "unblocked",
  "cancel_proposed",
  "planned",
  "merged",
  "cancelled",
  "delivered",
] as const;
export type PhaseKey = (typeof PHASE_KEYS)[number];

export function isPhaseKey(v: string | undefined): v is PhaseKey {
  return !!v && (PHASE_KEYS as readonly string[]).includes(v);
}

// usePhaseLabel localizes known orchestrator phases; unknown ones pass through.
export function usePhaseLabel() {
  const { t } = useT("orch");
  return (phase: string | undefined): string => (isPhaseKey(phase) ? t(($) => $.phase[phase]) : (phase ?? ""));
}
