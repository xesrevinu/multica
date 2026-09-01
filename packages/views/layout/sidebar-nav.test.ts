import { describe, expect, it } from "vitest";
import { configureNav, personalNav, workspaceNav } from "./sidebar-nav";

describe("sidebar nav groups", () => {
  it("keeps autopilots with issues and projects in the workspace group", () => {
    expect(workspaceNav.map((item) => item.key)).toEqual([
      "issues",
      "projects",
      "autopilots",
    ]);
  });

  it("moves squads and usage into configure, with agents under runtimes", () => {
    expect(configureNav.map((item) => item.key)).toEqual([
      "runtimes",
      "squads",
      "usage",
      "skills",
      "settings",
    ]);
  });

  it("does not give agents or my-issues their own sidebar row", () => {
    const keys = [...personalNav, ...workspaceNav, ...configureNav].map(
      (item) => item.key,
    );
    expect(keys).not.toContain("agents");
    expect(keys).not.toContain("myIssues");
    expect(personalNav.map((item) => item.key)).toEqual(["inbox", "chat"]);
  });
});
