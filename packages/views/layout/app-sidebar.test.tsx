import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";

vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog([]),
}));
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { renderWithI18n } from "../test/i18n";
import { AppSidebar } from "./app-sidebar";

// The live-work section owns its own queries and suite; the sidebar suite
// asserts nav, pins, and badges, so the section is stubbed out here rather
// than dragging a QueryClient through every case.
vi.mock("./active-work-section", () => ({ ActiveWorkSection: () => null }));

const { appForeground, chatSessions, chatStore, detail, deletePin, invitationApi, navigation, pins, sidebarState, summary, workspaces } = vi.hoisted(() => ({
  appForeground: { current: true },
  sidebarState: { setOpenMobile: vi.fn() },
  chatSessions: { current: [] as { id?: string; unread_count?: number }[] },
  chatStore: { current: { activeSessionId: null as string | null, isOpen: false } },
  detail: { current: { isPending: false, isError: false, data: null as unknown, error: null as unknown } },
  deletePin: vi.fn(),
  // Captures the sidebar's invitation accept/decline mutations so the
  // self-heal wiring (error → invalidate the pending list) is observable.
  invitationApi: {
    accept: vi.fn(),
    decline: vi.fn(),
    invalidateQueries: vi.fn(),
    mutations: [] as Array<Record<string, unknown>>,
  },
  navigation: { current: { pathname: "/acme/issues" } },
  summary: { current: [] as { workspace_id: string; count: number }[] },
  workspaces: {
    current: [] as { id: string; name: string; slug: string; avatar_url: string | null }[],
  },
  pins: {
    current: [
      {
        id: "pin-1",
        workspace_id: "ws-1",
        user_id: "user-1",
        item_type: "issue" as const,
        item_id: "issue-1",
        position: 0,
        created_at: "2026-05-06T00:00:00Z",
      },
    ],
  },
}));

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PointerSensor: vi.fn(),
  closestCenter: vi.fn(),
  useSensor: vi.fn(),
  useSensors: vi.fn(),
}));
vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useSortable: () => ({ attributes: {}, listeners: {}, setNodeRef: vi.fn() }),
  verticalListSortingStrategy: vi.fn(),
}));
vi.mock("@dnd-kit/utilities", () => ({ CSS: { Transform: { toString: () => undefined } } }));
vi.mock("@multica/ui/components/ui/sidebar", () => ({
  Sidebar: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarFooter: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarGroup: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarGroupLabel: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarHeader: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarMenuButton: ({
    children,
    isActive,
    render,
    ...props
  }: {
    children: React.ReactNode;
    isActive?: boolean;
    render?: React.ReactElement<{ href?: string }>;
  } & React.ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button {...props} type="button" data-active={isActive ? "true" : undefined} data-href={render?.props.href}>
      {children}
    </button>
  ),
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarRail: () => null,
  useSidebar: () => ({ setOpenMobile: sidebarState.setOpenMobile }),
}));
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuSeparator: () => null,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
}));
vi.mock("@multica/ui/components/ui/collapsible", () => ({
  Collapsible: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  CollapsibleContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  CollapsibleTrigger: () => <button type="button" />,
}));
vi.mock("@multica/ui/components/ui/tooltip", async () => {
  const React = await import("react");
  return {
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children, render }: { children: React.ReactNode; render?: React.ReactElement }) =>
    render ? React.cloneElement(render, undefined, children) : <button type="button">{children}</button>,
};
});
vi.mock("../common/use-app-foreground", () => ({
  useAppForeground: () => appForeground.current,
}));
vi.mock("./help-launcher", () => ({ HelpLauncher: () => null }));
vi.mock("../auth", () => ({ useLogout: () => vi.fn() }));
vi.mock("../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("../navigation", () => ({
  AppLink: ({ children, href, ...rest }: { children?: React.ReactNode; href: string } & Record<string, unknown>) => (
    <a href={href} {...(rest as React.AnchorHTMLAttributes<HTMLAnchorElement>)}>{children}</a>
  ),
  useNavigation: () => ({ pathname: navigation.current.pathname, push: vi.fn(), searchParams: new URLSearchParams() }),
}));
// The issue-centric sidebar sections have their own tests; here they only
// expose the props AppSidebar hands them.
vi.mock("../orch", () => ({
  AgentDmSection: (props: { viewedSessionId: string | null; chatSessions: unknown[] }) => (
    <div data-testid="dm-section" data-viewed={props.viewedSessionId ?? ""} data-sessions={props.chatSessions.length} />
  ),
  IssueAttentionList: () => <div data-testid="issue-list" />,
}));
vi.mock("@multica/core/orchestrator", () => ({ orchIssuesOptions: () => ({ queryKey: ["orch"] }) }));
vi.mock("../projects/components/project-icon", () => ({ ProjectIcon: () => <span /> }));
vi.mock("../workspace/workspace-avatar", () => ({ WorkspaceAvatar: () => <span /> }));
vi.mock("@multica/ui/components/common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "user-1" } }),
}));
// Callable-store shape (selectorFn + getState) per the repo testing rules.
vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (state: { activeSessionId: string | null; isOpen: boolean }) => unknown) =>
      selector(chatStore.current),
    { getState: () => chatStore.current },
  ),
}));
vi.mock("@multica/core/paths", async (importOriginal) => ({
  // Spread the real module so pure helpers (resolveRouteIconName, used by the
  // nav to derive each item's icon from its href) stay intact; only the
  // workspace/context hooks below are stubbed to control routes in tests.
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  paths: { workspace: (slug: string) => ({ issues: () => `/${slug}/issues`, root: () => `/${slug}/chat` }) },
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({
    inbox: () => "/acme/inbox",
    chat: () => "/acme/chat",
    myIssues: () => "/acme/my-issues",
    issues: () => "/acme/issues",
    projects: () => "/acme/projects",
    autopilots: () => "/acme/autopilots",
    agents: () => "/acme/agents",
    usage: () => "/acme/usage",
    runtimes: () => "/acme/runtimes",
    skills: () => "/acme/skills",
    settings: () => "/acme/settings",
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));
vi.mock("@multica/core/api", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/api")>();
  return {
    ...actual,
    api: {
      ...actual.api,
      getBaseUrl: () => "http://127.0.0.1:8080",
      acceptInvitation: invitationApi.accept,
      declineInvitation: invitationApi.decline,
    },
  };
});
vi.mock("@multica/core/inbox/queries", () => ({
  inboxUnreadSummaryOptions: () => ({ queryKey: ["inbox", "unread-summary"] }),
  // The nav badge and the switcher dot read the SAME cross-workspace summary,
  // so the fixture that drives one drives the other.
  useInboxUnreadCount: (currentWsId: string | null) =>
    summary.current.find((s) => s.workspace_id === currentWsId)?.count ?? 0,
  hasOtherWorkspaceUnread: (
    entries: { workspace_id: string; count: number }[],
    currentWsId: string | null,
  ) => entries.some((s) => s.workspace_id !== currentWsId && s.count > 0),
  unreadWorkspaceIds: (entries: { workspace_id: string; count: number }[]) =>
    new Set(entries.filter((s) => s.count > 0).map((s) => s.workspace_id)),
}));
vi.mock("@multica/core/issues/queries", () => ({ issueDetailOptions: () => ({ queryKey: ["issue"] }) }));
vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  useCreateModeStore: { getState: () => ({ lastMode: "agent" }) },
  openCreateIssueWithPreference: vi.fn(),
}));
vi.mock("@multica/core/issues/stores/draft-store", () => ({ useIssueDraftStore: () => false }));
vi.mock("@multica/core/modals", () => ({ useModalStore: { getState: () => ({ modal: null, open: vi.fn() }) } }));
vi.mock("@multica/core/pins/mutations", () => ({ useDeletePin: () => ({ mutate: deletePin }), useReorderPins: () => ({ mutate: vi.fn() }) }));
vi.mock("@multica/core/pins/queries", () => ({ pinListOptions: () => ({ queryKey: ["pins"] }) }));
vi.mock("@multica/core/projects/queries", () => ({ projectDetailOptions: () => ({ queryKey: ["project"] }) }));
vi.mock("@multica/core/workspace/queries", () => ({
  myInvitationListOptions: () => ({ queryKey: ["invitations"] }),
  workspaceKeys: { myInvitations: () => ["invitations"] },
  workspaceListOptions: () => ({ queryKey: ["workspaces"] }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useMutation: (options: Record<string, unknown> = {}) => {
    invitationApi.mutations.push(options);
    return { isPending: false, mutate: vi.fn() };
  },
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    if (queryKey[0] === "pins") return { data: pins.current };
    if (queryKey[0] === "issue") return detail.current;
    if (queryKey[0] === "inbox" && queryKey[1] === "unread-summary") return { data: summary.current };
    if (queryKey[0] === "workspaces") return { data: workspaces.current };
    if (queryKey[0] === "chat" && queryKey[2] === "sessions") return { data: chatSessions.current };
    return { data: [] };
  },
  useQueryClient: () => ({ fetchQuery: vi.fn(), invalidateQueries: invitationApi.invalidateQueries }),
}));

describe("PinRow", () => {
  beforeEach(() => {
    deletePin.mockReset();
    navigation.current.pathname = "/acme/issues";
    detail.current = { isPending: false, isError: false, data: null, error: null };
    summary.current = [];
    workspaces.current = [];
  });

  it("unpins missing details", async () => {
    detail.current = { isPending: false, isError: true, data: null, error: new ApiError("missing", 404, "Not Found") };
    render(<AppSidebar />);
    await waitFor(() => expect(deletePin).toHaveBeenCalledTimes(1));
  });

  it("ignores non-404 errors", async () => {
    detail.current = { isPending: false, isError: true, data: null, error: new ApiError("error", 500, "Server Error") };
    render(<AppSidebar />);
    await waitFor(() => expect(deletePin).not.toHaveBeenCalled());
  });

  it("renders loaded details", async () => {
    detail.current = { isPending: false, isError: false, data: { identifier: "MUL-123", title: "Keep this pin", status: "todo" }, error: null };
    render(<AppSidebar />);
    expect(await screen.findByText("Keep this pin")).toBeInTheDocument();
    expect(screen.queryByText("MUL-123 Keep this pin")).not.toBeInTheDocument();
  });

  it("does not also highlight the board/list nav for an active pin", async () => {
    navigation.current.pathname = "/acme/issues/issue-1";
    detail.current = {
      isPending: false,
      isError: false,
      data: { identifier: "MUL-123", title: "Keep this pin", status: "todo" },
      error: null,
    };

    const { container } = render(<AppSidebar />);

    expect((await screen.findByText("Keep this pin")).closest("button")).toHaveAttribute(
      "data-active",
      "true",
    );
    expect(container.querySelector('a[href="/acme/issues"]')).not.toHaveAttribute("data-active");
  });

  it("reveals hidden pins on demand", () => {
    const originalPins = pins.current;
    pins.current = Array.from({ length: 6 }, (_, index) => ({
      ...originalPins[0]!,
      id: `pin-${index + 1}`,
      item_id: `issue-${index + 1}`,
      position: index,
    }));
    navigation.current.pathname = "/acme/issues/issue-6";
    detail.current = {
      isPending: false,
      isError: false,
      data: { identifier: "MUL-123", title: "Pinned issue", status: "todo" },
      error: null,
    };

    try {
      const { container } = renderWithI18n(<AppSidebar />);
      const lastPin = () => container.querySelector('button[data-href="/acme/issues/issue-6"]');

      expect(lastPin()).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole("button", { name: "Show 1 more…" }));
      expect(lastPin()).toHaveAttribute("data-active", "true");
      fireEvent.click(screen.getByRole("button", { name: "Show fewer" }));
      expect(lastPin()).not.toBeInTheDocument();
    } finally {
      pins.current = originalPins;
    }
  });
});

// On a phone the sidebar is a Sheet laid over the page, so a nav tap that
// leaves it open renders the destination underneath and reads as a dead tap.
describe("mobile sheet dismissal", () => {
  beforeEach(() => {
    sidebarState.setOpenMobile.mockClear();
    navigation.current = { pathname: "/acme/issues" };
  });

  it("dismisses the sheet once the route changes", () => {
    const { rerender } = render(<AppSidebar />);
    sidebarState.setOpenMobile.mockClear();

    navigation.current = { pathname: "/acme/inbox" };
    rerender(<AppSidebar />);

    expect(sidebarState.setOpenMobile).toHaveBeenCalledWith(false);
  });

  // Closing on `pathname` rather than per-link keeps every route out of the
  // sidebar covered at once — nav groups, pins, and the switcher's own push.
  it("does not re-dismiss while the route holds still", () => {
    const { rerender } = render(<AppSidebar />);
    sidebarState.setOpenMobile.mockClear();

    rerender(<AppSidebar />);

    expect(sidebarState.setOpenMobile).not.toHaveBeenCalled();
  });
});

describe("workspace-switcher unread dot", () => {
  beforeEach(() => {
    summary.current = [];
    workspaces.current = [];
  });

  // The aggregate switcher dot is the only `.ring-sidebar` span in the tree
  // (DraftDot is null when there's no draft, and there are no invitations).
  const dot = (container: HTMLElement) => container.querySelector("span.bg-brand.ring-sidebar");

  it("shows a dot when another workspace has unread inbox items", () => {
    summary.current = [{ workspace_id: "ws-2", count: 3 }];
    const { container } = render(<AppSidebar />);
    expect(dot(container)).not.toBeNull();
  });

  it("does not show a dot when only the active workspace has unread", () => {
    // Active workspace is ws-1 (see useCurrentWorkspace mock).
    summary.current = [{ workspace_id: "ws-1", count: 3 }];
    const { container } = render(<AppSidebar />);
    expect(dot(container)).toBeNull();
  });

  it("does not show a dot when no workspace has unread", () => {
    summary.current = [];
    const { container } = render(<AppSidebar />);
    expect(dot(container)).toBeNull();
  });
});

describe("workspace-switcher dropdown per-workspace dot", () => {
  beforeEach(() => {
    summary.current = [];
    // Active workspace is ws-1 (see useCurrentWorkspace mock); "Other" is ws-2.
    workspaces.current = [
      { id: "ws-1", name: "Active WS", slug: "active", avatar_url: null },
      { id: "ws-2", name: "Other WS", slug: "other", avatar_url: null },
    ];
  });

  // Row dots are brand dots WITHOUT the aggregate avatar dot's `ring-sidebar`.
  // The footer Inbox dot (active workspace unread) is excluded too.
  const rowDots = (container: HTMLElement) =>
    container.querySelectorAll("span.bg-brand:not(.ring-sidebar):not([data-testid=inbox-unread-dot])");

  it("dots the specific other workspace that has unread", () => {
    summary.current = [{ workspace_id: "ws-2", count: 3 }];
    const { container } = render(<AppSidebar />);
    // Exactly one row dot, sitting right after the "Other WS" name; the active
    // row shows the check, not a dot.
    expect(rowDots(container)).toHaveLength(1);
    expect(screen.getByText("Other WS").nextElementSibling?.className).toContain("bg-brand");
    expect(screen.getByText("Active WS").nextElementSibling?.className ?? "").not.toContain("bg-brand");
  });

  it("does not dot a workspace whose unread count is zero", () => {
    summary.current = [{ workspace_id: "ws-2", count: 0 }];
    const { container } = render(<AppSidebar />);
    expect(rowDots(container)).toHaveLength(0);
  });

  it("never dots the active workspace even when it has unread", () => {
    summary.current = [{ workspace_id: "ws-1", count: 5 }];
    const { container } = render(<AppSidebar />);
    expect(rowDots(container)).toHaveLength(0);
  });
});

describe("secondary nav strip", () => {
  beforeEach(() => {
    summary.current = [];
    navigation.current = { pathname: "/acme/issues" };
  });

  it("links every secondary surface from the footer strip", () => {
    const { getByTestId } = render(<AppSidebar />);
    const strip = getByTestId("orch-secondary-nav");
    for (const href of ["/acme/inbox", "/acme/issues", "/acme/projects", "/acme/autopilots", "/acme/runtimes", "/acme/skills", "/acme/usage", "/acme/settings"]) {
      expect(strip.querySelector(`a[href="${href}"]`)).not.toBeNull();
    }
  });

  it("dots Inbox when the active workspace has unread items", () => {
    summary.current = [{ workspace_id: "ws-1", count: 2 }];
    const { getByTestId } = render(<AppSidebar />);
    expect(getByTestId("inbox-unread-dot")).toBeInTheDocument();
  });
});

describe("agent DMs — viewed session", () => {
  beforeEach(() => {
    chatSessions.current = [];
    navigation.current = { pathname: "/acme/issues" };
    chatStore.current = { activeSessionId: null, isOpen: false };
    appForeground.current = true;
  });

  const viewed = (container: HTMLElement) => container.querySelector('[data-testid="dm-section"]')?.getAttribute("data-viewed");

  it("passes the chat sessions to the DM section", () => {
    chatSessions.current = [{ id: "a", unread_count: 1 }];
    const { container } = render(<AppSidebar />);
    expect(container.querySelector('[data-testid="dm-section"]')).toHaveAttribute("data-sessions", "1");
  });

  it("marks the session read on the chat page as viewed", () => {
    navigation.current = { pathname: "/acme/chat" };
    chatStore.current = { activeSessionId: "a", isOpen: false };
    expect(viewed(render(<AppSidebar />).container)).toBe("a");
  });

  it("marks the floating window's session as viewed off-route", () => {
    chatStore.current = { activeSessionId: "a", isOpen: true };
    expect(viewed(render(<AppSidebar />).container)).toBe("a");
  });

  it("does not treat a remembered selection as viewed", () => {
    chatStore.current = { activeSessionId: "a", isOpen: false };
    expect(viewed(render(<AppSidebar />).container)).toBe("");
  });

  it("does not treat a backgrounded app as viewing", () => {
    navigation.current = { pathname: "/acme/chat" };
    chatStore.current = { activeSessionId: "a", isOpen: false };
    appForeground.current = false;
    expect(viewed(render(<AppSidebar />).container)).toBe("");
  });
});

describe("Pending invitation self-heal", () => {
  beforeEach(() => {
    invitationApi.accept.mockReset();
    invitationApi.decline.mockReset();
    invitationApi.invalidateQueries.mockClear();
    invitationApi.mutations.length = 0;
    invitationApi.accept.mockRejectedValue(new Error("invitation is not pending"));
    invitationApi.decline.mockRejectedValue(new Error("invitation is not pending"));
    navigation.current.pathname = "/acme/issues";
    workspaces.current = [];
  });

  // "invitation is not pending" means the row on screen was concluded from
  // another surface. Both mutations must invalidate the pending list on
  // failure so the stale row drops instead of surviving until restart.
  it("invalidates the pending-invitations list when accept or decline fails", async () => {
    render(<AppSidebar />);
    expect(invitationApi.mutations).toHaveLength(2);

    for (const options of invitationApi.mutations as Array<{
      mutationFn: (id: string) => Promise<unknown>;
      onError?: (...args: unknown[]) => unknown;
      onSettled?: (...args: unknown[]) => unknown;
    }>) {
      const settle = options.onError ?? options.onSettled;
      expect(settle).toBeTypeOf("function");
      invitationApi.invalidateQueries.mockClear();
      await expect(options.mutationFn("inv-1")).rejects.toThrow("invitation is not pending");
      await settle!(new Error("invitation is not pending"), "inv-1", undefined, undefined);
      expect(invitationApi.invalidateQueries).toHaveBeenCalledWith({ queryKey: ["invitations"] });
    }
    expect(invitationApi.accept).toHaveBeenCalledTimes(1);
    expect(invitationApi.decline).toHaveBeenCalledTimes(1);
  });
});

describe("sidebar nav groups", () => {
  beforeEach(() => {
    navigation.current.pathname = "/acme/issues";
  });

  it("drops the standalone agents row and keeps agents under runtimes", () => {
    const { container } = render(<AppSidebar />);
    expect(container.querySelector('a[href="/acme/agents"]')).toBeNull();
    expect(container.querySelector('a[href="/acme/runtimes"]')).not.toBeNull();
  });

  it("highlights runtimes while the merged agents surface is open", () => {
    navigation.current.pathname = "/acme/agents";
    const { container } = render(<AppSidebar />);
    expect(container.querySelector('a[href="/acme/runtimes"]')).toHaveAttribute("data-active", "true");
  });

  it("renders the agent DMs and the issue list", () => {
    const { getByTestId } = render(<AppSidebar />);
    expect(getByTestId("dm-section")).toBeInTheDocument();
    expect(getByTestId("issue-list")).toBeInTheDocument();
  });
});
