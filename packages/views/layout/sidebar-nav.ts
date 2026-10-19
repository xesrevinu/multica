// Nav items reference WorkspacePaths method names so they can be resolved
// against the current workspace slug at render time (see AppSidebar body).
// Only parameterless paths are valid nav destinations.
export type SidebarNavKey =
  | "inbox"
  | "chat"
  | "myIssues"
  | "issues"
  | "projects"
  | "autopilots"
  | "agents"
  | "squads"
  | "usage"
  | "runtimes"
  | "skills"
  | "settings";

// Static schema (key only) — labels resolved at render via useT("layout"),
// icons derived from the destination path via routeIconForPath.
export type SidebarNavLabelKey =
  | "inbox"
  | "chat"
  | "my_issues"
  | "issues"
  | "projects"
  | "autopilots"
  | "agents"
  | "squads"
  | "usage"
  | "runtimes"
  | "skills"
  | "settings";

export type SidebarNavItem = {
  key: SidebarNavKey;
  labelKey: SidebarNavLabelKey;
};

// Nav icons are NOT declared here: they are derived from each item's
// destination path at render time, so the sidebar and the desktop tab bar
// always agree. See route-icon-components.tsx.
export const personalNav: SidebarNavItem[] = [
  { key: "inbox", labelKey: "inbox" },
  { key: "chat", labelKey: "chat" },
];

export const workNav: SidebarNavItem[] = [
  { key: "issues", labelKey: "issues" },
  { key: "projects", labelKey: "projects" },
  { key: "autopilots", labelKey: "autopilots" },
];

// Agents live on the Runtimes page (see AgentsRuntimesPage). Upstream's
// AI Team group otherwise keeps squads / skills / runtimes.
export const aiTeamNav: SidebarNavItem[] = [
  { key: "runtimes", labelKey: "runtimes" },
  { key: "squads", labelKey: "squads" },
  { key: "skills", labelKey: "skills" },
];

export const utilityNav: SidebarNavItem[] = [
  { key: "usage", labelKey: "usage" },
  { key: "settings", labelKey: "settings" },
];

// Issue-centric layout (docs/orchestrator/ui-redesign.md): the sidebar body is
// agent DMs + the attention-sorted issue list, so every other surface lives in
// this footer icon strip. "issues" is the board/list/table views page.
export const secondaryNav: SidebarNavItem[] = [
  { key: "inbox", labelKey: "inbox" },
  { key: "issues", labelKey: "issues" },
  { key: "projects", labelKey: "projects" },
  { key: "autopilots", labelKey: "autopilots" },
  { key: "runtimes", labelKey: "runtimes" },
  { key: "squads", labelKey: "squads" },
  { key: "skills", labelKey: "skills" },
  { key: "usage", labelKey: "usage" },
  { key: "settings", labelKey: "settings" },
];
