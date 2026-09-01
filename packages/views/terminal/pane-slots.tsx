"use client";

import { useLayoutEffect, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

const slots = new Map<string, HTMLElement>();
const listeners = new Set<() => void>();

function notify() {
  for (const listener of listeners) listener();
}

export function PtySlot({ leafId }: { leafId: string }) {
  return (
    <div
      data-pty-slot={leafId}
      className="h-full min-h-0 min-w-0"
      ref={(node) => {
        if (node) slots.set(leafId, node);
        else slots.delete(leafId);
        notify();
      }}
    />
  );
}

export function PtyPortal({ leafId, children }: { leafId: string; children: ReactNode }) {
  const [target, setTarget] = useState<HTMLElement | null>(() => slots.get(leafId) ?? null);

  useLayoutEffect(() => {
    const sync = () => setTarget(slots.get(leafId) ?? null);
    sync();
    listeners.add(sync);
    return () => {
      listeners.delete(sync);
    };
  }, [leafId]);

  if (!target) return null;
  return createPortal(children, target);
}
