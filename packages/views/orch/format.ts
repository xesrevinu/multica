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

// shortAgo is the sidebar's compact relative time: "now", "5m", "2h", "3d", "4w".
export function shortAgo(iso: string | undefined, now = Date.now()): string {
  const ms = since(iso, now);
  if (!iso || ms < 45_000) return "now";
  const m = Math.round(ms / 60_000);
  if (m < 60) return `${m}m`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h}h`;
  const d = Math.round(h / 24);
  if (d < 14) return `${d}d`;
  return `${Math.round(d / 7)}w`;
}

export type PRStateTone = "open" | "merged" | "closed" | "draft";

export function prStateTone(state: string | undefined, draft?: boolean): PRStateTone {
  const s = (state ?? "").toLowerCase();
  if (s === "merged") return "merged";
  if (s === "closed") return "closed";
  if (draft || s === "draft") return "draft";
  return "open";
}

// GitHub's own state colours (open green, merged purple, closed red).
export const PR_STATE_TEXT: Record<PRStateTone, string> = {
  open: "text-emerald-600 dark:text-emerald-400",
  merged: "text-violet-600 dark:text-violet-400",
  closed: "text-red-600 dark:text-red-400",
  draft: "text-muted-foreground",
};

export const PR_STATE_CHIP: Record<PRStateTone, string> = {
  open: "bg-emerald-500/12 text-emerald-700 dark:text-emerald-300",
  merged: "bg-violet-500/12 text-violet-700 dark:text-violet-300",
  closed: "bg-red-500/12 text-red-700 dark:text-red-300",
  draft: "bg-muted text-muted-foreground",
};

export function compactCount(n: number): string {
  if (n < 1000) return String(n);
  const k = n / 1000;
  return `${k < 10 ? k.toFixed(1).replace(/\.0$/, "") : Math.round(k)}k`;
}

// Stable hue per string, for monogram badges.
export function hueOf(s: string): number {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0;
  return h % 360;
}
