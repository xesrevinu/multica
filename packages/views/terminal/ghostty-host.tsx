"use client";

import { useEffect, useRef } from "react";
import { FitAddon } from "@xterm/addon-fit";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { WebglAddon } from "@xterm/addon-webgl";
import { Terminal } from "@xterm/xterm";
import { useTheme } from "@multica/ui/components/common/theme-provider";
import { cn } from "@multica/ui/lib/utils";
import {
  GHOSTTY_FONT_FAMILY,
  GHOSTTY_FONT_SIZE,
  tokyoNightTheme,
} from "./ghostty-theme";
import "@xterm/xterm/css/xterm.css";
import "./terminal-xterm.css";

const MIN_COLS = 8;
const MIN_ROWS = 4;
const MIN_BOX_PX = 24;
const FIT_DEBOUNCE_MS = 80;

export interface GhosttyHostHandle {
  write: (data: string | Uint8Array) => void;
  focus: () => void;
  blur: () => void;
  resize: (cols: number, rows: number) => void;
  copy: () => Promise<void>;
  paste: () => Promise<void>;
  selectAll: () => void;
  clear: () => void;
  zoomIn: () => void;
  zoomOut: () => void;
  zoomReset: () => void;
  cols: number;
  rows: number;
}

interface GhosttyHostProps {
  onReady: (handle: GhosttyHostHandle) => void;
  onData: (data: string) => void;
  onResize: (size: { cols: number; rows: number }) => void;
  suspendGeometry?: boolean;
  focused?: boolean;
  className?: string;
}

export function GhosttyHost({
  onReady,
  onData,
  onResize,
  suspendGeometry = false,
  focused = false,
  className,
}: GhosttyHostProps) {
  const { resolvedTheme } = useTheme();
  const mode = resolvedTheme === "light" ? "light" : "dark";
  const palette = tokyoNightTheme(mode);
  const containerRef = useRef<HTMLDivElement>(null);
  const onReadyRef = useRef(onReady);
  const onDataRef = useRef(onData);
  const onResizeRef = useRef(onResize);
  const suspendRef = useRef(suspendGeometry);
  const focusedRef = useRef(focused);
  const fitNowRef = useRef<() => void>(() => {});
  const applyFocusRef = useRef<() => void>(() => {});
  onReadyRef.current = onReady;
  onDataRef.current = onData;
  onResizeRef.current = onResize;
  suspendRef.current = suspendGeometry;
  focusedRef.current = focused;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    let disposed = false;
    let fitTimer: number | null = null;
    const disposers: Array<() => void> = [];

    const term = new Terminal({
      allowProposedApi: true,
      fontSize: GHOSTTY_FONT_SIZE,
      fontFamily: GHOSTTY_FONT_FAMILY,
      fontWeight: "400",
      fontWeightBold: "700",
      cursorBlink: true,
      cursorStyle: "bar",
      cursorWidth: 2,
      cursorInactiveStyle: "bar",
      theme: palette,
      scrollback: 10_000,
      scrollSensitivity: 1.15,
      fastScrollSensitivity: 5,
      allowTransparency: false,
      macOptionIsMeta: false,
      macOptionClickForcesSelection: true,
      drawBoldTextInBrightColors: true,
      minimumContrastRatio: 1,
      convertEol: false,
    });

    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(new Unicode11Addon());
    term.unicode.activeVersion = "11";
    term.loadAddon(new WebLinksAddon());
    try {
      const webgl = new WebglAddon();
      webgl.onContextLoss(() => webgl.dispose());
      term.loadAddon(webgl);
      disposers.push(() => webgl.dispose());
    } catch {
      // Canvas renderer is the fallback.
    }

    term.open(el);

    let lastNotified = { cols: 0, rows: 0 };
    const notifyResize = (cols: number, rows: number) => {
      if (cols < MIN_COLS || rows < MIN_ROWS) return;
      if (lastNotified.cols === cols && lastNotified.rows === rows) return;
      lastNotified = { cols, rows };
      onResizeRef.current({ cols, rows });
    };

    const applyFit = () => {
      if (disposed || suspendRef.current) return;
      const box = el.getBoundingClientRect();
      if (box.width < MIN_BOX_PX || box.height < MIN_BOX_PX) return;
      const proposed = fitAddon.proposeDimensions();
      if (!proposed || proposed.cols < MIN_COLS || proposed.rows < MIN_ROWS) return;
      if (proposed.cols === term.cols && proposed.rows === term.rows) return;
      fitAddon.fit();
    };
    fitNowRef.current = applyFit;

    const scheduleFit = () => {
      if (suspendRef.current) return;
      if (fitTimer !== null) window.clearTimeout(fitTimer);
      fitTimer = window.setTimeout(() => {
        fitTimer = null;
        applyFit();
      }, FIT_DEBOUNCE_MS);
    };

    let fontSize = GHOSTTY_FONT_SIZE;
    const applyFontSize = (next: number) => {
      fontSize = Math.min(32, Math.max(8, next));
      term.options.fontSize = fontSize;
      applyFit();
    };

    const applyFocus = () => {
      term.options.disableStdin = !focusedRef.current;
      if (focusedRef.current) term.focus();
      else term.blur();
    };
    applyFocusRef.current = applyFocus;

    const dataSub = term.onData((data) => onDataRef.current(data));
    const resizeSub = term.onResize((size) => notifyResize(size.cols, size.rows));
    const selectionSub = term.onSelectionChange(() => {
      const text = term.getSelection();
      if (text) void navigator.clipboard.writeText(text);
    });
    disposers.push(() => dataSub.dispose());
    disposers.push(() => resizeSub.dispose());
    disposers.push(() => selectionSub.dispose());

    term.attachCustomKeyEventHandler((event) => {
      if (!(event.metaKey || event.ctrlKey) || event.altKey) return true;
      if (event.key === "=" || event.key === "+" || event.key === "-" || event.key === "0") {
        if (event.type === "keydown") {
          if (event.key === "0") applyFontSize(GHOSTTY_FONT_SIZE);
          else applyFontSize(fontSize + (event.key === "-" ? -1 : 1));
        }
        return false;
      }
      return true;
    });

    const onWheel = (event: WheelEvent) => {
      if (!(event.metaKey || event.ctrlKey)) return;
      event.preventDefault();
      applyFontSize(fontSize + (event.deltaY < 0 ? 1 : -1));
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    disposers.push(() => el.removeEventListener("wheel", onWheel));

    applyFit();
    applyFocus();
    const observer = new ResizeObserver(() => scheduleFit());
    observer.observe(el);
    disposers.push(() => observer.disconnect());
    window.addEventListener("resize", scheduleFit);
    window.visualViewport?.addEventListener("resize", scheduleFit);
    disposers.push(() => {
      window.removeEventListener("resize", scheduleFit);
      window.visualViewport?.removeEventListener("resize", scheduleFit);
    });

    onReadyRef.current({
      write: (data) => term.write(data),
      focus: () => {
        focusedRef.current = true;
        applyFocus();
      },
      blur: () => {
        focusedRef.current = false;
        applyFocus();
      },
      resize: (cols, rows) => {
        if (cols < MIN_COLS || rows < MIN_ROWS) return;
        term.resize(cols, rows);
      },
      copy: async () => {
        const text = term.getSelection();
        if (text) await navigator.clipboard.writeText(text);
      },
      paste: async () => {
        const text = await navigator.clipboard.readText();
        if (text) term.paste(text);
      },
      selectAll: () => term.selectAll(),
      clear: () => term.clear(),
      zoomIn: () => applyFontSize(fontSize + 1),
      zoomOut: () => applyFontSize(fontSize - 1),
      zoomReset: () => applyFontSize(GHOSTTY_FONT_SIZE),
      get cols() {
        return term.cols;
      },
      get rows() {
        return term.rows;
      },
    });

    return () => {
      disposed = true;
      if (fitTimer !== null) window.clearTimeout(fitTimer);
      for (const dispose of disposers) dispose();
      term.dispose();
    };
  }, [mode]);

  useEffect(() => {
    if (suspendGeometry) return;
    fitNowRef.current();
  }, [suspendGeometry]);

  useEffect(() => {
    applyFocusRef.current();
  }, [focused]);

  return (
    <div
      style={{ backgroundColor: palette.background, color: palette.foreground }}
      className={cn("flex h-full min-h-0 w-full flex-col overflow-hidden px-3 pb-2 pt-8", className)}
    >
      <div
        ref={containerRef}
        className="multica-xterm relative min-h-0 w-full flex-1 overflow-hidden"
      />
    </div>
  );
}
