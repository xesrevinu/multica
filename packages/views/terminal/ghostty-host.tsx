"use client";

import { useEffect, useRef } from "react";
import { FitAddon, Terminal, init } from "ghostty-web";

export interface GhosttyHostHandle {
  write: (data: string | Uint8Array) => void;
  resize: (cols: number, rows: number) => void;
  cols: number;
  rows: number;
}

interface GhosttyHostProps {
  onReady: (handle: GhosttyHostHandle) => void;
  onData: (data: string) => void;
  onResize: (size: { cols: number; rows: number }) => void;
}

let initPromise: Promise<void> | null = null;

function ensureInit(): Promise<void> {
  initPromise ??= init();
  return initPromise;
}

export function GhosttyHost({ onReady, onData, onResize }: GhosttyHostProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const onReadyRef = useRef(onReady);
  const onDataRef = useRef(onData);
  const onResizeRef = useRef(onResize);
  onReadyRef.current = onReady;
  onDataRef.current = onData;
  onResizeRef.current = onResize;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    let disposed = false;
    let term: Terminal | null = null;
    const disposers: Array<() => void> = [];

    void ensureInit()
      .then(() => {
        if (disposed) return;
        term = new Terminal({ fontSize: 13, cursorBlink: true });
        const fitAddon = new FitAddon();
        term.loadAddon(fitAddon);
        term.open(el);
        const dataSub = term.onData((data) => onDataRef.current(data));
        const resizeSub = term.onResize((size) => onResizeRef.current(size));
        disposers.push(() => dataSub.dispose());
        disposers.push(() => resizeSub.dispose());

        const applyFit = () => {
          if (!term) return;
          fitAddon.fit();
          onResizeRef.current({ cols: term.cols, rows: term.rows });
        };
        applyFit();
        const observer = new ResizeObserver(() => applyFit());
        observer.observe(el);
        disposers.push(() => observer.disconnect());

        onReadyRef.current({
          write: (data) => term?.write(data),
          resize: (cols, rows) => term?.resize(cols, rows),
          get cols() {
            return term?.cols ?? 80;
          },
          get rows() {
            return term?.rows ?? 24;
          },
        });
      })
      .catch((error: unknown) => {
        if (!disposed) {
          el.textContent =
            error instanceof Error ? error.message : "Failed to load terminal renderer";
        }
      });

    return () => {
      disposed = true;
      for (const dispose of disposers) dispose();
      term?.dispose();
    };
  }, []);

  return <div ref={containerRef} className="h-full min-h-0 w-full overflow-hidden bg-black" />;
}
