import { describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import type { RuntimeMachine } from "./runtime-machines";
import { MachineRail } from "./machine-rail";

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    runtimeDetail: (id: string) => `/acme/runtimes/${id}`,
  }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({
    href,
    children,
    ...props
  }: {
    href: string;
    children: React.ReactNode;
    "aria-label"?: string;
  }) => (
    <a href={href} aria-label={props["aria-label"]}>
      {children}
    </a>
  ),
}));

function makeMachine(
  overrides: Partial<RuntimeMachine> = {},
): RuntimeMachine {
  return {
    id: "machine-1",
    daemonId: "daemon-1",
    title: "dev.local",
    subtitle: "arm64 macOS",
    deviceInfo: "dev.local · arm64 macOS",
    cliVersion: "1.0.0",
    launchedBy: null,
    mode: "local",
    section: "local",
    isCurrent: true,
    health: "online",
    runtimes: [{ id: "rt-1" } as RuntimeMachine["runtimes"][number]],
    onlineCount: 1,
    issueCount: 0,
    runningCount: 0,
    queuedCount: 0,
    providerNames: ["claude"],
    lastSeenAt: "2026-05-17T11:59:50Z",
    ...overrides,
  };
}

describe("MachineRail", () => {
  it("selects a machine to filter the fused agent list", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    renderWithI18n(
      <MachineRail
        machines={[makeMachine()]}
        selectedId={null}
        onSelect={onSelect}
        onConnectRemote={vi.fn()}
      />,
    );

    expect(screen.getByRole("button", { name: /All machines/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    await user.click(screen.getByRole("button", { name: /dev.local/ }));
    expect(onSelect).toHaveBeenCalledWith("machine-1");
  });

  it("links through to the machine detail without replacing the filter chip", () => {
    renderWithI18n(
      <MachineRail
        machines={[makeMachine()]}
        selectedId="machine-1"
        onSelect={vi.fn()}
        onConnectRemote={vi.fn()}
      />,
    );

    expect(screen.getByRole("link", { name: "Open dev.local" })).toHaveAttribute(
      "href",
      "/acme/runtimes/machine-1",
    );
  });
});
