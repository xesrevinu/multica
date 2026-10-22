import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";

vi.mock("@multica/core/api", () => ({
  api: { rerunIssue: vi.fn().mockRejectedValue(new Error("this issue is an epic: retry the sub-issue")) },
  dispatchReasonCode: () => undefined,
}));
vi.mock("../../i18n", () => ({ useT: () => ({ t: (f: (x: unknown) => unknown) => (String(f(new Proxy({}, { get: (_t, k) => new Proxy({}, { get: (_u, kk) => `${String(k)}.${String(kk)}` }) }))).includes("dismiss") ? "Dismiss" : "retry") }) }));
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: () => null,
  TooltipTrigger: ({ render: r, children, className }: { render: React.ReactElement; children: React.ReactNode; className?: string }) => {
    const el = r as React.ReactElement<Record<string, unknown>>;
    return { ...el, props: { ...el.props, className, children } };
  },
}));

import { RetryRunButton } from "./retry-run-button";

describe("RetryRunButton", () => {
  it("keeps a refused retry's reason visible inline until dismissed", async () => {
    render(<RetryRunButton task={{ id: "t1", status: "failed" } as never} issueId="i1" />);
    fireEvent.click(screen.getByRole("button", { name: "retry" }));
    await waitFor(() => expect(screen.getByRole("alert").textContent).toContain("epic"));
    await new Promise((r) => setTimeout(r, 50));
    expect(screen.getByRole("alert")).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Dismiss" }));
    expect(screen.queryByRole("alert")).toBeNull();
  });
});
