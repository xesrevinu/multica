"use client";

import { useEffect, useRef } from "react";
import { Clipboard, Columns2, Copy, Equal, Eraser, Maximize2, Minimize2, Pencil, Rows2, TextSelect, X } from "lucide-react";
import { getShortcutPlatform } from "@multica/core/shortcuts";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";

function MenuItem({
  icon,
  label,
  shortcut,
  danger,
  onSelect,
}: {
  icon: React.ReactNode;
  label: string;
  shortcut?: string;
  danger?: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      className={cn(
        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body outline-none",
        danger
          ? "text-destructive hover:bg-destructive/10"
          : "hover:bg-accent hover:text-accent-foreground",
      )}
      onClick={onSelect}
    >
      <span className="[&_svg]:size-3.5">{icon}</span>
      <span className="flex-1">{label}</span>
      {shortcut ? (
        <span className="text-caption tracking-widest text-muted-foreground">{shortcut}</span>
      ) : null}
    </button>
  );
}

export function TerminalPaneMenu({
  open,
  point,
  canClose,
  canExpand,
  expanded,
  onOpenChange,
  onSplitRight,
  onSplitDown,
  onEqualize,
  onToggleExpand,
  onClose,
  onCopy,
  onPaste,
  onSelectAll,
  onClear,
  onSetTitle,
}: {
  open: boolean;
  point: { x: number; y: number };
  canClose: boolean;
  canExpand: boolean;
  expanded: boolean;
  onOpenChange: (open: boolean) => void;
  onSplitRight: () => void;
  onSplitDown: () => void;
  onEqualize: () => void;
  onToggleExpand: () => void;
  onClose: () => void;
  onCopy: () => void;
  onPaste: () => void;
  onSelectAll: () => void;
  onClear: () => void;
  onSetTitle: () => void;
}) {
  const { t } = useT("layout");
  const mac = getShortcutPlatform() === "macos";
  const mod = mac ? "⌘" : "Ctrl";
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (event: MouseEvent) => {
      if (event.target instanceof Node && menuRef.current?.contains(event.target)) return;
      onOpenChange(false);
    };
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onOpenChange(false);
    };
    window.addEventListener("pointerdown", onDown, true);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("pointerdown", onDown, true);
      window.removeEventListener("keydown", onKey);
    };
  }, [open, onOpenChange]);

  if (!open) return null;

  const maxX = typeof window === "undefined" ? point.x : Math.min(point.x, window.innerWidth - 240);
  const maxY = typeof window === "undefined" ? point.y : Math.min(point.y, window.innerHeight - 280);

  return (
    <div
      ref={menuRef}
      role="menu"
      data-terminal-pane-menu=""
      className="fixed z-50 min-w-56 rounded-lg bg-surface-raised p-1 text-popover-foreground shadow-[var(--menu-shadow)] ring-1 ring-surface-border"
      style={{ left: Math.max(8, maxX), top: Math.max(8, maxY) }}
    >
      <MenuItem
        icon={<Columns2 />}
        label={t(($) => $.terminal.split_right)}
        shortcut={`${mod}\\`}
        onSelect={() => {
          onSplitRight();
          onOpenChange(false);
        }}
      />
      <MenuItem
        icon={<Rows2 />}
        label={t(($) => $.terminal.split_down)}
        shortcut={`${mod}⇧\\`}
        onSelect={() => {
          onSplitDown();
          onOpenChange(false);
        }}
      />
      {canClose ? (
        <MenuItem
          icon={<Equal />}
          label={t(($) => $.terminal.equalize_panes)}
          onSelect={() => {
            onEqualize();
            onOpenChange(false);
          }}
        />
      ) : null}
      {canExpand ? (
        <MenuItem
          icon={expanded ? <Minimize2 /> : <Maximize2 />}
          label={expanded ? t(($) => $.terminal.restore_pane) : t(($) => $.terminal.maximize_pane)}
          onSelect={() => {
            onToggleExpand();
            onOpenChange(false);
          }}
        />
      ) : null}
      {canClose ? (
        <MenuItem
          icon={<X />}
          label={t(($) => $.terminal.close_pane)}
          danger
          onSelect={() => {
            onClose();
            onOpenChange(false);
          }}
        />
      ) : null}
      <div className="-mx-1 my-1 h-px bg-border" />
      <MenuItem
        icon={<Pencil />}
        label={t(($) => $.terminal.set_title)}
        onSelect={() => {
          onSetTitle();
          onOpenChange(false);
        }}
      />
      <MenuItem
        icon={<Copy />}
        label={t(($) => $.terminal.copy)}
        shortcut={mac ? "⌘C" : "Ctrl+C"}
        onSelect={() => {
          onCopy();
          onOpenChange(false);
        }}
      />
      <MenuItem
        icon={<Clipboard />}
        label={t(($) => $.terminal.paste)}
        shortcut={mac ? "⌘V" : "Ctrl+V"}
        onSelect={() => {
          onPaste();
          onOpenChange(false);
        }}
      />
      <MenuItem
        icon={<TextSelect />}
        label={t(($) => $.terminal.select_all)}
        onSelect={() => {
          onSelectAll();
          onOpenChange(false);
        }}
      />
      <MenuItem
        icon={<Eraser />}
        label={t(($) => $.terminal.clear_screen)}
        onSelect={() => {
          onClear();
          onOpenChange(false);
        }}
      />
    </div>
  );
}
