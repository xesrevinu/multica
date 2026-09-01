"use client";

import {
  createContext,
  useContext,
  useState,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

const TabChromeActionsSlotContext = createContext<HTMLElement | null>(null);

/**
 * Holds the DOM node on the session-tab strip where the active page parks
 * its header actions (New project, filters, …) so they share one chrome
 * row with the tabs instead of a second in-card header.
 */
export function TabChromeActionsProvider({
  children,
}: {
  children: (slotRef: (node: HTMLElement | null) => void) => ReactNode;
}) {
  const [slot, setSlot] = useState<HTMLElement | null>(null);
  return (
    <TabChromeActionsSlotContext.Provider value={slot}>
      {children(setSlot)}
    </TabChromeActionsSlotContext.Provider>
  );
}

export function TabChromeActions({ children }: { children: ReactNode }) {
  const slot = useContext(TabChromeActionsSlotContext);
  if (!slot) return null;
  return createPortal(children, slot);
}
