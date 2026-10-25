import { TextAttributes } from "@opentui/core";
import { useTheme } from "./context/theme";
import { useDialog, type DialogContext } from "./ui/dialog";
import type { IssuePriority, Label, Project, Workspace } from "../api/generated";
import { SPACE_VIEWS, SURFACES, type SpaceView, type Surface } from "./nav";
import type { FilterGroup } from "./filter-bar";
import { openPicker, type PickerOption } from "./picker";
import { renderProgressBar } from "./project";
import type { Lookups, Store } from "./store";

/** One menu for every list filter: attention, status, scope, machine… */
export function openFilterPicker(
  dialog: DialogContext,
  title: string,
  groups: readonly FilterGroup[],
  onSelect: (groupId: string, optionId: string) => void,
) {
  const options: PickerOption[] = groups.flatMap((group) =>
    group.options.map((option) => ({
      id: `${group.id}:${option.id}`,
      title: `${option.id === group.value ? "● " : "  "}${option.label}${
        option.count !== undefined ? ` ${option.count}` : ""
      }`,
      category: group.label,
      search: `${group.label} ${option.label}`.toLowerCase(),
    })),
  );

  openPicker(dialog, {
    title,
    placeholder: "Filter…",
    options,
    onSelect: (id) => {
      const index = id.indexOf(":");
      onSelect(id.slice(0, index), id.slice(index + 1));
    },
  });
}

export function openAssigneePicker(
  dialog: DialogContext,
  store: Store,
  lookups: Lookups,
  current: { type: string | null; id: string | null },
  onSelect: (type: string | null, id: string | null) => void,
) {
  const options: PickerOption[] = [
    { id: "none", title: "Unassigned", search: "unassigned none", category: "none" },
    ...store.members.map((member) => ({
      id: `member:${member.user_id}`,
      title: member.name,
      detail: member.email,
      category: "member",
      search: `${member.name} ${member.email} member`.toLowerCase(),
    })),
    ...store.agents
      .filter((agent) => !agent.archived_at)
      .map((agent) => ({
        id: `agent:${agent.id}`,
        title: agent.name,
        detail: agent.status,
        category: "agent",
        search: `${agent.name} ${agent.status} agent`.toLowerCase(),
      })),
  ];

  const selected =
    current.type && current.id
      ? `${current.type}:${current.id}`
      : current.type || current.id
        ? undefined
        : "none";

  openPicker(dialog, {
    title: "Assign to",
    placeholder: "Filter people…",
    options,
    selectedId: selected,
    size: "large",
    onSelect: (id) => {
      if (id === "none") {
        onSelect(null, null);
        return;
      }
      const [type, assigneeId] = id.split(":");
      onSelect(type, assigneeId);
    },
  });
}

export function openStatusPicker(
  dialog: DialogContext,
  store: Store,
  currentKey: string,
  onSelect: (statusKey: string) => void,
) {
  const options: PickerOption[] = store.statuses
    .filter((status) => !status.archived_at)
    .map((status) => ({
      id: status.key,
      title: status.name,
      category: status.category,
      search: `${status.name} ${status.key} ${status.category}`.toLowerCase(),
    }));

  openPicker(dialog, {
    title: "Change status",
    placeholder: "Filter statuses…",
    options,
    selectedId: currentKey,
    onSelect,
  });
}

export function openAgentPicker(
  dialog: DialogContext,
  store: Store,
  onSelect: (agentId: string) => void,
) {
  const options: PickerOption[] = store.agents
    .filter((agent) => !agent.archived_at)
    .map((agent) => ({
      id: agent.id,
      title: agent.name,
      detail: agent.status,
      search: `${agent.name} ${agent.status}`.toLowerCase(),
    }));

  openPicker(dialog, {
    title: "New chat with…",
    placeholder: "Filter agents…",
    options,
    size: "large",
    onSelect,
  });
}

export function openInboxBatchPicker(
  dialog: DialogContext,
  onSelect: (
    action: "mark_all_read" | "archive_all_read" | "archive_completed" | "archive_all",
  ) => void,
) {
  const options: PickerOption[] = [
    { id: "mark_all_read", title: "Mark all read", search: "mark all read" },
    { id: "archive_all_read", title: "Archive all read", search: "archive all read" },
    {
      id: "archive_completed",
      title: "Archive completed issues",
      search: "archive completed done",
    },
    {
      id: "archive_all",
      title: "Archive ALL inbox items",
      detail: "destructive",
      search: "archive all everything",
    },
  ];

  openPicker(dialog, {
    title: "Inbox batch",
    placeholder: "Filter actions…",
    options,
    onSelect: (id) =>
      onSelect(id as "mark_all_read" | "archive_all_read" | "archive_completed" | "archive_all"),
  });
}

export function openPriorityPicker(
  dialog: DialogContext,
  current: IssuePriority,
  onSelect: (priority: IssuePriority) => void,
) {
  const priorities: IssuePriority[] = ["urgent", "high", "medium", "low", "none"];
  const options: PickerOption[] = priorities.map((priority) => ({
    id: priority,
    title: priority,
    search: priority,
  }));

  openPicker(dialog, {
    title: "Change priority",
    placeholder: "Filter…",
    options,
    selectedId: current,
    onSelect: (id) => onSelect(id as IssuePriority),
  });
}

export function openLabelPicker(
  dialog: DialogContext,
  labels: readonly Label[],
  attachedIds: readonly string[],
  onToggle: (labelId: string, attached: boolean) => void,
) {
  const attached = new Set(attachedIds);
  const options: PickerOption[] = labels.map((label) => ({
    id: label.id,
    title: attached.has(label.id) ? `#${label.name} (on)` : `#${label.name}`,
    detail: label.description,
    search: `${label.name} ${label.description ?? ""}`.toLowerCase(),
  }));

  openPicker(dialog, {
    title: "Labels",
    placeholder: "Filter labels…",
    options,
    onSelect: (id) => onToggle(id, attached.has(id)),
  });
}

export function openWorkspacePicker(
  dialog: DialogContext,
  workspaces: Workspace[],
  currentID: string,
  onSelect: (id: string) => void,
) {
  const options: PickerOption[] = workspaces.map((workspace) => ({
    id: workspace.id,
    title: workspace.name,
    detail: workspace.issue_prefix ? `prefix ${workspace.issue_prefix}` : undefined,
    search: `${workspace.name} ${workspace.issue_prefix ?? ""}`.toLowerCase(),
  }));

  openPicker(dialog, {
    title: "Switch workspace",
    placeholder: "Filter workspaces…",
    options,
    selectedId: currentID,
    onSelect,
  });
}

export function openProjectPicker(
  dialog: DialogContext,
  projects: readonly Project[],
  currentID: string,
  onSelect: (id: string) => void,
) {
  const options: PickerOption[] = [
    {
      id: "all",
      title: "All projects",
      detail: "show every issue",
      search: "all projects",
    },
    ...projects.map((project) => ({
      id: project.id,
      title: `${project.icon ?? "📁"} ${project.title}`,
      detail: `${renderProgressBar(project.done_count, project.issue_count, 8)} · ${project.status}`,
      search: `${project.title} ${project.status} ${project.description ?? ""}`.toLowerCase(),
    })),
  ];

  openPicker(dialog, {
    title: "Switch project",
    placeholder: "Filter projects…",
    options,
    selectedId: currentID === "all" ? undefined : currentID,
    size: "large",
    onSelect,
  });
}

export function openGoToPicker(
  dialog: DialogContext,
  current: Surface,
  onSelect: (surface: Surface, spaceView?: SpaceView) => void,
) {
  const options: PickerOption[] = [
    ...SURFACES.map((surface) => ({
      id: surface,
      title: surface,
      category: "tabs",
      search: surface,
    })),
    ...SPACE_VIEWS.map((view) => ({
      id: `space:${view}`,
      title: view,
      category: "space",
      search: `space ${view}`,
    })),
  ];

  openPicker(dialog, {
    title: "Go to",
    placeholder: "overview, inbox, issues, projects…",
    options,
    selectedId: current,
    onSelect: (id) => {
      if (id.startsWith("space:")) {
        onSelect("space", id.slice("space:".length) as SpaceView);
        return;
      }
      onSelect(id as Surface);
    },
  });
}

const HELP: readonly (readonly string[])[] = [
  [
    "Global",
    "  1–4          switch surface",
    "  h / l        next / previous tab",
    "  g            go to…",
    "  f            filters",
    "  /  r         search · refresh",
    "  p  ctrl+w    project · workspace",
    "  ?  q         help · quit",
    "",
    "Issues",
    "  Enter        open detail",
    "  o m s        attention · mine · status",
    "  [ ]          page",
    "  c            new issue",
  ],
  [
    "Inbox",
    "  Enter e u    open · archive · read",
    "  A b          read all · batch",
    "",
    "Space",
    "  projects     Enter → its issues",
    "  agents       Enter · v runtimes",
    "  autopilots   Enter · t run now",
    "  chats        c · P pin · e archive",
    "",
    "Detail",
    "  tab [ ]      pane · j/k scroll",
    "  c s a P l    comment · assignee · labels",
    "  n N r e d R  comments",
    "  o            orchestrator actions",
    "  t x          transcript · cancel",
  ],
];

function HelpDialog() {
  const dialog = useDialog();
  const theme = useTheme();
  dialog.setSize("large");
  dialog.setCentered(true);
  return (
    <box flexDirection="column" paddingLeft={1} paddingRight={1} gap={0}>
      <box flexDirection="row" justifyContent="space-between">
        <text fg={theme.text.base} attributes={TextAttributes.BOLD}>
          Key bindings
        </text>
        <text fg={theme.text.muted}>Esc close</text>
      </box>
      <box flexDirection="row" gap={3} marginTop={1}>
        {HELP.map((column, index) => (
          <box key={index} flexDirection="column" gap={0}>
            {column.map((line, i) => (
              <text
                key={i}
                fg={line.startsWith(" ") ? theme.text.base : theme.text.action.primary.base}
                height={1}
              >
                {line || " "}
              </text>
            ))}
          </box>
        ))}
      </box>
    </box>
  );
}

export function openHelpDialog(dialog: DialogContext) {
  dialog.replace(() => <HelpDialog />);
}
