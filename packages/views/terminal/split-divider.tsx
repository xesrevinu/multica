"use client";

import { useEffect, useRef } from "react";
import { cn } from "@multica/ui/lib/utils";

const MIN_PANE_SIZE = 80;

export function SplitDivider({
  vertical,
  onDragActiveChange,
  onRatioCommit,
}: {
  vertical: boolean;
  onDragActiveChange: (active: boolean) => void;
  onRatioCommit: (ratio: number) => void;
}) {
  const dividerRef = useRef<HTMLDivElement>(null);
  const frameRef = useRef<number | null>(null);
  const dragRef = useRef<{
    pointerId: number;
    start: number;
    total: number;
    prevSize: number;
    prev: HTMLElement;
    next: HTMLElement;
  } | null>(null);

  const flushFlex = (prev: HTMLElement, next: HTMLElement, prevSize: number, total: number) => {
    prev.style.flex = `${prevSize} 1 0%`;
    next.style.flex = `${total - prevSize} 1 0%`;
  };

  const finish = (commit: boolean) => {
    const drag = dragRef.current;
    if (!drag) return;
    dragRef.current = null;
    if (frameRef.current !== null) {
      cancelAnimationFrame(frameRef.current);
      frameRef.current = null;
    }
    dividerRef.current?.classList.remove("is-dragging");
    onDragActiveChange(false);
    if (!commit) return;
    const prevBox = drag.prev.getBoundingClientRect();
    const nextBox = drag.next.getBoundingClientRect();
    const prevSize = vertical ? prevBox.width : prevBox.height;
    const nextSize = vertical ? nextBox.width : nextBox.height;
    const total = prevSize + nextSize;
    if (total <= 0) return;
    onRatioCommit(prevSize / total);
  };

  useEffect(() => {
    const onMove = (event: PointerEvent) => {
      const drag = dragRef.current;
      if (!drag || event.pointerId !== drag.pointerId) return;
      const current = vertical ? event.clientX : event.clientY;
      const min = Math.min(MIN_PANE_SIZE, drag.total / 2);
      const nextPrev = Math.min(
        Math.max(drag.prevSize + (current - drag.start), min),
        drag.total - min,
      );
      if (frameRef.current !== null) cancelAnimationFrame(frameRef.current);
      frameRef.current = requestAnimationFrame(() => {
        frameRef.current = null;
        flushFlex(drag.prev, drag.next, nextPrev, drag.total);
      });
    };
    const onUp = (event: PointerEvent) => {
      const drag = dragRef.current;
      if (!drag || event.pointerId !== drag.pointerId) return;
      finish(true);
    };
    const onCancel = (event: PointerEvent) => {
      const drag = dragRef.current;
      if (!drag || event.pointerId !== drag.pointerId) return;
      finish(false);
    };
    const onBlur = () => finish(false);
    window.addEventListener("pointermove", onMove, true);
    window.addEventListener("pointerup", onUp, true);
    window.addEventListener("pointercancel", onCancel, true);
    window.addEventListener("blur", onBlur, true);
    return () => {
      window.removeEventListener("pointermove", onMove, true);
      window.removeEventListener("pointerup", onUp, true);
      window.removeEventListener("pointercancel", onCancel, true);
      window.removeEventListener("blur", onBlur, true);
    };
  }, [onDragActiveChange, onRatioCommit, vertical]);

  return (
    <div
      ref={dividerRef}
      role="separator"
      aria-orientation={vertical ? "vertical" : "horizontal"}
      className={cn("pty-divider", vertical ? "is-vertical" : "is-horizontal")}
      onPointerDown={(event) => {
        if (event.button !== 0) return;
        const divider = event.currentTarget;
        const prev = divider.previousElementSibling as HTMLElement | null;
        const next = divider.nextElementSibling as HTMLElement | null;
        if (!prev || !next) return;
        const prevBox = prev.getBoundingClientRect();
        const nextBox = next.getBoundingClientRect();
        const prevMeasured = vertical ? prevBox.width : prevBox.height;
        const nextMeasured = vertical ? nextBox.width : nextBox.height;
        const total = prevMeasured + nextMeasured;
        if (total <= 0) return;
        divider.setPointerCapture(event.pointerId);
        divider.classList.add("is-dragging");
        dragRef.current = {
          pointerId: event.pointerId,
          start: vertical ? event.clientX : event.clientY,
          total,
          prevSize: prevMeasured,
          prev,
          next,
        };
        onDragActiveChange(true);
        event.preventDefault();
      }}
      onDoubleClick={(event) => {
        const divider = event.currentTarget;
        const prev = divider.previousElementSibling as HTMLElement | null;
        const next = divider.nextElementSibling as HTMLElement | null;
        if (!prev || !next) return;
        prev.style.flex = "1 1 0%";
        next.style.flex = "1 1 0%";
        onRatioCommit(0.5);
      }}
    />
  );
}
