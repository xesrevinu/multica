// Compact durations for run folds and status strips: "45s", "12m", "2h 05m", "3d".
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) ms = 0;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${String(m % 60).padStart(2, "0")}m`;
  return `${Math.floor(h / 24)}d`;
}

export function since(iso: string | undefined, now = Date.now()): number {
  if (!iso) return 0;
  const t = new Date(iso).getTime();
  return Number.isFinite(t) ? Math.max(0, now - t) : 0;
}

export function shortSha(sha: string | undefined, n = 7): string {
  return sha ? sha.slice(0, n) : "";
}

// checksTone maps a rollup/conclusion to a tone used for dots and chips.
export function checksTone(state: string | undefined): "ok" | "bad" | "pending" | "none" {
  const s = (state ?? "").toLowerCase();
  if (!s) return "none";
  if (s === "success" || s === "neutral" || s === "skipped") return "ok";
  if (s === "failure" || s === "error" || s === "timed_out" || s === "cancelled" || s === "action_required") return "bad";
  return "pending";
}

export const TONE_DOT: Record<ReturnType<typeof checksTone>, string> = {
  ok: "bg-emerald-500",
  bad: "bg-red-500",
  pending: "bg-amber-500",
  none: "bg-muted-foreground/40",
};
