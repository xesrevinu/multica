import { useState, useEffect, useMemo, useRef } from "react";
import { TextAttributes } from "@opentui/core";
import { useKeyboard, useRenderer, useTerminalDimensions } from "@opentui/react";
import { useTheme } from "./context/theme";
import { useDialog } from "./ui/dialog";
import type { DashboardUsageByAgent, Issue, Workspace } from "../api/generated";
import { useQueryClient } from "@tanstack/react-query";
import { inboxKeys } from "@multica/core/inbox/queries";
import { writeWorkspaceID, type Connection } from "../config";
import { setSharedWorkspaceId } from "../api/shared";
import { makeDataClient } from "../data";
import { makeRealtimeClient } from "../realtime";
import { AgentDetail } from "./agent";
import { AutopilotDetail } from "./autopilot";
import { ChatDetail } from "./chat";
import { ContextBar, NavBar } from "./context-bar";
import { FilterBar, nextFilterValue, type FilterGroup } from "./filter-bar";
import {
  openAgentPicker,
  openFilterPicker,
  openGoToPicker,
  openHelpDialog,
  openInboxBatchPicker,
  openProjectPicker,
  openWorkspacePicker,
} from "./dialogs";
import { openComposeDialog, openTextPrompt } from "./compose-dialog";
import { IssueDetail } from "./issue";
import {
  buildIssueListParams,
  ISSUE_MINE_SCOPES,
  ISSUE_PAGE_SIZE,
  type IssueMineScope,
} from "./issue-query";
import { filterInboxItems, INBOX_VIEWS, type InboxView } from "./inbox";
import { useSharedInbox } from "./use-inbox";
import { Overview } from "./overview";
import { SettingsView } from "./settings";
import { SkillDetail } from "./skill";
import { UsageView } from "./usage";
import { SurfaceStrip } from "./surface";
import {
  agentRows,
  autopilotRows,
  orchRows,
  chatRows,
  emptyStore,
  inboxRows,
  issueRows,
  machineSummaries,
  makeLookups,
  projectRows,
  runtimeRows,
  skillRows,
  TAB_HEADERS,
} from "./store";
import {
  defaultWorkspaceView,
  readSessionStateSync,
  sessionStatePath,
  settleWorkspaceView,
  withWorkspace,
  writeSessionState,
  type WorkspaceDetail,
  type WorkspaceView,
} from "./session-state";
import { Table, type Row } from "./table";
import {
  ISSUE_ATTENTION,
  SPACE_VIEWS,
  SURFACES,
  neighborSpaceView,
  neighborSurface,
  spaceViewFromDigit,
  surfaceFromDigit,
  type IssueAttention,
  type SpaceView,
  type Surface,
} from "./nav";

type Detail = WorkspaceDetail;

export function App(props: { connection: Connection }) {
  const theme = useTheme();
  const dialog = useDialog();
  const renderer = useRenderer();
  const dimensions = useTerminalDimensions();
  const sessionPath = sessionStatePath(props.connection.source);
  const sessionRef = useRef(readSessionStateSync(sessionPath));
  const initialView =
    sessionRef.current.workspaces[props.connection.workspaceID] ?? defaultWorkspaceView();
  const quittingRef = useRef(false);
  const writeChainRef = useRef(Promise.resolve());
  const sessionTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const flushSessionRef = useRef(() => Promise.resolve());
  const quit = () => {
    if (quittingRef.current) return;
    quittingRef.current = true;
    void flushSessionRef.current().finally(() => {
      renderer.setTerminalTitle("");
      if (!renderer.isDestroyed) renderer.destroy();
    });
  };

  const [tab, setTab] = useState<Surface>(initialView.surface);
  const [spaceView, setSpaceView] = useState<SpaceView>(initialView.space_view);
  const [issueAttention, setIssueAttention] = useState<IssueAttention>(initialView.issue_attention);

  const goSurface = (next: Surface) => {
    setTab(next);
    setDetail(undefined);
  };

  const goSpaceView = (next: SpaceView) => {
    setTab("space");
    setSpaceView(next);
    setDetail(undefined);
  };

  /** List selection key: space subviews keep their own cursor. */
  const listKey: Surface | SpaceView = tab === "space" ? spaceView : tab;
  const inChats = tab === "space" && spaceView === "chats";
  const inAgents = tab === "space" && spaceView === "agents";
  const inProjects = tab === "space" && spaceView === "projects";
  const inAutopilots = tab === "space" && spaceView === "autopilots";
  const [store, setStore] = useState(emptyStore);
  const [workspaces, setWorkspaces] = useState<Workspace[]>([]);
  const [workspaceID, setWorkspaceID] = useState(props.connection.workspaceID);
  const [error, setError] = useState<string>();
  const [refreshedAt, setRefreshedAt] = useState<number>();
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<Partial<Record<Surface | SpaceView, string>>>({
    ...initialView.selected,
  });
  const [detail, setDetail] = useState<Detail | undefined>(initialView.detail);
  const [now, setNow] = useState(Date.now());

  // Global project context (filters issues; shown in header)
  const [contextProject, setContextProject] = useState<string>(initialView.project_id);
  const [issueStatus, setIssueStatus] = useState<string>(initialView.issue_status);
  const [issueMine, setIssueMine] = useState<IssueMineScope>(initialView.issue_mine);
  const [inboxView, setInboxView] = useState<InboxView>(initialView.inbox_view);
  const inbox = useSharedInbox(workspaceID, inboxView);
  const queryClient = useQueryClient();
  const [chatStatus, setChatStatus] = useState<"active" | "archived">(initialView.chat_status);
  const [agentMachine, setAgentMachine] = useState<string>(initialView.agent_machine);
  const [agentScope, setAgentScope] = useState(initialView.agent_scope);
  const [agentsView, setAgentsView] = useState(initialView.agents_view);

  // Filters for projects
  const [projectStatus, setProjectStatus] = useState(initialView.project_status);
  const [projectPriority, setProjectPriority] = useState(initialView.project_priority);

  const [issuesView, setIssuesView] = useState<readonly Issue[]>([]);
  const [issuesViewTotal, setIssuesViewTotal] = useState(0);
  const [issueOffset, setIssueOffset] = useState(0);
  const [usageRows, setUsageRows] = useState<readonly DashboardUsageByAgent[]>([]);
  const [usageError, setUsageError] = useState<string>();

  const captureView = (): WorkspaceView => ({
    surface: tab,
    space_view: spaceView,
    project_id: contextProject,
    issue_status: issueStatus,
    issue_mine: issueMine,
    issue_attention: issueAttention,
    inbox_view: inboxView,
    chat_status: chatStatus,
    agent_machine: agentMachine,
    agent_scope: agentScope,
    agents_view: agentsView,
    project_status: projectStatus,
    project_priority: projectPriority,
    selected: { ...selected },
    detail: detail,
  });

  const applyView = (view: WorkspaceView) => {
    setTab(view.surface);
    setSpaceView(view.space_view);
    setContextProject(view.project_id);
    setIssueStatus(view.issue_status);
    setIssueMine(view.issue_mine);
    setIssueAttention(view.issue_attention);
    setInboxView(view.inbox_view);
    setChatStatus(view.chat_status);
    setAgentMachine(view.agent_machine);
    setAgentScope(view.agent_scope);
    setAgentsView(view.agents_view);
    setProjectStatus(view.project_status);
    setProjectPriority(view.project_priority);
    setSelected({ ...view.selected });
    setDetail(view.detail);
    setQuery("");
    setSearching(false);
    setIssueOffset(0);
  };

  const persistSession = () => {
    writeChainRef.current = writeChainRef.current
      .catch(() => undefined)
      .then(() => writeSessionState(sessionPath, sessionRef.current))
      .catch((reason) => {
        if (quittingRef.current) return;
        setError(reason instanceof Error ? reason.message : String(reason));
      });
    return writeChainRef.current;
  };
  const scheduleSession = () => {
    if (sessionTimerRef.current) clearTimeout(sessionTimerRef.current);
    sessionTimerRef.current = setTimeout(() => {
      sessionTimerRef.current = undefined;
      void persistSession();
    }, 200);
  };
  flushSessionRef.current = () => {
    sessionRef.current = withWorkspace(sessionRef.current, workspaceID, captureView());
    if (sessionTimerRef.current) clearTimeout(sessionTimerRef.current);
    sessionTimerRef.current = undefined;
    return persistSession();
  };
  useEffect(() => {
    return () => {
      if (sessionTimerRef.current) clearTimeout(sessionTimerRef.current);
    };
  }, []);

  // Rebuilt when the workspace changes; the old client's runtime is disposed.
  const data = useMemo(
    () => makeDataClient(props.connection, workspaceID),
    [props.connection, workspaceID],
  );
  useEffect(() => {
    return () => {
      void data.dispose();
    };
  }, [data]);

  const workspaceSlug = store.workspace?.slug;

  const realtime = useMemo(() => {
    if (!workspaceSlug) return undefined;
    return makeRealtimeClient(props.connection, workspaceSlug);
  }, [props.connection, workspaceSlug]);
  useEffect(() => {
    return () => {
      void realtime?.dispose();
    };
  }, [realtime]);

  const lookups = useMemo(() => makeLookups(store), [store]);

  const spaceRows = useMemo((): Row[] => {
    const current = store;
    const lookup = lookups;
    const at = now;
    const queryText = query.trim().toLowerCase();
    const filterRows = (list: Row[]) =>
      queryText ? list.filter((row) => row.search.includes(queryText)) : list;
    if (spaceView === "projects") {
      return filterRows(
        projectRows(current, lookup, at, {
          status: projectStatus !== "all" ? projectStatus : undefined,
          priority: projectPriority !== "all" ? projectPriority : undefined,
        }),
      );
    }
    if (spaceView === "autopilots") return filterRows(autopilotRows(current, lookup, at));
    if (spaceView === "agents") {
      return filterRows(
        agentsView === "runtimes"
          ? runtimeRows(current, at)
          : agentRows(current, lookup, at, {
              machine: agentMachine,
              scope: agentScope,
            }),
      );
    }
    if (spaceView === "skills") return filterRows(skillRows(current, at));
    if (spaceView === "chats") return filterRows(chatRows(current, lookup, at));
    return [];
  }, [
    store,
    lookups,
    now,
    query,
    spaceView,
    agentsView,
    agentMachine,
    agentScope,
    projectStatus,
    projectPriority,
  ]);

  const issueListRows = useMemo((): Row[] => {
    const current = store;
    const lookup = lookups;
    const at = now;
    const queryText = query.trim().toLowerCase();
    const filterRows = (list: Row[]) =>
      queryText ? list.filter((row) => row.search.includes(queryText)) : list;
    if (issueAttention !== "all") {
      return filterRows(
        orchRows(current, at).filter((row) => {
          const issue = current.orchIssues.find((entry) => entry.id === row.id);
          return issue?.attention.bucket === issueAttention;
        }),
      );
    }
    return filterRows(issueRows(current, lookup, at, { issues: issuesView }));
  }, [store, lookups, now, query, issueAttention, issuesView]);

  const currentRows = useMemo((): Row[] => {
    if (tab === "inbox") {
      const queryText = query.trim().toLowerCase();
      const list = inboxRows(inbox.items, now);
      return queryText ? list.filter((row) => row.search.includes(queryText)) : list;
    }
    if (tab === "issues") return issueListRows;
    if (tab === "space") return spaceRows;
    return [];
  }, [tab, query, inbox.items, now, issueListRows, spaceRows]);

  const selectedId = selected[listKey];

  useEffect(() => {
    const list = currentRows;
    if (
      tab === "overview" ||
      (tab === "space" && (spaceView === "usage" || spaceView === "settings"))
    )
      return;
    // Wait until this workspace's lists are in. An empty list before that would
    // wipe a restored selection, and the next load would snap to the first row.
    if (!refreshedAt || store.workspace?.id !== workspaceID) return;
    const current = selectedId;
    if (list.some((row) => row.id === current)) return;
    const next = list[0]?.id;
    if (next === current) return;
    setSelected((prev) => ({ ...prev, [listKey]: next }));
  }, [
    currentRows,
    tab,
    spaceView,
    listKey,
    refreshedAt,
    store.workspace?.id,
    workspaceID,
    selectedId,
  ]);

  const reloadIssues = async () => {
    const client = data;
    const workspace = workspaceID;
    const result = await client.listIssues(
      buildIssueListParams(store, {
        offset: issueOffset,
        query: query,
        statusCategory: issueStatus,
        projectID: contextProject,
        mine: issueMine,
        userID: store.me?.id,
      }),
    );
    if (workspace !== workspaceID) return;
    setIssuesView(result.issues);
    setIssuesViewTotal(result.total);
  };

  const refresh = async () => {
    const client = data;
    setNow(Date.now());
    const workspace = workspaceID;
    void queryClient.invalidateQueries({ queryKey: inboxKeys.all(workspace) });
    const [
      me,
      ws,
      members,
      statuses,
      issues,
      agents,
      runtimes,
      projects,
      skills,
      working,
      chats,
      autopilots,
      labels,
      orch,
    ] = await Promise.all([
      client.getMe(),
      client.getWorkspace(workspace),
      client.listMembers(workspace),
      client.listIssueStatuses(),
      client.listIssues({ sort_by: "updated_at", sort_direction: "desc", limit: 200 }),
      client.listAgents({ include_archived: true }),
      client.listRuntimes(),
      client.listProjects(),
      client.listSkills(),
      client.listWorkingAgents(),
      client.listChatSessions({ status: chatStatus }),
      client.listAutopilots(),
      client.listLabels({ resource_type: "issue" }),
      client.listOrchIssues({ settledLimit: 30 }),
    ]);
    if (workspace !== workspaceID) return;
    setStore({
      me,
      workspace: ws,
      members,
      statuses: statuses.statuses,
      issues: issues.issues,
      issuesTotal: issues.total,
      agents,
      runtimes,
      projects: projects.projects,
      skills,
      working,
      inbox: [],
      inboxArchived: [],
      chats,
      autopilots: autopilots.autopilots,
      labels: labels.labels,
      orchIssues: orch.issues,
      orchSettledTotal: orch.settledTotal,
    });
    setError(undefined);
    await reloadIssues();
    if (workspace !== workspaceID) return;
    setRefreshedAt(Date.now());
  };
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;

  useEffect(() => {
    if (!store.statuses.length) return;
    if (issueAttention !== "all") return;
    void reloadIssues().catch((reason) =>
      setError(reason instanceof Error ? reason.message : String(reason)),
    );
  }, [
    store.statuses.length,
    store.me?.id,
    issueStatus,
    issueMine,
    issueAttention,
    contextProject,
    issueOffset,
    query,
    data,
  ]);

  useEffect(() => {
    if (!inbox.error) return;
    setError(inbox.error instanceof Error ? inbox.error.message : String(inbox.error));
  }, [inbox.error]);

  const issuePageLabel = (() => {
    const total = issuesViewTotal;
    if (total <= ISSUE_PAGE_SIZE) return undefined;
    const page = Math.floor(issueOffset / ISSUE_PAGE_SIZE) + 1;
    const pages = Math.max(1, Math.ceil(total / ISSUE_PAGE_SIZE));
    return `${page}/${pages} (${total})`;
  })();

  useEffect(() => {
    if (!(tab === "space" && spaceView === "usage")) return;
    const client = data;
    const workspace = workspaceID;
    void client
      .getDashboardUsageByAgent({ days: 7 })
      .then((usage) => {
        if (workspace !== workspaceID) return;
        setUsageRows(usage);
        setUsageError(undefined);
      })
      .catch((reason) => {
        if (workspace !== workspaceID) return;
        setUsageError(reason instanceof Error ? reason.message : String(reason));
      });
  }, [tab, spaceView, data, workspaceID]);

  const prevWorkspaceRef = useRef(workspaceID);
  useEffect(() => {
    const next = workspaceID;
    setSharedWorkspaceId(next);
    const prev = prevWorkspaceRef.current;
    if (prev && prev !== next) {
      sessionRef.current = withWorkspace(sessionRef.current, prev, captureView());
      void persistSession();
      applyView(sessionRef.current.workspaces[next] ?? defaultWorkspaceView());
    }
    prevWorkspaceRef.current = next;
    setStore(emptyStore);
    setRefreshedAt(undefined);
    setUsageRows([]);
    setUsageError(undefined);
    setIssueOffset(0);
    void refreshRef
      .current()
      .catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)));
    const timer = setInterval(
      () =>
        void refreshRef
          .current()
          .catch((reason) => setError(reason instanceof Error ? reason.message : String(reason))),
      15_000,
    );
    return () => clearInterval(timer);
  }, [workspaceID]);

  useEffect(() => {
    const view = captureView();
    sessionRef.current = withWorkspace(sessionRef.current, workspaceID, view);
    scheduleSession();
  }, [
    tab,
    spaceView,
    contextProject,
    issueStatus,
    issueMine,
    issueAttention,
    inboxView,
    chatStatus,
    agentMachine,
    agentScope,
    agentsView,
    projectStatus,
    projectPriority,
    selected,
    detail,
    workspaceID,
  ]);

  useEffect(() => {
    const current = store;
    if (!refreshedAt || current.workspace?.id !== workspaceID) return;
    const settled = settleWorkspaceView(captureView(), {
      projectIDs: current.projects.map((project) => project.id),
      statusKeys: current.statuses
        .filter((status) => !status.archived_at)
        .map((status) => status.key),
      machines: machineSummaries(current.runtimes).map((machine) => machine.name),
    });
    if (contextProject !== settled.project_id) setContextProject(settled.project_id);
    if (issueStatus !== settled.issue_status) setIssueStatus(settled.issue_status);
    if (agentMachine !== settled.agent_machine) setAgentMachine(settled.agent_machine);
  }, [refreshedAt, store, workspaceID]);

  useEffect(() => {
    const rt = realtime;
    if (!rt) return;
    const off = rt.onWorkspace(() => {
      void refreshRef
        .current()
        .catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)));
    });
    return () => {
      void off();
    };
  }, [realtime]);

  useEffect(() => {
    if (workspaces.length > 0) return;
    void data
      .listWorkspaces()
      .then((list) => setWorkspaces([...list]))
      .catch(() => {});
  }, [workspaces.length, data]);

  const contextProjectLabel =
    contextProject === "all" ? "all projects" : lookups.projectTitle(contextProject);

  const defaultIssueStatus = () => {
    const statuses = store.statuses.filter((status) => !status.archived_at);
    const todo = statuses.find((status) => status.category === "todo");
    return todo?.key ?? statuses[0]?.key ?? "todo";
  };

  const openCreateIssue = () => {
    queueMicrotask(() => {
      openTextPrompt(dialog, {
        title: "New issue",
        placeholder: "Issue title…",
        onSubmit: async (title) => {
          const projectID = contextProject !== "all" ? contextProject : undefined;
          const me = store.me;
          const issue = await data.createIssue({
            title,
            status: defaultIssueStatus(),
            project_id: projectID ?? null,
            ...(issueMine === "assigned" && me
              ? { assignee_type: "member" as const, assignee_id: me.id }
              : {}),
          });
          await refresh();
          goSurface("issues");
          setDetail({ kind: "issue", id: issue.id });
          setSelected({ ...selected, issues: issue.id });
        },
      });
    });
  };

  const pickWorkspace = () => {
    const list = workspaces;
    if (list.length === 0) return;
    queueMicrotask(() => {
      openWorkspacePicker(dialog, list, workspaceID, (id) => {
        if (id === workspaceID) return;
        setWorkspaceID(id);
        void writeWorkspaceID(props.connection.source, id).catch((reason) => {
          setError(reason instanceof Error ? reason.message : String(reason));
        });
      });
    });
  };

  const pickProject = () => {
    queueMicrotask(() => {
      openProjectPicker(dialog, store.projects, contextProject, (id) => setContextProject(id));
    });
  };

  const move = (delta: number) => {
    const list = currentRows;
    if (list.length === 0) return;
    const index = Math.max(
      0,
      list.findIndex((row) => row.id === selectedId),
    );
    const next = list[(index + delta + list.length) % list.length];
    if (next) setSelected({ ...selected, [listKey]: next.id });
  };

  const openSelected = () => {
    const id = selectedId;
    if (!id) return;
    if (tab === "inbox") {
      const item = [...inbox.active, ...inbox.archived].find((entry) => entry.id === id);
      if (!item) return;
      if (!item.read) {
        void inbox.markRead.mutateAsync(id).catch(() => {});
      }
      if (item.issue_id) setDetail({ kind: "issue", id: item.issue_id });
      return;
    }
    if (tab === "issues") {
      setDetail({ kind: "issue", id });
      return;
    }
    if (tab !== "space") return;
    if (spaceView === "chats") {
      setDetail({ kind: "chat", id });
      return;
    }
    if (spaceView === "agents") {
      if (agentsView === "agents") setDetail({ kind: "agent", id });
      return;
    }
    if (spaceView === "projects") {
      // List-only: jump into the unified issues tab filtered to this project.
      setContextProject(id);
      setIssueAttention("all");
      goSurface("issues");
      return;
    }
    if (spaceView === "skills") {
      setDetail({ kind: "skill", id });
      return;
    }
    if (spaceView === "autopilots") {
      setDetail({ kind: "autopilot", id });
    }
  };

  const tabBadge = (name: Surface) => {
    const s = store;
    if (name === "inbox") {
      return inbox.unreadCount ? String(inbox.unreadCount) : undefined;
    }
    if (name === "issues") {
      const needs = s.orchIssues.filter((issue) => issue.attention.bucket === "needs_you").length;
      if (needs) return String(needs);
      return issuesViewTotal ? String(issuesViewTotal) : undefined;
    }
    if (name === "space") {
      const unread = s.chats.reduce(
        (sum, session) => sum + (session.unread_count ?? (session.has_unread ? 1 : 0)),
        0,
      );
      return unread ? String(unread) : undefined;
    }
    return undefined;
  };

  useKeyboard((event) => {
    if (dialog.stack.length) return;
    if (event.ctrl && event.name === "c") {
      quit();
      event.preventDefault();
      return;
    }
    if (searching) {
      if (event.name === "escape" || event.name === "return" || event.name === "enter") {
        setSearching(false);
        if (tab === "issues") {
          setIssueOffset(0);
          void reloadIssues();
        }
        event.preventDefault();
        return;
      }
      if (event.name === "backspace") {
        setQuery((value) => value.slice(0, -1));
        event.preventDefault();
        return;
      }
      if (event.name === "u" && event.ctrl) {
        setQuery("");
        event.preventDefault();
        return;
      }
      if (event.sequence && event.sequence.length === 1 && !event.ctrl) {
        // Functional update: fast typing delivers several keys per frame.
        setQuery((value) => `${value}${event.sequence}`);
        event.preventDefault();
      }
      return;
    }
    if (detail) {
      if (
        event.name === "escape" ||
        event.name === "q" ||
        event.name === "h" ||
        event.name === "left"
      ) {
        setDetail(undefined);
        event.preventDefault();
      }
      return;
    }

    if (event.name === "c" && !event.ctrl && !event.shift && tab === "issues") {
      openCreateIssue();
      event.preventDefault();
      return;
    }
    if (event.name === "c" && !event.ctrl && !event.shift && inChats) {
      queueMicrotask(() => {
        openAgentPicker(dialog, store, (agentId) => {
          void data
            .createChatSession({ agent_id: agentId })
            .then(async (session) => {
              await refresh();
              goSpaceView("chats");
              setDetail({ kind: "chat", id: session.id });
              setSelected({ ...selected, chats: session.id });
            })
            .catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)));
        });
      });
      event.preventDefault();
      return;
    }
    if (event.name === "b" && tab === "inbox" && inboxView !== "archived") {
      queueMicrotask(() => {
        openInboxBatchPicker(dialog, (action) => {
          const run =
            action === "mark_all_read"
              ? inbox.markAllRead.mutateAsync()
              : action === "archive_all_read"
                ? inbox.archiveAllRead.mutateAsync()
                : action === "archive_completed"
                  ? inbox.archiveCompleted.mutateAsync()
                  : inbox.archiveAll.mutateAsync();
          void run.catch((reason) =>
            setError(reason instanceof Error ? reason.message : String(reason)),
          );
        });
      });
      event.preventDefault();
      return;
    }
    if (event.name === "e" && tab === "inbox") {
      const id = selectedId;
      if (id) {
        const action =
          inboxView === "archived"
            ? inbox.unarchive.mutateAsync(id)
            : inbox.archive.mutateAsync(id);
        void action.catch((reason) =>
          setError(reason instanceof Error ? reason.message : String(reason)),
        );
      }
      event.preventDefault();
      return;
    }
    if (event.name === "u" && tab === "inbox") {
      const id = selectedId;
      if (id) {
        const item = [...inbox.active, ...inbox.archived].find((entry) => entry.id === id);
        if (item) {
          const action = item.read
            ? inbox.markUnread.mutateAsync(id)
            : inbox.markRead.mutateAsync(id);
          void action.catch((reason) =>
            setError(reason instanceof Error ? reason.message : String(reason)),
          );
        }
      }
      event.preventDefault();
      return;
    }
    if (
      (event.name === "A" || (event.name === "a" && event.shift)) &&
      tab === "inbox" &&
      inboxView !== "archived"
    ) {
      void inbox.markAllRead
        .mutateAsync()
        .catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)));
      event.preventDefault();
      return;
    }
    if (tab === "inbox" && event.name === "v") {
      const next = INBOX_VIEWS[(INBOX_VIEWS.indexOf(inboxView) + 1) % INBOX_VIEWS.length];
      if (next) setInboxView(next);
      event.preventDefault();
      return;
    }
    if (inChats && (event.name === "P" || (event.name === "p" && event.shift))) {
      const id = selectedId;
      const session = store.chats.find((entry) => entry.id === id);
      if (id && session) {
        void data
          .setChatSessionPinned(id, !session.pinned)
          .then(() => refresh())
          .catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)));
      }
      event.preventDefault();
      return;
    }
    if (inChats && event.name === "e") {
      const id = selectedId;
      if (id) {
        void data
          .setChatSessionArchived(id, chatStatus !== "archived")
          .then(() => refresh())
          .catch((reason) => setError(reason instanceof Error ? reason.message : String(reason)));
      }
      event.preventDefault();
      return;
    }
    if (inChats && event.name === "v") {
      setChatStatus(chatStatus === "active" ? "archived" : "active");
      void refresh().catch((reason) =>
        setError(reason instanceof Error ? reason.message : String(reason)),
      );
      event.preventDefault();
      return;
    }
    if (!event.ctrl && !event.meta && event.sequence && /^[1-4]$/.test(event.sequence)) {
      if (event.shift && tab === "space") {
        const next = spaceViewFromDigit(event.sequence);
        if (next) {
          goSpaceView(next);
          event.preventDefault();
          return;
        }
      }
      const next = surfaceFromDigit(event.sequence);
      if (next) {
        goSurface(next);
        event.preventDefault();
        return;
      }
    }

    if (event.name === "q") {
      quit();
      event.preventDefault();
      return;
    }
    if (event.name === "escape") {
      if (query) {
        setQuery("");
        event.preventDefault();
        return;
      }
      if (tab === "inbox" && inboxView !== "all") {
        setInboxView("all");
        event.preventDefault();
        return;
      }
      if (inChats && chatStatus !== "active") {
        setChatStatus("active");
        void refresh().catch((reason) =>
          setError(reason instanceof Error ? reason.message : String(reason)),
        );
        event.preventDefault();
        return;
      }
      if (
        tab === "issues" &&
        (issueAttention !== "all" ||
          issueStatus !== "all" ||
          issueMine !== "all" ||
          contextProject !== "all")
      ) {
        setIssueAttention("all");
        setIssueStatus("all");
        setIssueMine("all");
        setContextProject("all");
        setIssueOffset(0);
        event.preventDefault();
        return;
      }
      if (
        inAgents &&
        (agentMachine !== "all" || agentScope !== "active" || agentsView !== "agents")
      ) {
        setAgentMachine("all");
        setAgentScope("active");
        setAgentsView("agents");
        event.preventDefault();
        return;
      }
      if (inProjects && (projectStatus !== "all" || projectPriority !== "all")) {
        setProjectStatus("all");
        setProjectPriority("all");
        event.preventDefault();
        return;
      }
      quit();
      event.preventDefault();
      return;
    }
    if (event.name === "/" && searchable) {
      setSearching(true);
      event.preventDefault();
      return;
    }
    // "?" arrives as its own name or as shift+"/" depending on the terminal.
    if (event.name === "?" || (event.shift && (event.name === "/" || event.name === "slash"))) {
      openHelpDialog(dialog);
      event.preventDefault();
      return;
    }
    if (event.name === "f" && !event.ctrl && !event.meta && filterGroups.length) {
      openFilters();
      event.preventDefault();
      return;
    }
    if (event.name === "g" && !event.ctrl && !event.shift) {
      queueMicrotask(() => {
        openGoToPicker(dialog, tab, (surface, nextSpace) => {
          if (nextSpace) goSpaceView(nextSpace);
          else goSurface(surface);
        });
      });
      event.preventDefault();
      return;
    }
    if (event.ctrl && event.name === "w") {
      pickWorkspace();
      event.preventDefault();
      return;
    }
    if (event.name === "r") {
      void refresh().catch((reason) =>
        setError(reason instanceof Error ? reason.message : String(reason)),
      );
      event.preventDefault();
      return;
    }
    if (event.name === "p" && !event.shift) {
      pickProject();
      event.preventDefault();
      return;
    }
    if (event.name === "tab") {
      if (tab === "space" && !detail) {
        goSpaceView(neighborSpaceView(spaceView, event.shift ? -1 : 1));
      } else {
        goSurface(neighborSurface(tab, event.shift ? -1 : 1));
      }
      event.preventDefault();
      return;
    }
    if (event.name === "left") {
      if (tab === "space" && !detail) goSpaceView(neighborSpaceView(spaceView, -1));
      else goSurface(neighborSurface(tab, -1));
      event.preventDefault();
      return;
    }
    if (event.name === "right") {
      if (tab === "space" && !detail) goSpaceView(neighborSpaceView(spaceView, 1));
      else goSurface(neighborSurface(tab, 1));
      event.preventDefault();
      return;
    }

    if (tab === "issues" && (event.name === "[" || event.name === "leftbracket")) {
      setIssueOffset(Math.max(0, issueOffset - ISSUE_PAGE_SIZE));
      event.preventDefault();
      return;
    }
    if (tab === "issues" && (event.name === "]" || event.name === "rightbracket")) {
      const next = issueOffset + ISSUE_PAGE_SIZE;
      if (next < issuesViewTotal) setIssueOffset(next);
      event.preventDefault();
      return;
    }

    if (tab === "issues" && event.name === "s") {
      const statuses = ["all", "in_progress", "in_review", "todo", "backlog", "done"] as const;
      const current = issueStatus as (typeof statuses)[number];
      const next = statuses[(statuses.indexOf(current) + 1) % statuses.length];
      if (next) {
        setIssueOffset(0);
        setIssueStatus(next);
      }
      event.preventDefault();
      return;
    }

    if (tab === "issues" && event.name === "m") {
      const next =
        ISSUE_MINE_SCOPES[(ISSUE_MINE_SCOPES.indexOf(issueMine) + 1) % ISSUE_MINE_SCOPES.length];
      if (next) {
        setIssueOffset(0);
        setIssueMine(next);
      }
      event.preventDefault();
      return;
    }

    if (tab === "issues" && event.name === "o") {
      const next =
        ISSUE_ATTENTION[(ISSUE_ATTENTION.indexOf(issueAttention) + 1) % ISSUE_ATTENTION.length];
      if (next) {
        setIssueOffset(0);
        setIssueAttention(next);
      }
      event.preventDefault();
      return;
    }

    if (inAgents) {
      if (event.name === "m") {
        const machines = ["all", ...machineSummaries(store.runtimes).map((m) => m.name)];
        const next = machines[(machines.indexOf(agentMachine) + 1) % machines.length];
        if (next) setAgentMachine(next);
        event.preventDefault();
        return;
      }
      if (event.name === "s" || event.name === "a") {
        const scopes = ["active", "all", "mine", "working", "archived"] as const;
        const next = scopes[(scopes.indexOf(agentScope) + 1) % scopes.length];
        if (next) setAgentScope(next);
        event.preventDefault();
        return;
      }
      if (event.name === "v") {
        setAgentsView(agentsView === "agents" ? "runtimes" : "agents");
        event.preventDefault();
        return;
      }
    }

    if (inProjects) {
      if (event.name === "s") {
        const statuses = ["all", "in_progress", "planned", "completed"] as const;
        const next =
          statuses[
            (statuses.indexOf(projectStatus as (typeof statuses)[number]) + 1) % statuses.length
          ];
        if (next) setProjectStatus(next);
        event.preventDefault();
        return;
      }
      if (event.name === "p" && event.shift) {
        const priorities = ["all", "urgent", "high", "medium", "low"] as const;
        const next =
          priorities[
            (priorities.indexOf(projectPriority as (typeof priorities)[number]) + 1) %
              priorities.length
          ];
        if (next) setProjectPriority(next);
        event.preventDefault();
        return;
      }
      if (event.name === "i" || event.name === "return" || event.name === "enter") {
        // Enter also handled below; i is an explicit "show issues for project".
        const id = selectedId;
        if (id && event.name === "i") {
          setContextProject(id);
          setIssueAttention("all");
          goSurface("issues");
          event.preventDefault();
          return;
        }
      }
    }

    if (event.name === "h") {
      if (tab === "space" && !detail) goSpaceView(neighborSpaceView(spaceView, -1));
      else goSurface(neighborSurface(tab, -1));
      event.preventDefault();
      return;
    }
    if (event.name === "l") {
      if (tab === "space" && !detail) goSpaceView(neighborSpaceView(spaceView, 1));
      else goSurface(neighborSurface(tab, 1));
      event.preventDefault();
      return;
    }

    if (event.name === "down" || event.name === "j") {
      move(1);
      event.preventDefault();
      return;
    }
    if (event.name === "up" || event.name === "k") {
      move(-1);
      event.preventDefault();
      return;
    }
    if (event.name === "pagedown") {
      move(20);
      event.preventDefault();
      return;
    }
    if (event.name === "pageup") {
      move(-20);
      event.preventDefault();
      return;
    }
    if (event.name === "return" || event.name === "enter") {
      openSelected();
      event.preventDefault();
    }
  });

  const pad = dimensions.width < 44 ? 1 : 2;
  const filterGroups = useMemo((): FilterGroup[] => {
    const s = store;
    if (tab === "inbox") {
      return [
        {
          id: "inbox_view",
          label: "view",
          value: inboxView,
          fallback: "all",
          options: INBOX_VIEWS.map((view) => ({
            id: view,
            label: view,
            count: filterInboxItems(inbox.active, inbox.archived, view).length,
          })),
        },
      ];
    }
    if (tab === "issues") {
      const statuses = ["all", "in_progress", "in_review", "todo", "backlog", "done"] as const;
      return [
        {
          id: "attention",
          label: "attention",
          value: issueAttention,
          fallback: "all",
          compact: true,
          options: ISSUE_ATTENTION.map((bucket) => ({
            id: bucket,
            label: bucket,
            count:
              bucket === "all"
                ? undefined
                : s.orchIssues.filter((i) => i.attention.bucket === bucket).length,
          })),
        },
        {
          id: "status",
          label: "status",
          value: issueStatus,
          fallback: "all",
          compact: true,
          options: statuses.map((status) => ({
            id: status,
            label: status,
            count:
              status === "all"
                ? issuesViewTotal
                : s.issues.filter(
                    (i) =>
                      (i.status_category ??
                        s.statuses.find((entry) => entry.key === i.status)?.category) === status,
                  ).length,
          })),
        },
        {
          id: "mine",
          label: "mine",
          value: issueMine,
          fallback: "all",
          compact: true,
          options: ISSUE_MINE_SCOPES.map((scope) => ({ id: scope, label: scope })),
        },
      ];
    }
    if (tab !== "space") return [];
    if (spaceView === "projects") {
      return [
        {
          id: "project_status",
          label: "status",
          value: projectStatus,
          fallback: "all",
          options: (["all", "in_progress", "planned", "completed"] as const).map((status) => ({
            id: status,
            label: status,
          })),
        },
        {
          id: "project_priority",
          label: "priority",
          value: projectPriority,
          fallback: "all",
          compact: true,
          options: ["all", "urgent", "high", "medium", "low"].map((priority) => ({
            id: priority,
            label: priority,
          })),
        },
      ];
    }
    if (spaceView === "chats") {
      return [
        {
          id: "chat_status",
          label: "view",
          value: chatStatus,
          fallback: "active",
          options: (["active", "archived"] as const).map((status) => ({
            id: status,
            label: status,
          })),
        },
      ];
    }
    if (spaceView === "agents") {
      return [
        {
          id: "agent_scope",
          label: "scope",
          value: agentScope,
          fallback: "active",
          options: (["active", "all", "mine", "working", "archived"] as const).map((scope) => ({
            id: scope,
            label: scope,
          })),
        },
        {
          id: "agent_machine",
          label: "machine",
          value: agentMachine,
          fallback: "all",
          compact: true,
          options: [
            { id: "all", label: `all machines (${s.runtimes.length})` },
            ...machineSummaries(s.runtimes).map((machine) => ({
              id: machine.name,
              label: `${machine.isOnline ? "●" : "○"} ${machine.name}`,
            })),
          ],
        },
        {
          id: "agents_view",
          label: "show",
          value: agentsView,
          fallback: "agents",
          compact: true,
          options: [
            { id: "agents", label: "agents" },
            { id: "runtimes", label: "runtimes" },
          ],
        },
      ];
    }
    return [];
  }, [
    store,
    tab,
    spaceView,
    inbox.active,
    inbox.archived,
    inboxView,
    issueAttention,
    issueStatus,
    issueMine,
    issuesViewTotal,
    chatStatus,
    agentScope,
    agentMachine,
    agentsView,
    projectStatus,
    projectPriority,
  ]);

  // Keep the row even without filters so switching space views never shifts the list.
  const nonListSpaceView = spaceView === "usage" || spaceView === "settings";
  const searchable = tab === "inbox" || tab === "issues" || (tab === "space" && !nonListSpaceView);
  const showFilterBar = tab !== "overview" && !detail && !(tab === "space" && nonListSpaceView);
  const filterSummary = `${currentRows.length} ${listKey}`;
  // header (2) + filter bar + footer (1) are chrome; the rest is list/detail.
  const contentHeight = Math.max(4, dimensions.height - pad * 2 - 3 - (showFilterBar ? 1 : 0));

  const applyFilter = (groupId: string, optionId: string) => {
    setIssueOffset(0);
    if (groupId === "inbox_view") setInboxView(optionId as InboxView);
    else if (groupId === "attention") setIssueAttention(optionId as IssueAttention);
    else if (groupId === "status") {
      setIssueAttention("all");
      setIssueStatus(optionId);
    } else if (groupId === "mine") {
      setIssueAttention("all");
      setIssueMine(optionId as IssueMineScope);
    } else if (groupId === "chat_status") setChatStatus(optionId as "active" | "archived");
    else if (groupId === "project_status") setProjectStatus(optionId as typeof projectStatus);
    else if (groupId === "project_priority") setProjectPriority(optionId as typeof projectPriority);
    else if (groupId === "agent_scope") setAgentScope(optionId as typeof agentScope);
    else if (groupId === "agent_machine") setAgentMachine(optionId);
    else if (groupId === "agents_view") setAgentsView(optionId as typeof agentsView);
  };

  const cycleFilter = (groupId: string) => {
    const group = filterGroups.find((entry) => entry.id === groupId);
    if (!group) return;
    applyFilter(groupId, nextFilterValue(group));
  };

  const openFilters = () => {
    if (!filterGroups.length) return;
    queueMicrotask(() =>
      openFilterPicker(dialog, `Filter · ${listKey}`, filterGroups, applyFilter),
    );
  };

  const tableHeaders = (() => {
    if (tab === "inbox") return TAB_HEADERS.inbox;
    if (tab === "issues") return issueAttention === "all" ? TAB_HEADERS.issues : TAB_HEADERS.orch;
    if (tab === "space") {
      if (spaceView === "agents" && agentsView === "runtimes") return TAB_HEADERS.runtimes;
      if (spaceView === "chats") return TAB_HEADERS.chat;
      return TAB_HEADERS[spaceView] ?? TAB_HEADERS.projects;
    }
    return TAB_HEADERS.issues;
  })();

  const footerHint = (() => {
    if (searching) return "Enter · Esc";
    if (detail?.kind === "issue") return "Esc back · Tab pane";
    if (detail?.kind === "chat") return "Esc back · c message";
    if (detail) return "Esc back";
    const keys: string[] = [];
    if (filterGroups.length) keys.push("f filter");
    if (tab === "issues" || inChats) keys.push("c new");
    if (tab === "inbox") keys.push("b batch");
    if (searchable) keys.push("/ search");
    keys.push("? help");
    return keys.join(" · ");
  })();

  const mainContent = (() => {
    if (!detail) {
      if (tab === "overview") return <Overview store={store} height={contentHeight} now={now} />;
      if (tab === "space" && spaceView === "usage") {
        return <UsageView rows={usageRows} lookups={lookups} error={usageError} />;
      }
      if (tab === "space" && spaceView === "settings") {
        return <SettingsView store={store} connection={props.connection} />;
      }
      return (
        <Table
          headers={tableHeaders}
          rows={currentRows}
          selectedId={selectedId}
          onSelect={(id) => setSelected({ ...selected, [listKey]: id })}
          height={contentHeight}
        />
      );
    }
    if (detail.kind === "issue") {
      return (
        <IssueDetail
          id={detail.id}
          data={data}
          store={store}
          lookups={lookups}
          height={contentHeight}
          now={now}
          realtime={realtime}
          onOpenIssue={(issueId) => setDetail({ kind: "issue", id: issueId })}
        />
      );
    }
    if (detail.kind === "chat") {
      return (
        <ChatDetail id={detail.id} data={data} lookups={lookups} now={now} realtime={realtime} />
      );
    }
    if (detail.kind === "autopilot") {
      return <AutopilotDetail id={detail.id} data={data} lookups={lookups} now={now} />;
    }
    if (detail.kind === "agent") {
      return (
        <AgentDetail
          id={detail.id}
          data={data}
          store={store}
          lookups={lookups}
          height={contentHeight}
          now={now}
          realtime={realtime}
        />
      );
    }
    if (detail.kind === "skill") {
      return <SkillDetail id={detail.id} data={data} height={contentHeight} />;
    }
    return null;
  })();

  return (
    <box
      flexGrow={1}
      flexDirection="column"
      backgroundColor={theme.background.base}
      paddingTop={pad}
      paddingBottom={pad}
      paddingLeft={pad}
      paddingRight={pad}
    >
      {dialog.stack.length === 0 ? (
        <>
          <SurfaceStrip tone="raised" flexShrink={0}>
            <box flexDirection="column" gap={0}>
              <ContextBar
                workspace={store.workspace}
                contextProject={contextProject}
                projectLabel={contextProjectLabel}
                onWorkspaceClick={pickWorkspace}
                onProjectClick={pickProject}
              />
              <NavBar
                surfaces={SURFACES}
                current={tab}
                detail={!!detail}
                badge={tabBadge}
                onSelect={goSurface}
              />
            </box>
          </SurfaceStrip>

          {showFilterBar ? (
            <FilterBar
              groups={filterGroups}
              searching={searching}
              query={query}
              summary={filterSummary}
              onOpen={openFilters}
              onCycle={cycleFilter}
            />
          ) : null}

          <box height={contentHeight} flexShrink={0} flexDirection="column" overflow="hidden">
            {mainContent}
          </box>

          <SurfaceStrip tone="raised" flexShrink={0} height={1}>
            <box flexDirection="row" height={1} justifyContent="space-between" flexShrink={0}>
              {error ? (
                <text fg={theme.text.feedback.error.base} wrapMode="none">
                  error: {error}
                </text>
              ) : (
                <>
                  <box flexDirection="row" gap={2} height={1} flexShrink={1}>
                    <text fg={theme.text.muted} wrapMode="none">
                      {refreshedAt ? new Date(refreshedAt).toLocaleTimeString() : "…"}
                    </text>
                    {realtime?.isConnected() ? (
                      <text fg={theme.text.feedback.success.base} wrapMode="none">
                        ● live
                      </text>
                    ) : null}
                    {store.working.length > 0 ? (
                      <text fg={theme.text.feedback.success.base} wrapMode="none">
                        ● {store.working.length} working
                      </text>
                    ) : null}
                  </box>
                  <text fg={theme.text.muted} wrapMode="none" flexShrink={0}>
                    {footerHint}
                  </text>
                </>
              )}
            </box>
          </SurfaceStrip>
        </>
      ) : null}
    </box>
  );
}
