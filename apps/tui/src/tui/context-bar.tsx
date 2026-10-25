import { TextAttributes } from "@opentui/core";
import { useTheme } from "./context/theme";
import type { Workspace } from "../api/generated";

export function ContextBar(props: {
  workspace?: Workspace;
  contextProject: string;
  projectLabel: string;
  onWorkspaceClick: () => void;
  onProjectClick: () => void;
}) {
  const theme = useTheme();

  return (
    <box flexDirection="row" height={1} flexShrink={0} gap={2} alignItems="center">
      <text fg={theme.text.action.primary.base} attributes={TextAttributes.BOLD}>
        multica
      </text>
      <text fg={theme.text.muted}>·</text>
      <box onMouseUp={props.onWorkspaceClick}>
        <text fg={theme.text.muted}>
          ws{" "}
          <span style={{ fg: theme.text.base, attributes: TextAttributes.BOLD }}>
            {props.workspace?.name ?? "…"}
          </span>
        </text>
      </box>
      <text fg={theme.text.muted}>·</text>
      <box onMouseUp={props.onProjectClick}>
        <text fg={theme.text.muted}>
          proj{" "}
          <span
            style={{
              fg: props.contextProject !== "all" ? theme.text.action.primary.base : theme.text.base,
              attributes: TextAttributes.BOLD,
            }}
          >
            {props.projectLabel}
          </span>
        </text>
      </box>
      <box flexGrow={1} />
    </box>
  );
}

export function NavBar<T extends string>(props: {
  surfaces: readonly T[];
  current: T;
  detail?: boolean;
  badge: (name: T) => string | undefined;
  onSelect: (name: T) => void;
}) {
  const theme = useTheme();

  return (
    <box flexDirection="row" height={1} gap={2} alignItems="center" flexShrink={0}>
      {props.surfaces.map((name, index) => {
        const isCurrent = props.current === name && !props.detail;
        const badge = props.badge(name);
        return (
          <box key={name} onMouseUp={() => props.onSelect(name)}>
            <text
              fg={isCurrent ? theme.text.action.primary.base : theme.text.muted}
              attributes={isCurrent ? TextAttributes.BOLD : undefined}
            >
              {index + 1}:{name}
              {badge ? (
                <span style={{ fg: isCurrent ? theme.text.base : theme.text.muted }}> {badge}</span>
              ) : null}
            </text>
          </box>
        );
      })}
    </box>
  );
}
