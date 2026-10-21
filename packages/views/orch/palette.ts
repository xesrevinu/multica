"use client";

import { useEffect } from "react";
import type { LucideIcon } from "lucide-react";
import { create } from "zustand";

// Command-palette (⌘K) items contributed by the orchestrator views. The
// issue chat registers its actions while mounted; the sidebar contributes
// "needs you" jumps. SearchCommand reads them through useOrchPaletteItems.
export type OrchPaletteItem = {
  key: string;
  label: string;
  icon: LucideIcon;
  keywords: string[];
  onSelect: () => void;
};

type State = {
  sources: Record<string, OrchPaletteItem[]>;
  set: (source: string, items: OrchPaletteItem[] | null) => void;
};

export const useOrchPaletteStore = create<State>((set) => ({
  sources: {},
  set: (source, items) =>
    set((s) => {
      const next = { ...s.sources };
      if (items && items.length > 0) next[source] = items;
      else delete next[source];
      return { sources: next };
    }),
}));

const EMPTY: OrchPaletteItem[] = [];
let cache: { sources: State["sources"]; flat: OrchPaletteItem[] } | null = null;
function flatten(sources: State["sources"]): OrchPaletteItem[] {
  if (cache?.sources === sources) return cache.flat;
  const flat = Object.values(sources).flat();
  cache = { sources, flat: flat.length ? flat : EMPTY };
  return cache.flat;
}

export function useOrchPaletteItems(): OrchPaletteItem[] {
  return useOrchPaletteStore((s) => flatten(s.sources));
}

// Register `items` under `source` while the caller is mounted.
export function useRegisterOrchPalette(source: string, items: OrchPaletteItem[]) {
  const set = useOrchPaletteStore((s) => s.set);
  useEffect(() => {
    set(source, items);
  }, [set, source, items]);
  useEffect(() => () => set(source, null), [set, source]);
}
