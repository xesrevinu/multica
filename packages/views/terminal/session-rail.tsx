"use client";

import { useEffect, useRef, useState, type ReactNode } from "react";
import { Copy, PanelLeft, Pencil, Terminal, X } from "lucide-react";
import type { PtySession } from "@multica/core/terminal";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

export function TerminalSessionRail({
  sessions,
  activeSessionId,
  collapsed,
  className,
  showCollapse = true,
  newAction,
  onSelect,
  onClose,
  onRename,
  onDuplicate,
  onToggleCollapsed,
}: {
  sessions: PtySession[];
  activeSessionId: string | null;
  collapsed: boolean;
  className?: string;
  showCollapse?: boolean;
  newAction: ReactNode;
  onSelect: (sessionId: string) => void;
  onClose: (sessionId: string) => void;
  onRename: (sessionId: string, title: string) => void;
  onDuplicate: (sessionId: string) => void;
  onToggleCollapsed: () => void;
}) {
  const { t } = useT("layout");
  const [editingId, setEditingId] = useState<string | null>(null);
  const [menu, setMenu] = useState<{ id: string; x: number; y: number } | null>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!menu) return;
    const onDown = (event: PointerEvent) => {
      if (event.target instanceof Node && menuRef.current?.contains(event.target)) return;
      setMenu(null);
    };
    window.addEventListener("pointerdown", onDown, true);
    return () => window.removeEventListener("pointerdown", onDown, true);
  }, [menu]);

  if (collapsed) {
    return (
      <div className={cn("flex h-full w-10 shrink-0 flex-col items-center border-r bg-background py-2", className)}>
        <Button
          type="button"
          size="icon-xs"
          variant="ghost"
          aria-label={t(($) => $.terminal.expand_sessions)}
          onClick={onToggleCollapsed}
        >
          <PanelLeft className="size-3.5" />
        </Button>
        <div className="mt-1">{newAction}</div>
        <div className="mt-2 flex min-h-0 flex-1 flex-col items-center gap-1 overflow-y-auto">
          {sessions.map((session) => (
            <button
              key={session.id}
              type="button"
              title={session.title}
              aria-label={session.title}
              onClick={() => onSelect(session.id)}
              className={cn(
                "flex size-7 items-center justify-center rounded-md",
                session.id === activeSessionId
                  ? "bg-accent text-foreground"
                  : "text-muted-foreground hover:bg-accent/60",
              )}
            >
              <Terminal className="size-3.5" />
            </button>
          ))}
        </div>
      </div>
    );
  }

  return (
    <div className={cn("flex h-full min-h-0 w-full shrink-0 flex-col border-r bg-background", className)}>
      <div className="flex h-10 shrink-0 items-center gap-1 px-2">
        <span className="min-w-0 flex-1 truncate px-1 text-caption font-medium">
          {t(($) => $.terminal.sessions)}
        </span>
        {newAction}
        {showCollapse ? (
          <Button
            type="button"
            size="icon-xs"
            variant="ghost"
            aria-label={t(($) => $.terminal.collapse_sessions)}
            onClick={onToggleCollapsed}
          >
            <PanelLeft className="size-3.5" />
          </Button>
        ) : null}
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-1 pb-2">
        {sessions.length === 0 ? (
          <p className="px-2 py-3 text-caption text-muted-foreground">
            {t(($) => $.terminal.empty_sessions)}
          </p>
        ) : (
          sessions.map((session) => {
            const pane = session.panes[session.activeLeafId];
            const live = Object.values(session.panes).some((item) => item.kind !== "idle");
            const subtitle = pane?.cwd || t(($) => $.terminal.cwd_home);
            return (
              <div
                key={session.id}
                className={cn(
                  "group/session flex items-center rounded-md",
                  session.id === activeSessionId ? "bg-accent" : "hover:bg-accent/50",
                )}
                onContextMenu={(event) => {
                  event.preventDefault();
                  setMenu({ id: session.id, x: event.clientX, y: event.clientY });
                }}
              >
                <button
                  type="button"
                  className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left"
                  onClick={() => onSelect(session.id)}
                  onDoubleClick={(event) => {
                    event.preventDefault();
                    setEditingId(session.id);
                  }}
                >
                  <span
                    className={cn(
                      "size-1.5 shrink-0 rounded-full",
                      live ? "bg-success" : "bg-muted-foreground/40",
                    )}
                  />
                  <span className="min-w-0 flex-1">
                    {editingId === session.id ? (
                      <input
                        autoFocus
                        defaultValue={session.title}
                        className="h-6 w-full rounded-md border border-input bg-background px-1 text-body outline-none"
                        onClick={(event) => event.stopPropagation()}
                        onBlur={(event) => {
                          const next = event.currentTarget.value.trim();
                          if (next) onRename(session.id, next);
                          setEditingId(null);
                        }}
                        onKeyDown={(event) => {
                          if (event.key === "Enter") {
                            event.currentTarget.blur();
                          }
                          if (event.key === "Escape") {
                            setEditingId(null);
                          }
                        }}
                      />
                    ) : (
                      <>
                        <span className="block truncate text-body">{session.title}</span>
                        <span className="block truncate font-mono text-caption text-muted-foreground">
                          {subtitle}
                        </span>
                      </>
                    )}
                  </span>
                </button>
                <Button
                  type="button"
                  size="icon-xs"
                  variant="ghost"
                  className="mr-1 opacity-0 group-hover/session:opacity-100"
                  aria-label={t(($) => $.terminal.close_session)}
                  onClick={() => onClose(session.id)}
                >
                  <X className="size-3" />
                </Button>
              </div>
            );
          })
        )}
      </div>
      {menu ? (
        <div
          ref={menuRef}
          className="fixed z-50 min-w-44 rounded-lg bg-surface-raised p-1 shadow-[var(--menu-shadow)] ring-1 ring-surface-border"
          style={{ left: menu.x, top: menu.y }}
        >
          <button
            type="button"
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent"
            onClick={() => {
              setEditingId(menu.id);
              setMenu(null);
            }}
          >
            <Pencil className="size-3.5" />
            {t(($) => $.terminal.set_title)}
          </button>
          <button
            type="button"
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent"
            onClick={() => {
              onDuplicate(menu.id);
              setMenu(null);
            }}
          >
            <Copy className="size-3.5" />
            {t(($) => $.terminal.duplicate_session)}
          </button>
          <button
            type="button"
            className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body text-destructive hover:bg-destructive/10"
            onClick={() => {
              onClose(menu.id);
              setMenu(null);
            }}
          >
            <X className="size-3.5" />
            {t(($) => $.terminal.close_session)}
          </button>
        </div>
      ) : null}
    </div>
  );
}
