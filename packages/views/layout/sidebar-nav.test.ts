import { describe, expect, it } from "vitest";
import { aiTeamNav, personalNav, utilityNav, workNav } from "./sidebar-nav";

describe("sidebar nav groups", () => {
  it("keeps autopilots with issues and projects in the work group", () => {
    expect(workNav.map((item) => item.key)).toEqual([
      "issues",
      "projects",
      "autopilots",
    ]);
  });

  it("puts runtimes with squads and skills in the AI team group", () => {
    expect(aiTeamNav.map((item) => item.key)).toEqual([
      "runtimes",
      "squads",
      "skills",
    ]);
  });

  it("keeps usage and settings in the utility strip", () => {
    expect(utilityNav.map((item) => item.key)).toEqual(["usage", "settings"]);
  });

  it("does not give agents or my-issues their own sidebar row", () => {
    const keys = [...personalNav, ...workNav, ...aiTeamNav, ...utilityNav].map(
      (item) => item.key,
    );
    expect(keys).not.toContain("agents");
    expect(keys).not.toContain("myIssues");
    expect(personalNav.map((item) => item.key)).toEqual(["inbox", "chat"]);
  });
});
