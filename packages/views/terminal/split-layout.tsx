"use client";

import { useState, type ReactNode } from "react";
import { collectLeafIds, type PtyLayoutNode, type PtyPane } from "@multica/core/terminal";
import { cn } from "@multica/ui/lib/utils";
import { PtyPortal, PtySlot } from "./pane-slots";
import type { TerminalLauncherRow } from "./terminal-launcher";
import { SplitDragContext } from "./split-drag-context";
import { SplitDivider } from "./split-divider";
import { TerminalPaneView } from "./terminal-pane";
import "./split-layout.css";

export function TerminalSplitLayout({
  node,
  panes,
  activeLeafId,
  expandedLeafId,
  slug,
  launcherRows,
  hasMachine,
  onFocus,
  onSplitRight,
  onSplitDown,
  onClose,
  onToggleExpand,
  onEqualize,
  onSplitRatio,
  onRename,
  onOpenShell,
  onStartAgent,
}: {
  node: PtyLayoutNode;
  panes: Record<string, PtyPane>;
  activeLeafId: string;
  expandedLeafId: string | null;
  slug: string;
  launcherRows: TerminalLauncherRow[];
  hasMachine: boolean;
  onFocus: (leafId: string) => void;
  onSplitRight: (leafId: string) => void;
  onSplitDown: (leafId: string) => void;
  onClose: (leafId: string) => void;
  onToggleExpand: (leafId: string) => void;
  onEqualize: (leafId: string) => void;
  onSplitRatio: (splitId: string, ratio: number) => void;
  onRename: (leafId: string, title: string) => void;
  onOpenShell: (leafId: string, projectId: string | null) => void;
  onStartAgent: (leafId: string, projectId: string | null, agentId: string) => void;
}) {
  const [dragging, setDragging] = useState(false);
  const leafIds = collectLeafIds(node);
  const paneCount = leafIds.length;
  const hiddenIds = expandedLeafId ? leafIds.filter((id) => id !== expandedLeafId) : [];

  const renderTree = (current: PtyLayoutNode): ReactNode => {
    if (current.type === "leaf") {
      return <PtySlot leafId={current.id} />;
    }
    const vertical = current.direction === "vertical";
    const ratio = current.ratio ?? 0.5;
    return (
      <div className={cn("pty-split", vertical ? "is-vertical" : "is-horizontal", dragging && "is-dragging")}>
        <div className="pty-split-child" style={{ flex: `${ratio} 1 0%` }}>
          {renderTree(current.first)}
        </div>
        <SplitDivider
          vertical={vertical}
          onDragActiveChange={setDragging}
          onRatioCommit={(next) => onSplitRatio(current.id, next)}
        />
        <div className="pty-split-child" style={{ flex: `${1 - ratio} 1 0%` }}>
          {renderTree(current.second)}
        </div>
      </div>
    );
  };

  return (
    <SplitDragContext.Provider value={dragging}>
      <div className="relative h-full min-h-0 w-full">
        {expandedLeafId && panes[expandedLeafId] ? <PtySlot leafId={expandedLeafId} /> : renderTree(node)}
        {hiddenIds.length > 0 ? (
          <div className="hidden" aria-hidden>
            {hiddenIds.map((id) => (
              <PtySlot key={id} leafId={id} />
            ))}
          </div>
        ) : null}
      </div>
      {leafIds.map((leafId) => {
        const pane = panes[leafId];
        if (!pane) return null;
        return (
          <PtyPortal key={leafId} leafId={leafId}>
            <TerminalPaneView
              pane={pane}
              slug={slug}
              active={activeLeafId === leafId}
              canClose={paneCount > 1}
              canExpand={paneCount > 1}
              expanded={expandedLeafId === leafId}
              launcherRows={launcherRows}
              hasMachine={hasMachine}
              onFocus={() => onFocus(leafId)}
              onSplitRight={() => onSplitRight(leafId)}
              onSplitDown={() => onSplitDown(leafId)}
              onClose={() => onClose(leafId)}
              onToggleExpand={() => onToggleExpand(leafId)}
              onEqualize={() => onEqualize(leafId)}
              onRename={(title) => onRename(leafId, title)}
              onOpenShell={(projectId) => onOpenShell(leafId, projectId)}
              onStartAgent={(projectId, agentId) => onStartAgent(leafId, projectId, agentId)}
            />
          </PtyPortal>
        );
      })}
    </SplitDragContext.Provider>
  );
}
