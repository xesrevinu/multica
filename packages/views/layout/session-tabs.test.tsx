// @vitest-environment jsdom
import { describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { SessionTabsProvider, useSessionTabsEnabled } from "./session-tabs";

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsCompact: () => mock.compact,
}));

const mock = vi.hoisted(() => ({ compact: false }));

describe("useSessionTabsEnabled", () => {
  it("turns on after mount when the strip is mounted and the viewport is not compact", async () => {
    mock.compact = false;
    const { result } = renderHook(() => useSessionTabsEnabled(), {
      wrapper: SessionTabsProvider,
    });
    await waitFor(() => expect(result.current).toBe(true));
  });

  it("stays off on compact after mount", async () => {
    mock.compact = true;
    const { result } = renderHook(() => useSessionTabsEnabled(), {
      wrapper: SessionTabsProvider,
    });
    await waitFor(() => expect(result.current).toBe(false));
  });

  // A page rendered outside the strip (an overlay, a test, a future consumer)
  // keeps its own header: nothing above it would name the page otherwise.
  it("stays off where no strip is mounted", async () => {
    mock.compact = false;
    const { result } = renderHook(() => useSessionTabsEnabled());
    await waitFor(() => expect(result.current).toBe(false));
  });

  it("stays off when the shell opts out explicitly", async () => {
    mock.compact = false;
    const { result } = renderHook(() => useSessionTabsEnabled(), {
      wrapper: ({ children }) => (
        <SessionTabsProvider enabled={false}>{children}</SessionTabsProvider>
      ),
    });
    await waitFor(() => expect(result.current).toBe(false));
  });
});
