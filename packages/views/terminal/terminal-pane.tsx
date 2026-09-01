"use client";

import { useEffect, useRef, useState } from "react";
import { Columns2, Maximize2, Minimize2, Rows2, X } from "lucide-react";
import type { PtyPane } from "@multica/core/terminal";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { GhosttyHost } from "./ghostty-host";
import { TerminalLauncher, type TerminalLauncherRow } from "./terminal-launcher";
import { TerminalPaneMenu } from "./terminal-pane-menu";
import { usePtyConnection } from "./use-pty-connection";
import { useSplitDragging } from "./split-drag-context";

export function TerminalPaneView({
  pane,
  slug,
  active,
  canClose,
  canExpand,
  expanded,
  launcherRows,
  hasMachine,
  onFocus,
  onSplitRight,
  onSplitDown,
  onClose,
  onToggleExpand,
  onEqualize,
  onRename,
  onOpenShell,
  onStartAgent,
}: {
  pane: PtyPane;
  slug: string;
  active: boolean;
  canClose: boolean;
  canExpand: boolean;
  expanded: boolean;
  launcherRows: TerminalLauncherRow[];
  hasMachine: boolean;
  onFocus: () => void;
  onSplitRight: () => void;
  onSplitDown: () => void;
  onClose: () => void;
  onToggleExpand: () => void;
  onEqualize: () => void;
  onRename: (title: string) => void;
  onOpenShell: (projectId: string | null) => void;
  onStartAgent: (projectId: string | null, agentId: string) => void;
}) {
  const { t } = useT("layout");
  const live = pane.kind !== "idle" && !!pane.daemonId;
  const holdResize = useSplitDragging();
  const rootRef = useRef<HTMLDivElement>(null);
  const [menu, setMenu] = useState<{ x: number; y: number } | null>(null);
  const [editing, setEditing] = useState(false);

  useEffect(() => {
    const el = rootRef.current;
    if (!el) return;
    const onMenu = (event: MouseEvent) => {
      event.preventDefault();
      event.stopPropagation();
      onFocus();
      setMenu({ x: event.clientX, y: event.clientY });
    };
    el.addEventListener("contextmenu", onMenu, true);
    return () => el.removeEventListener("contextmenu", onMenu, true);
  }, [onFocus]);
  const [ttyFocus, setTtyFocus] = useState(false);
  const { status, message, onReady, onData, onResize, copy, paste, selectAll, clear, focus } =
    usePtyConnection({
    enabled: live,
    daemonId: pane.daemonId,
    ptyId: pane.id,
    argv: pane.argv,
    cwd: pane.cwd,
    slug,
    holdResize,
  });

  useEffect(() => {
    const onPointerDown = (event: PointerEvent) => {
      const root = rootRef.current;
      if (!root) return;
      if (event.target instanceof Element && event.target.closest("[data-terminal-pane-menu]")) {
        return;
      }
      if (event.target instanceof Node && root.contains(event.target)) return;
      setTtyFocus(false);
      const activeEl = document.activeElement;
      if (activeEl instanceof HTMLElement && root.contains(activeEl)) activeEl.blur();
    };
    document.addEventListener("pointerdown", onPointerDown, true);
    return () => document.removeEventListener("pointerdown", onPointerDown, true);
  }, []);

  return (
    <div
      ref={rootRef}
      data-terminal-pane={pane.id}
      className={cn(
        "group/pane relative flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden",
        ttyFocus ? "ring-1 ring-brand/40" : "ring-1 ring-transparent opacity-[0.82]",
      )}
      onMouseDown={(event) => {
        onFocus();
        const target = event.target as HTMLElement | null;
        if (target?.closest("[data-pane-chrome], button, input")) return;
        setTtyFocus(true);
        focus();
      }}
    >
      <div
        data-pane-chrome
        className={cn(
          "pointer-events-none absolute top-0 right-3 left-0 z-10 flex h-7 items-center gap-1 bg-black/40 px-1.5 text-white transition-opacity [&_button]:pointer-events-auto [&_input]:pointer-events-auto",
          canClose || ttyFocus ? "opacity-100" : "opacity-0 group-hover/pane:opacity-100",
        )}
      >
        {editing ? (
          <input
            autoFocus
            defaultValue={pane.title}
            className="h-6 min-w-0 flex-1 rounded-md border border-white/20 bg-black/50 px-1 font-mono text-caption text-white outline-none"
            onBlur={(event) => {
              const next = event.currentTarget.value.trim();
              if (next) onRename(next);
              setEditing(false);
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter") event.currentTarget.blur();
              if (event.key === "Escape") setEditing(false);
            }}
            onClick={(event) => event.stopPropagation()}
          />
        ) : (
          <span
            className="pointer-events-auto min-w-0 flex-1 truncate px-1 font-mono text-caption"
            onDoubleClick={() => setEditing(true)}
          >
            {pane.title}
            {pane.cwd ? ` · ${pane.cwd}` : ""}
            {status === "connecting" ? ` · ${t(($) => $.terminal.connecting)}` : ""}
            {status === "error" && message ? ` · ${message}` : ""}
          </span>
        )}
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type="button"
                size="icon-xs"
                variant="ghost"
                className="text-white hover:bg-white/10"
                aria-label={t(($) => $.terminal.split_right)}
                onClick={onSplitRight}
              >
                <Columns2 className="size-3.5" />
              </Button>
            }
          />
          <TooltipContent>{t(($) => $.terminal.split_right)}</TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger
            render={
              <Button
                type="button"
                size="icon-xs"
                variant="ghost"
                className="text-white hover:bg-white/10"
                aria-label={t(($) => $.terminal.split_down)}
                onClick={onSplitDown}
              >
                <Rows2 className="size-3.5" />
              </Button>
            }
          />
          <TooltipContent>{t(($) => $.terminal.split_down)}</TooltipContent>
        </Tooltip>
        {canExpand ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  size="icon-xs"
                  variant="ghost"
                  className="text-white hover:bg-white/10"
                  aria-label={expanded ? t(($) => $.terminal.restore_pane) : t(($) => $.terminal.maximize_pane)}
                  onClick={onToggleExpand}
                >
                  {expanded ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
                </Button>
              }
            />
            <TooltipContent>
              {expanded ? t(($) => $.terminal.restore_pane) : t(($) => $.terminal.maximize_pane)}
            </TooltipContent>
          </Tooltip>
        ) : null}
        {canClose ? (
          <Tooltip>
            <TooltipTrigger
              render={
                <Button
                  type="button"
                  size="icon-xs"
                  variant="ghost"
                  className="text-white hover:bg-white/10"
                  aria-label={t(($) => $.terminal.close_pane)}
                  onClick={onClose}
                >
                  <X className="size-3.5" />
                </Button>
              }
            />
            <TooltipContent>{t(($) => $.terminal.close_pane)}</TooltipContent>
          </Tooltip>
        ) : null}
      </div>
      {live ? (
        <>
          <GhosttyHost
            onReady={onReady}
            onData={onData}
            onResize={onResize}
            suspendGeometry={holdResize}
            focused={ttyFocus}
          />
          {status === "connecting" ? (
            <div className="pointer-events-none absolute inset-0 flex items-center justify-center bg-black/20">
              <Spinner className="size-5 text-white/80" />
            </div>
          ) : null}
        </>
      ) : (
        <TerminalLauncher
          hasMachine={hasMachine}
          rows={launcherRows}
          onOpenShell={onOpenShell}
          onStartAgent={onStartAgent}
        />
      )}
      <TerminalPaneMenu
        open={menu !== null}
        point={menu ?? { x: 0, y: 0 }}
        canClose={canClose}
        canExpand={canExpand}
        expanded={expanded}
        onOpenChange={(open) => {
          if (!open) setMenu(null);
        }}
        onSplitRight={onSplitRight}
        onSplitDown={onSplitDown}
        onEqualize={onEqualize}
        onToggleExpand={onToggleExpand}
        onClose={onClose}
        onCopy={() => void copy()}
        onPaste={() => void paste()}
        onSelectAll={selectAll}
        onClear={clear}
        onSetTitle={() => setEditing(true)}
      />
    </div>
  );
}
