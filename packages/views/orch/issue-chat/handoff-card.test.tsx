import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../i18n", () => ({
  useT: () => ({ t: (f: (x: any) => string, o?: Record<string, unknown>) => {
    const key = f(new Proxy({}, { get: (_t, a) => new Proxy({}, { get: (_u, b) => `${String(a)}.${String(b)}` }) }));
    return o ? `${key}:${Object.values(o).join(",")}` : key;
  } }),
  useLocale: () => "en",
  useTimeAgo: () => () => "now",
}));

import { HandoffCard } from "./timeline-parts";

describe("HandoffCard", () => {
  it("shows kind, attempt, review findings and failing checks", () => {
    render(
      <HandoffCard
        handoff={{
          kind: "review_changes",
          to: "at-dev-a",
          attempt: 2,
          prev_head: "0123456789",
          summary: "reviewer verdict=changes",
          review_comments: [{ source: "github_inline", path: "src/a.js", line: 3, body: "negative input returns NaN" }],
          checks: [{ name: "test", conclusion: "failure", annotations: ["failure a.test.js:4 expected 3"] }],
        }}
      />,
    );
    expect(screen.getByTestId("orch-handoff")).toBeTruthy();
    expect(screen.getByText(/handoff_title:review changes/)).toBeTruthy();
    expect(screen.getByText(/handoff_attempt:2/)).toBeTruthy();
    expect(screen.getByText("negative input returns NaN")).toBeTruthy();
    expect(screen.getByText("failure a.test.js:4 expected 3")).toBeTruthy();
  });
});
