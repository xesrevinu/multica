"use client";

import { createContext, useContext } from "react";

export const SplitDragContext = createContext(false);

export function useSplitDragging(): boolean {
  return useContext(SplitDragContext);
}
