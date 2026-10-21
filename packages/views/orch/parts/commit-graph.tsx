"use client";

import type { ReactNode } from "react";
import { cn } from "@multica/ui/lib/utils";

// Codex-style commit graph: the PR branch is the main lane; a merge commit
// pulls a second lane (the base branch) in from the right. Rows are newest
// first, each a fixed height so the SVG lanes line up across rows.

export type GraphRow = {
  key: string;
  /** Lane of this row's dot: 0 = PR branch, 1 = base branch. */
  lane: 0 | 1;
  /** A merge commit: draw the base lane curving into this dot. */
  merge?: boolean;
  /** Pending row (dashed, hollow dot). */
  pending?: boolean;
  content: ReactNode;
};

const ROW = 34;
const LANE_X = [9, 25];
export const LANE_COLOR = ["oklch(0.685 0.169 237.3)", "oklch(0.606 0.25 292.7)"];

type Seg = { top0: boolean; bot0: boolean; top1: boolean; bot1: boolean };

function segments(rows: GraphRow[]): Seg[] {
  // Lane 0 runs through every PR row; lane 1 runs from the first merge row
  // down to the base row (the last lane-1 row).
  const firstMerge = rows.findIndex((r) => r.merge);
  const lastBase = rows.map((r) => r.lane).lastIndexOf(1);
  const last0 = rows.map((r) => r.lane).lastIndexOf(0);
  return rows.map((_r, i) => {
    const in1 = firstMerge >= 0 && lastBase > firstMerge && i > firstMerge && i <= lastBase;
    return {
      top0: i > 0 && i <= last0,
      bot0: i < last0,
      top1: in1,
      bot1: in1 && i < lastBase,
    };
  });
}

export function CommitGraph({ rows, className }: { rows: GraphRow[]; className?: string }) {
  const segs = segments(rows);
  const width = rows.some((r) => r.lane === 1 || r.merge) ? 34 : 18;
  return (
    <ol className={cn("flex flex-col", className)} data-testid="orch-commit-graph">
      {rows.map((r, i) => {
        const s = segs[i]!;
        const cx = LANE_X[r.lane]!;
        const color = LANE_COLOR[r.lane]!;
        return (
          <li key={r.key} className="flex items-center gap-2" style={{ height: ROW }}>
            <svg width={width} height={ROW} className="shrink-0 overflow-visible" aria-hidden>
              {s.top0 && <line x1={LANE_X[0]} x2={LANE_X[0]} y1={0} y2={ROW / 2} stroke={LANE_COLOR[0]} strokeWidth={2} strokeDasharray={rows[i - 1]?.pending ? "3 3" : undefined} />}
              {s.bot0 && <line x1={LANE_X[0]} x2={LANE_X[0]} y1={ROW / 2} y2={ROW} stroke={LANE_COLOR[0]} strokeWidth={2} />}
              {s.top1 && <line x1={LANE_X[1]} x2={LANE_X[1]} y1={0} y2={ROW / 2} stroke={LANE_COLOR[1]} strokeWidth={2} />}
              {s.bot1 && <line x1={LANE_X[1]} x2={LANE_X[1]} y1={ROW / 2} y2={ROW} stroke={LANE_COLOR[1]} strokeWidth={2} />}
              {r.merge && (
                <path
                  d={`M ${LANE_X[1]} ${ROW} C ${LANE_X[1]} ${ROW * 0.7}, ${LANE_X[0]} ${ROW * 0.75}, ${LANE_X[0]} ${ROW / 2}`}
                  fill="none"
                  stroke={LANE_COLOR[1]}
                  strokeWidth={2}
                  strokeDasharray={r.pending ? "3 3" : undefined}
                />
              )}
              <circle
                cx={cx}
                cy={ROW / 2}
                r={r.pending ? 4 : 4.5}
                fill={r.pending ? "var(--background)" : color}
                stroke={r.pending ? color : "var(--background)"}
                strokeWidth={2}
              />
            </svg>
            <div className="flex min-w-0 flex-1 items-center gap-2">{r.content}</div>
          </li>
        );
      })}
    </ol>
  );
}

export function LaneChip({ name, lane, className }: { name: string; lane: 0 | 1; className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex max-w-44 shrink-0 items-center rounded-full border px-1.5 font-mono text-micro leading-4",
        lane === 0
          ? "border-sky-500/40 bg-sky-500/10 text-sky-700 dark:text-sky-300"
          : "border-violet-500/40 bg-violet-500/10 text-violet-700 dark:text-violet-300",
        className,
      )}
      title={name}
    >
      <span className="truncate">{name}</span>
    </span>
  );
}
