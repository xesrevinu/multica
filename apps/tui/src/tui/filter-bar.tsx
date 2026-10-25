import { TextAttributes } from "@opentui/core";
import { useTheme } from "./context/theme";

export type FilterOption = { id: string; label: string; count?: number };

export type FilterGroup = {
  id: string;
  label: string;
  value: string;
  /** Option id that means "no filter" for this group. */
  fallback: string;
  options: readonly FilterOption[];
  /** Compact groups only show a chip when filtered. */
  compact?: boolean;
};

/** Next option for a chip click: keeps filtering one keystroke away. */
export function nextFilterValue(group: FilterGroup): string {
  const index = group.options.findIndex((option) => option.id === group.value);
  const next = group.options[(index + 1) % group.options.length];
  return next?.id ?? group.fallback;
}

export function activeFilterCount(groups: readonly FilterGroup[]): number {
  return groups.filter((group) => group.value !== group.fallback).length;
}

/**
 * One compact line of filter chips. Compact groups (many options, e.g. issue
 * status) only render while they are filtering, so the list stays dense;
 * `f` opens the full menu with every option and its count.
 */
export function FilterBar(props: {
  groups: readonly FilterGroup[];
  searching: boolean;
  query: string;
  /** Shown instead of chips when the surface has nothing to filter. */
  summary?: string;
  onOpen: () => void;
  onCycle: (groupId: string) => void;
}) {
  const theme = useTheme();

  if (props.searching) {
    return (
      <box flexDirection="row" height={1} gap={1} alignItems="center" flexShrink={0}>
        <text fg={theme.text.action.primary.base} attributes={TextAttributes.BOLD}>
          /
        </text>
        <text fg={theme.text.base}>{props.query}</text>
        <text fg={theme.text.action.primary.base}>█</text>
      </box>
    );
  }

  const chips = props.groups.flatMap((group) => {
    // Compact groups stay invisible until they filter something, so a long
    // option list costs no rows and no width.
    const options = group.compact
      ? group.options.filter((option) => option.id === group.value && option.id !== group.fallback)
      : group.options;
    return options.map((option) => {
      const isCurrent = option.id === group.value;
      const label = group.compact ? `${group.label} ${option.label}` : option.label;
      return (
        <box
          key={`${group.id}:${option.id}`}
          backgroundColor={isCurrent ? theme.background.raised.base : undefined}
          paddingLeft={1}
          paddingRight={1}
          onMouseUp={() => props.onCycle(group.id)}
        >
          <text
            fg={isCurrent ? theme.text.action.primary.base : theme.text.muted}
            attributes={isCurrent ? TextAttributes.BOLD : undefined}
          >
            {label}
            {option.count !== undefined ? ` ${option.count}` : ""}
          </text>
        </box>
      );
    });
  });

  // Compact groups hide their chips until filtered, so say "all" once instead.
  const unfiltered =
    props.groups.length > 0 &&
    props.groups.every((group) => group.compact && group.value === group.fallback);

  if (props.groups.length === 0) {
    return (
      <box flexDirection="row" height={1} gap={1} alignItems="center" flexShrink={0}>
        <text fg={theme.text.muted}>{props.summary ?? ""}</text>
      </box>
    );
  }

  return (
    <box flexDirection="row" height={1} gap={1} alignItems="center" flexShrink={0}>
      <box onMouseUp={props.onOpen}>
        <text fg={theme.text.action.primary.base} attributes={TextAttributes.BOLD}>
          f
        </text>
      </box>
      {chips.length ? chips : null}
      {unfiltered ? <text fg={theme.text.muted}>all</text> : null}
    </box>
  );
}
