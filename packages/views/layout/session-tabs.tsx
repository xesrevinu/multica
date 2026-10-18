"use client";

import {
  createContext,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";

/**
 * Marks the subtree that actually renders the session tab strip.
 *
 * Pages read `useSessionTabsEnabled()` to drop chrome the strip already shows
 * (the page title above all). That is only true where the strip exists: a page
 * mounted on its own — an overlay, a test, a future consumer without tabs —
 * must keep its own header, or its title, description, docs link and unread
 * count silently disappear.
 */
const SessionTabsContext = createContext(false);

export function SessionTabsProvider({
  enabled = true,
  children,
}: {
  enabled?: boolean;
  children: ReactNode;
}) {
  return (
    <SessionTabsContext.Provider value={enabled}>
      {children}
    </SessionTabsContext.Provider>
  );
}

/**
 * Session tabs (the in-app document strip) only exist above the compact
 * breakpoint, and only under `SessionTabsProvider`. Until the viewport is known,
 * treat them as off so a phone first-paint cannot flash a tab bar. See
 * docs/adr/0001-web-session-tabs.md.
 */
export function useSessionTabsEnabled(): boolean {
  const provided = useContext(SessionTabsContext);
  const compact = useIsCompact();
  const [ready, setReady] = useState(false);
  useEffect(() => {
    setReady(true);
  }, []);
  return provided && ready && !compact;
}
