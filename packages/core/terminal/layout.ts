/** Orca-compatible split axes: vertical = side-by-side, horizontal = stacked. */
export type PtySplitDirection = "horizontal" | "vertical";

export type PtyLayoutLeaf = { type: "leaf"; id: string };

export type PtyLayoutSplit = {
  type: "split";
  id: string;
  direction: PtySplitDirection;
  /** Flex share of `first` in 0–1. Defaults to 0.5. */
  ratio: number;
  first: PtyLayoutNode;
  second: PtyLayoutNode;
};

export type PtyLayoutNode = PtyLayoutLeaf | PtyLayoutSplit;

export function collectLeafIds(node: PtyLayoutNode): string[] {
  if (node.type === "leaf") return [node.id];
  return [...collectLeafIds(node.first), ...collectLeafIds(node.second)];
}

export function leafCount(node: PtyLayoutNode): number {
  if (node.type === "leaf") return 1;
  return leafCount(node.first) + leafCount(node.second);
}

export function adjacentLeaf(root: PtyLayoutNode, leafId: string, delta: number): string {
  const ids = collectLeafIds(root);
  const index = ids.indexOf(leafId);
  if (index < 0 || ids.length === 0) return leafId;
  return ids[(index + delta + ids.length) % ids.length];
}

/** Weight nested same-axis splits so 3 side-by-side panes become thirds, not 50/25/25. */
export function equalizeTree(root: PtyLayoutNode): PtyLayoutNode {
  if (root.type === "leaf") return root;
  const first = equalizeTree(root.first);
  const second = equalizeTree(root.second);
  const left = leafCount(first);
  const right = leafCount(second);
  const total = left + right;
  return {
    ...root,
    first,
    second,
    ratio: clampSplitRatio(total === 0 ? 0.5 : left / total),
  };
}

export function clampSplitRatio(ratio: number): number {
  if (!Number.isFinite(ratio)) return 0.5;
  return Math.min(0.92, Math.max(0.08, ratio));
}

export function splitLeaf(
  root: PtyLayoutNode,
  leafId: string,
  direction: PtySplitDirection,
  newLeafId: string,
  splitId: string,
): PtyLayoutNode {
  if (root.type === "leaf") {
    if (root.id !== leafId) return root;
    return {
      type: "split",
      id: splitId,
      direction,
      ratio: 0.5,
      first: root,
      second: { type: "leaf", id: newLeafId },
    };
  }
  return {
    ...root,
    first: splitLeaf(root.first, leafId, direction, newLeafId, splitId),
    second: splitLeaf(root.second, leafId, direction, newLeafId, splitId),
  };
}

export function closeLeaf(root: PtyLayoutNode, leafId: string): PtyLayoutNode | null {
  if (root.type === "leaf") {
    return root.id === leafId ? null : root;
  }
  const first = closeLeaf(root.first, leafId);
  const second = closeLeaf(root.second, leafId);
  if (first && second) {
    return { ...root, first, second };
  }
  return first ?? second;
}

export function setSplitRatio(
  root: PtyLayoutNode,
  splitId: string,
  ratio: number,
): PtyLayoutNode {
  if (root.type === "leaf") return root;
  if (root.id === splitId) {
    return { ...root, ratio: clampSplitRatio(ratio) };
  }
  return {
    ...root,
    first: setSplitRatio(root.first, splitId, ratio),
    second: setSplitRatio(root.second, splitId, ratio),
  };
}

export function cloneLayout(node: PtyLayoutNode): {
  node: PtyLayoutNode;
  idMap: Record<string, string>;
} {
  const idMap: Record<string, string> = {};
  const walk = (current: PtyLayoutNode): PtyLayoutNode => {
    if (current.type === "leaf") {
      const id = crypto.randomUUID();
      idMap[current.id] = id;
      return { type: "leaf", id };
    }
    return {
      type: "split",
      id: crypto.randomUUID(),
      direction: current.direction,
      ratio: current.ratio,
      first: walk(current.first),
      second: walk(current.second),
    };
  };
  return { node: walk(node), idMap };
}

export function parentSplitOf(
  root: PtyLayoutNode,
  leafId: string,
): PtyLayoutSplit | null {
  if (root.type === "leaf") return null;
  if (
    (root.first.type === "leaf" && root.first.id === leafId) ||
    (root.second.type === "leaf" && root.second.id === leafId)
  ) {
    return root;
  }
  return parentSplitOf(root.first, leafId) ?? parentSplitOf(root.second, leafId);
}
