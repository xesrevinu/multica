import { useState, useEffect, useRef, type ReactNode } from "react";
import { TextAttributes, type ScrollBoxRenderable } from "@opentui/core";
import { useKeyboard, useTerminalDimensions } from "@opentui/react";
import { useTheme } from "./context/theme";
import { openComposeDialog } from "./compose-dialog";
import {
  openAssigneePicker,
  openLabelPicker,
  openPriorityPicker,
  openStatusPicker,
} from "./dialogs";
import { useDialog } from "./ui/dialog";
import type {
  ActorType,
  AgentTask,
  Issue,
  IssuePullRequest,
  TaskMessage,
  TimelineEntry,
} from "../api/generated";
import type { DataClient } from "../data";
import type { RealtimeClient } from "../realtime";
import type { TaskMessagePayload } from "../ws/events";
import { formatRelative, formatTokens, shortId } from "../format";
import { ItemList, type ItemListRow } from "./item-list";
import { MarkdownView } from "./markdown-view";
import { statusTone, type Lookups, type Store } from "./store";
import { useTone, type Tone } from "./table";
import { TimelineView } from "./timeline-view";
import { PRIORITY_TONE, TASK_STATUS_TONE } from "./tones";
import { OrchFlowPanel } from "./orch-flow";

type Detail = {
  issue: Issue;
  timeline: readonly TimelineEntry[];
  tasks: readonly AgentTask[];
  children: readonly Issue[];
};

const PANES = ["description", "runs", "comments", "properties"] as const;

function newestFirst<T extends { id: string; created_at: string }>(items: readonly T[]): T[] {
  return [...items].sort(
    (left, right) =>
      right.created_at.localeCompare(left.created_at) || right.id.localeCompare(left.id),
  );
}
type Pane = (typeof PANES)[number];
const PROPERTIES_WIDTH = 36;

function DetailPane(props: {
  title: string;
  focused: boolean;
  grow?: number;
  width?: number;
  onFocus: () => void;
  onScroll: (element: ScrollBoxRenderable) => void;
  pinned?: ReactNode;
  children: ReactNode;
}) {
  const theme = useTheme();
  const fixed = props.width !== undefined;

  return (
    <box
      width={props.width}
      flexGrow={fixed ? 0 : (props.grow ?? 1)}
      flexShrink={fixed ? 0 : 1}
      minWidth={fixed ? props.width : 0}
      minHeight={0}
      flexDirection="column"
      border={["left"]}
      borderColor={props.focused ? theme.text.action.primary.base : theme.border.base}
      paddingLeft={1}
      onMouseDown={props.onFocus}
    >
      <text
        fg={props.focused ? theme.text.action.primary.base : theme.text.muted}
        attributes={TextAttributes.BOLD}
      >
        {props.title}
      </text>
      {props.pinned ? (
        <box flexShrink={0} flexDirection="column" gap={1} paddingRight={1} paddingBottom={1}>
          {props.pinned}
        </box>
      ) : null}
      <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column" overflow="hidden">
        <scrollbox
          ref={props.onScroll}
          height="100%"
          flexShrink={0}
          stickyScroll={false}
          scrollX={false}
          horizontalScrollbarOptions={{ visible: false }}
          verticalScrollbarOptions={{
            position: "absolute",
            right: 0,
            top: 0,
            width: 1,
            height: "100%",
            trackOptions: {
              backgroundColor: theme.background.base,
              foregroundColor: theme.scrollbar.base,
            },
          }}
        >
          <box flexDirection="column" gap={1} paddingRight={1}>
            {props.children}
          </box>
        </scrollbox>
      </box>
    </box>
  );
}

function Field(props: { label: string; value: string; tone?: Tone; onPress?: () => void }) {
  const theme = useTheme();
  const tone = useTone();
  return (
    <box flexDirection="column" gap={0} onMouseUp={() => props.onPress?.()}>
      <text fg={theme.text.muted}>{props.label}</text>
      <text fg={tone(props.tone, Boolean(props.onPress))} wrapMode="word">
        {props.value}
      </text>
    </box>
  );
}

function prState(pr: IssuePullRequest) {
  if (pr.merged_at) return "merged";
  return pr.state;
}

function prTone(state: string): Tone {
  if (state === "merged") return "success";
  if (state === "open") return "accent";
  if (state === "closed") return "subdued";
  return "default";
}

function PropertyFields(props: {
  issue: Issue;
  store: Store;
  lookups: Lookups;
  now: number;
  onChangeStatus: () => void;
  onChangeAssignee: () => void;
  onChangePriority: () => void;
  onChangeLabels: () => void;
  onOpenIssue: (id: string) => void;
}) {
  const theme = useTheme();
  const parentID = props.issue.parent_issue_id;
  const parent = parentID ? props.store.issues.find((issue) => issue.id === parentID) : undefined;
  const labels = (props.issue.labels ?? []).map((label) => `#${label.name}`).join(" ");
  return (
    <box flexDirection="column" gap={1}>
      <Field
        label="status"
        value={props.lookups.statusLabel(props.issue)}
        tone={statusTone(props.store, props.issue)}
        onPress={props.onChangeStatus}
      />
      <Field
        label="assignee"
        value={
          props.issue.assignee_type
            ? props.lookups.actorName(props.issue.assignee_type, props.issue.assignee_id)
            : "Unassigned"
        }
        onPress={props.onChangeAssignee}
      />
      <Field
        label="project"
        value={
          props.issue.project_id ? props.lookups.projectTitle(props.issue.project_id) : "No project"
        }
      />
      <Field
        label="priority"
        value={props.issue.priority}
        tone={PRIORITY_TONE[props.issue.priority]}
        onPress={props.onChangePriority}
      />
      {props.issue.due_date ? <Field label="due" value={props.issue.due_date} /> : null}
      <Field label="labels" value={labels || "None"} onPress={props.onChangeLabels} />
      {parentID ? (
        <Field
          label="parent"
          value={parent ? `${parent.identifier}  ${parent.title}` : shortId(parentID)}
          onPress={() => props.onOpenIssue(parentID)}
        />
      ) : null}
      <text fg={theme.text.muted}>updated {formatRelative(props.issue.updated_at, props.now)}</text>
    </box>
  );
}

function PropertyLists(props: {
  children: readonly Issue[];
  pullRequests: readonly IssuePullRequest[];
  pullRequestError: boolean;
  store: Store;
  lookups: Lookups;
  onOpenIssue: (id: string) => void;
}) {
  const theme = useTheme();
  const tone = useTone();
  return (
    <box flexDirection="column" gap={1}>
      <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
        pull requests{props.pullRequestError ? "" : ` (${props.pullRequests.length})`}
      </text>
      {props.pullRequestError ? (
        <text fg={theme.text.feedback.error.base}>Couldn't load pull requests.</text>
      ) : props.pullRequests.length > 0 ? (
        props.pullRequests.map((pr) => {
          const state = prState(pr);
          return (
            <box
              key={pr.id ?? `${pr.repo_owner}/${pr.repo_name}#${pr.number}`}
              flexDirection="column"
              gap={0}
            >
              <text fg={tone(prTone(state), false)} wrapMode="word">
                {state} {pr.repo_owner}/{pr.repo_name}#{pr.number}
              </text>
              <text fg={theme.text.base} wrapMode="word">
                {pr.title}
              </text>
            </box>
          );
        })
      ) : (
        <text fg={theme.text.muted}>No pull requests.</text>
      )}
      <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
        sub-issues ({props.children.length})
      </text>
      {props.children.length > 0 ? (
        props.children.map((child) => (
          <box
            key={child.id}
            flexDirection="column"
            gap={0}
            onMouseUp={() => props.onOpenIssue(child.id)}
          >
            <text fg={theme.text.base} wrapMode="word">
              {child.identifier} {child.title}
            </text>
            <text fg={tone(statusTone(props.store, child), false)}>
              {props.lookups.statusLabel(child)}
            </text>
          </box>
        ))
      ) : (
        <text fg={theme.text.muted}>No sub-issues.</text>
      )}
    </box>
  );
}

export function IssueDetail(props: {
  id: string;
  data: DataClient;
  store: Store;
  lookups: Lookups;
  height: number;
  now: number;
  realtime?: RealtimeClient;
  onOpenIssue?: (id: string) => void;
}) {
  const theme = useTheme();
  const dialog = useDialog();
  const dimensions = useTerminalDimensions();
  const [detail, setDetail] = useState<Detail>();
  const [error, setError] = useState<string>();
  const [live, setLive] = useState(false);
  const [taskMessages, setTaskMessages] = useState<readonly TaskMessagePayload[]>([]);
  const [transcript, setTranscript] = useState<readonly TaskMessage[]>([]);
  const [selectedTaskId, setSelectedTaskId] = useState<string>();
  const [selectedCommentId, setSelectedCommentId] = useState<string>();
  const [pane, setPane] = useState<Pane>("description");
  const [pullRequests, setPullRequests] = useState<readonly IssuePullRequest[]>([]);
  const [pullRequestError, setPullRequestError] = useState(false);
  const scrollsRef = useRef<Partial<Record<Pane, ScrollBoxRenderable>>>({});

  const load = async () => {
    const issueID = props.id;
    const [issue, timeline, tasks, children, pulls] = await Promise.all([
      props.data.getIssue(issueID),
      props.data.listIssueTimeline(issueID),
      props.data.listIssueTaskRuns(issueID),
      props.data.listIssueChildren(issueID),
      props.data.listIssuePullRequests(issueID).then(
        (rows) => ({ rows, error: false }),
        () => ({ rows: [] as readonly IssuePullRequest[], error: true }),
      ),
    ]);
    if (issueID !== props.id) return;
    setDetail({
      issue,
      timeline: newestFirst(timeline),
      tasks: newestFirst(tasks),
      children: children.issues,
    });
    setPullRequests(pulls.rows);
    setPullRequestError(pulls.error);
    setError(undefined);
  };

  useEffect(() => {
    setDetail(undefined);
    setPullRequests([]);
    setPullRequestError(false);
    setPane("description");
    void load().catch((reason) =>
      setError(reason instanceof Error ? reason.message : String(reason)),
    );
  }, [props.id]);

  useEffect(() => {
    const realtime = props.realtime;
    if (!realtime) {
      setLive(false);
      const timer = setInterval(
        () =>
          void load().catch((reason) =>
            setError(reason instanceof Error ? reason.message : String(reason)),
          ),
        10_000,
      );
      return () => clearInterval(timer);
    }

    setLive(realtime.isConnected());
    const offIssue = realtime.onIssue((issueId) => {
      if (issueId !== props.id) return;
      void load().catch((reason) =>
        setError(reason instanceof Error ? reason.message : String(reason)),
      );
    });
    const offWorkspace = realtime.onWorkspace(() => {
      void load().catch((reason) =>
        setError(reason instanceof Error ? reason.message : String(reason)),
      );
    });
    return () => {
      offIssue();
      offWorkspace();
    };
  }, [props.realtime, props.id]);

  useEffect(() => {
    const realtime = props.realtime;
    if (!realtime) return;
    setTaskMessages([]);
    const off = realtime.onTaskMessage((payload) => {
      if (payload.issue_id !== props.id) return;
      setTaskMessages((previous) => [...previous, payload].slice(-40));
    });
    return off;
  }, [props.realtime, props.id]);

  const activeTask = detail?.tasks.find(
    (task) => task.status === "running" || task.status === "queued" || task.status === "dispatched",
  );

  const scrollBy = (delta: number) => {
    const scroll = scrollsRef.current[pane];
    if (!scroll) return;
    if (delta < 0) scroll.stickyScroll = false;
    scroll.scrollBy(delta);
  };

  const cyclePane = (delta: number) => {
    const index = PANES.indexOf(pane);
    const next = PANES[(index + delta + PANES.length) % PANES.length];
    if (next) setPane(next);
  };

  const commentIds =
    detail?.timeline.filter((entry) => entry.type === "comment").map((entry) => entry.id) ?? [];

  const selectedComment = detail?.timeline.find(
    (entry) => entry.type === "comment" && entry.id === selectedCommentId,
  );

  const cycleComment = (delta: number) => {
    const ids = commentIds;
    if (ids.length === 0) return;
    const index = ids.indexOf(selectedCommentId ?? "");
    const next = ids[(index < 0 ? 0 : index + delta + ids.length) % ids.length];
    if (!next) return;
    setPane("comments");
    setSelectedCommentId(next);
    queueMicrotask(() => scrollsRef.current.comments?.scrollChildIntoView(`timeline-${next}`));
  };

  const changeStatus = () => {
    if (!detail) return;
    openStatusPicker(dialog, props.store, detail.issue.status, (status) => {
      void props.data.updateIssue(props.id, { status }).then(load);
    });
  };

  const changeAssignee = () => {
    if (!detail) return;
    openAssigneePicker(
      dialog,
      props.store,
      props.lookups,
      { type: detail.issue.assignee_type, id: detail.issue.assignee_id },
      (type, id) => {
        void props.data
          .updateIssue(props.id, {
            assignee_type: type as ActorType | null,
            assignee_id: id,
          })
          .then(load);
      },
    );
  };

  const changePriority = () => {
    if (!detail) return;
    openPriorityPicker(dialog, detail.issue.priority, (priority) => {
      void props.data.updateIssue(props.id, { priority }).then(load);
    });
  };

  const changeLabels = () => {
    if (!detail) return;
    const attached = (detail.issue.labels ?? []).map((label) => label.id);
    openLabelPicker(dialog, props.store.labels, attached, (labelId, already) => {
      const action = already
        ? props.data.detachLabel(props.id, labelId)
        : props.data.attachLabel(props.id, labelId);
      void action.then(load);
    });
  };

  useKeyboard((event) => {
    if (dialog.stack.length) return;
    if (detail && event.name === "c" && !event.ctrl && !event.shift) {
      openComposeDialog(dialog, {
        title: `Comment on ${detail.issue.identifier}`,
        placeholder: "Markdown supported…",
        onSubmit: async (content) => {
          await props.data.createComment(props.id, { content });
          await load();
        },
      });
      event.preventDefault();
      return;
    }
    if (detail && event.name === "s") {
      changeStatus();
      event.preventDefault();
      return;
    }
    if (detail && event.name === "a") {
      changeAssignee();
      event.preventDefault();
      return;
    }
    if (detail && event.name === "P") {
      changePriority();
      event.preventDefault();
      return;
    }
    if (detail && event.name === "x") {
      if (activeTask) void props.data.cancelTask(props.id, activeTask.id).then(load);
      event.preventDefault();
      return;
    }
    if (detail && event.name === "t") {
      const taskId = selectedTaskId ?? activeTask?.id ?? detail.tasks[0]?.id;
      if (!taskId) return;
      void props.data.listTaskMessages(taskId).then((messages) => {
        setTranscript(messages);
        setSelectedTaskId(taskId);
        setPane("runs");
      });
      event.preventDefault();
      return;
    }
    if (detail && event.name === "l") {
      changeLabels();
      event.preventDefault();
      return;
    }
    if (detail && event.name === "n") {
      cycleComment(event.shift ? -1 : 1);
      event.preventDefault();
      return;
    }
    if (detail && event.name === "r") {
      openComposeDialog(dialog, {
        title: selectedComment
          ? `Reply on ${detail.issue.identifier}`
          : `Comment on ${detail.issue.identifier}`,
        placeholder: "Markdown supported…",
        onSubmit: async (content) => {
          await props.data.createComment(props.id, {
            content,
            parent_id: selectedComment?.id ?? null,
          });
          await load();
        },
      });
      event.preventDefault();
      return;
    }
    if (detail && event.name === "e") {
      if (!selectedComment?.content) return;
      openComposeDialog(dialog, {
        title: "Edit comment",
        placeholder: "Markdown…",
        value: selectedComment.content,
        onSubmit: async (content) => {
          await props.data.updateComment(selectedComment.id, { content });
          await load();
        },
      });
      event.preventDefault();
      return;
    }
    if (detail && event.name === "R") {
      if (!selectedComment) return;
      const action = selectedComment.resolved_at
        ? props.data.unresolveComment(selectedComment.id)
        : props.data.resolveComment(selectedComment.id);
      void action.then(load);
      event.preventDefault();
      return;
    }
    if (
      detail &&
      (event.name === "tab" ||
        event.name === "[" ||
        event.name === "]" ||
        event.name === "leftbracket" ||
        event.name === "rightbracket")
    ) {
      const backward = event.shift || event.name === "[" || event.name === "leftbracket";
      cyclePane(backward ? -1 : 1);
      event.preventDefault();
      return;
    }
    if (detail && event.name === "d" && !event.ctrl) {
      if (!selectedComment) return;
      void props.data.deleteComment(selectedComment.id).then(() => {
        setSelectedCommentId(undefined);
        return load();
      });
      event.preventDefault();
      return;
    }
    const scroll = scrollsRef.current[pane];
    if (!scroll) return;
    const page = Math.max(1, scroll.viewport.height - 1);
    if (event.name === "j" || event.name === "down") {
      scrollBy(1);
      event.preventDefault();
      return;
    }
    if (event.name === "k" || event.name === "up") {
      scrollBy(-1);
      event.preventDefault();
      return;
    }
    if (event.name === "pagedown") {
      scrollBy(page);
      event.preventDefault();
      return;
    }
    if (event.name === "pageup") {
      scrollBy(-page);
      event.preventDefault();
    }
  });

  const wide = dimensions.width >= 88;
  const splitWork = dimensions.width >= 124;
  const contentWidth = (() => {
    const available = Math.max(24, dimensions.width - 8);
    if (!wide) return available;
    return Math.max(40, available - PROPERTIES_WIDTH - 2);
  })();

  const taskRows = (tasks: readonly AgentTask[]): ItemListRow[] =>
    tasks.map((task) => ({
      id: task.id,
      cells: [
        { text: task.status, width: 10, tone: TASK_STATUS_TONE[task.status] },
        { text: shortId(task.id), width: 14 },
        {
          text: [
            formatRelative(task.created_at, props.now),
            task.attempt && task.attempt > 1 ? `attempt ${task.attempt}` : "",
            task.kind ?? "",
            task.branch_name ?? "",
          ]
            .filter(Boolean)
            .join(" · "),
        },
      ],
      lines: [
        ...((task.trigger_summary ?? task.handoff_note)
          ? [task.trigger_summary ?? task.handoff_note!]
          : []),
        ...(task.error ? [task.error] : []),
        ...(task.usage && task.usage.length > 0
          ? task.usage.map(
              (usage) =>
                `${usage.model}: ${formatTokens(usage.input_tokens)} in / ${formatTokens(usage.output_tokens)} out`,
            )
          : []),
      ],
    }));

  if (!detail) {
    return (
      <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column">
        <box paddingTop={1}>
          <text fg={error ? theme.text.feedback.error.base : theme.text.muted}>
            {error ?? "Loading issue…"}
          </text>
        </box>
      </box>
    );
  }

  const descriptionWidth = splitWork ? Math.max(24, Math.floor(contentWidth * 0.58)) : contentWidth;
  const commentWidth = splitWork ? Math.max(20, contentWidth - descriptionWidth - 2) : contentWidth;
  const openIssue = (id: string) => props.onOpenIssue?.(id);
  const fields = (
    <PropertyFields
      issue={detail.issue}
      store={props.store}
      lookups={props.lookups}
      now={props.now}
      onChangeStatus={changeStatus}
      onChangeAssignee={changeAssignee}
      onChangePriority={changePriority}
      onChangeLabels={changeLabels}
      onOpenIssue={openIssue}
    />
  );
  const lists = (
    <PropertyLists
      children={detail.children}
      pullRequests={pullRequests}
      pullRequestError={pullRequestError}
      store={props.store}
      lookups={props.lookups}
      onOpenIssue={openIssue}
    />
  );
  const propertiesPane = (fixed: boolean) => (
    <DetailPane
      title="properties"
      width={fixed ? PROPERTIES_WIDTH : undefined}
      grow={1}
      focused={pane === "properties"}
      onFocus={() => setPane("properties")}
      onScroll={(element) => {
        scrollsRef.current.properties = element;
      }}
    >
      <box flexDirection="column" gap={1}>
        <OrchFlowPanel
          workspaceId={props.data.workspaceID}
          issueKey={detail.issue.identifier}
          focused={pane === "properties"}
          onChanged={() =>
            void load().catch((reason) =>
              setError(reason instanceof Error ? reason.message : String(reason)),
            )
          }
        />
        {fields}
      </box>
      {lists}
    </DetailPane>
  );

  return (
    <box height={props.height} flexShrink={0} flexDirection="column">
      <box flexGrow={1} flexShrink={1} minHeight={0} flexDirection="column" gap={1}>
        <box flexShrink={0} flexDirection="row" justifyContent="space-between">
          <box flexDirection="row" flexGrow={1} flexShrink={1} minWidth={0}>
            <text fg={theme.text.muted}>{detail.issue.identifier} </text>
            <text
              fg={theme.text.base}
              attributes={TextAttributes.BOLD}
              wrapMode="word"
              flexShrink={1}
            >
              {detail.issue.title}
            </text>
          </box>
          {live ? <text fg={theme.text.feedback.success.base}>● live</text> : null}
        </box>

        <box
          flexGrow={1}
          flexShrink={1}
          minHeight={0}
          flexDirection={wide ? "row" : "column"}
          gap={1}
        >
          {!wide ? propertiesPane(false) : null}
          <box
            flexGrow={wide ? 1 : 3}
            flexShrink={1}
            minWidth={0}
            minHeight={0}
            flexDirection={splitWork ? "row" : "column"}
            gap={1}
          >
            <DetailPane
              title="description"
              grow={3}
              focused={pane === "description"}
              onFocus={() => setPane("description")}
              onScroll={(element) => {
                scrollsRef.current.description = element;
              }}
            >
              {detail.issue.description ? (
                <MarkdownView source={detail.issue.description} width={descriptionWidth} />
              ) : (
                <text fg={theme.text.muted}>No description.</text>
              )}
            </DetailPane>

            <box
              flexGrow={2}
              flexShrink={1}
              minWidth={0}
              minHeight={0}
              flexDirection="column"
              gap={1}
            >
              <DetailPane
                title={`runs (${detail.tasks.length})`}
                grow={2}
                focused={pane === "runs"}
                onFocus={() => setPane("runs")}
                onScroll={(element) => {
                  scrollsRef.current.runs = element;
                }}
              >
                <ItemList
                  rows={taskRows(detail.tasks)}
                  empty="No agent runs yet."
                  onRowSelect={(id) => {
                    setSelectedTaskId(id);
                    setPane("runs");
                    void props.data.listTaskMessages(id).then(setTranscript);
                  }}
                />
                {transcript.length > 0 ? (
                  <box flexDirection="column" gap={0}>
                    <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
                      transcript {selectedTaskId ? shortId(selectedTaskId) : ""} (
                      {transcript.length})
                    </text>
                    {[...transcript]
                      .sort((left, right) => right.seq - left.seq)
                      .map((message) => (
                        <text
                          key={`${message.seq}-${message.type}`}
                          fg={theme.text.base}
                          wrapMode="word"
                        >
                          [{message.type}
                          {message.tool ? ` ${message.tool}` : ""}]{" "}
                          {message.content ?? message.output ?? ""}
                        </text>
                      ))}
                  </box>
                ) : null}
                {activeTask && taskMessages.length > 0 ? (
                  <box flexDirection="column" gap={0}>
                    <text fg={theme.text.muted} attributes={TextAttributes.BOLD}>
                      live agent output ({taskMessages.length})
                    </text>
                    {[...taskMessages]
                      .sort((left, right) => right.seq - left.seq)
                      .map((message) => (
                        <text
                          key={`${message.seq}-${message.type}`}
                          fg={theme.text.base}
                          wrapMode="word"
                        >
                          [{message.type}
                          {message.tool ? ` ${message.tool}` : ""}]{" "}
                          {message.content ?? message.output ?? ""}
                        </text>
                      ))}
                  </box>
                ) : null}
              </DetailPane>

              <DetailPane
                title={`comments (${detail.timeline.length})`}
                grow={3}
                focused={pane === "comments"}
                onFocus={() => setPane("comments")}
                onScroll={(element) => {
                  scrollsRef.current.comments = element;
                }}
              >
                {detail.timeline.length > 0 ? (
                  <TimelineView
                    entries={detail.timeline}
                    lookups={props.lookups}
                    now={props.now}
                    width={commentWidth}
                    selectedCommentId={selectedCommentId}
                    onSelectComment={(id) => {
                      setPane("comments");
                      setSelectedCommentId(id);
                    }}
                  />
                ) : (
                  <text fg={theme.text.muted}>No comments yet.</text>
                )}
              </DetailPane>
            </box>
          </box>
          {wide ? propertiesPane(true) : null}
        </box>
      </box>
    </box>
  );
}
