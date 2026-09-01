"use client";

import { Home, Monitor } from "lucide-react";
import type { Agent, Project } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  ListGrid,
  ListGridBody,
  ListGridCell,
  ListGridHeader,
  ListGridHeaderCell,
  ListGridRow,
} from "@multica/ui/components/ui/list-grid";
import { ActorAvatar } from "../common/actor-avatar";
import { CollectionPageState } from "../layout/collection-page";
import { PAGE_GUTTER } from "../layout/page-header";
import { useT } from "../i18n";
import { ProjectIcon } from "../projects/components/project-icon";

const LAUNCHER_GRID =
  "grid-cols-[0.75rem_minmax(140px,1fr)_auto_0.75rem] " +
  "@2xl:grid-cols-[0.75rem_minmax(180px,1.1fr)_minmax(140px,0.8fr)_auto_0.75rem]";

export interface TerminalLauncherRow {
  id: string;
  project: Project | null;
  title: string;
  path: string;
  agents: Agent[];
}

export function TerminalLauncher({
  hasMachine,
  rows,
  onOpenShell,
  onStartAgent,
}: {
  hasMachine: boolean;
  rows: TerminalLauncherRow[];
  onOpenShell: (projectId: string | null) => void;
  onStartAgent: (projectId: string | null, agentId: string) => void;
}) {
  const { t } = useT("layout");

  if (!hasMachine) {
    return (
      <CollectionPageState
        icon={Monitor}
        title={t(($) => $.terminal.no_online_machines)}
        description={t(($) => $.terminal.select_machine)}
      />
    );
  }

  return (
    <div className="@container min-h-0 flex-1 overflow-auto">
      <p className={`pt-3 pb-1 text-caption text-muted-foreground ${PAGE_GUTTER}`}>
        {t(($) => $.terminal.idle_hint)}
      </p>
      <ListGrid className={LAUNCHER_GRID}>
        <ListGridHeader>
          <ListGridHeaderCell>{t(($) => $.terminal.project)}</ListGridHeaderCell>
          <ListGridHeaderCell className="hidden @2xl:flex">
            {t(($) => $.terminal.path)}
          </ListGridHeaderCell>
          <ListGridHeaderCell>{t(($) => $.terminal.actions)}</ListGridHeaderCell>
        </ListGridHeader>
        <ListGridBody>
          {rows.map((row) => (
            <ListGridRow key={row.id}>
              <ListGridCell className="gap-2">
                {row.project ? (
                  <ProjectIcon project={row.project} size="sm" />
                ) : (
                  <Home className="size-3.5 shrink-0 text-muted-foreground" />
                )}
                <button
                  type="button"
                  className="min-w-0 truncate text-left text-body font-medium hover:underline"
                  onClick={() => onOpenShell(row.project?.id ?? null)}
                >
                  {row.title}
                </button>
              </ListGridCell>
              <ListGridCell className="hidden @2xl:flex">
                <span className="truncate font-mono text-caption text-muted-foreground">
                  {row.path || t(($) => $.terminal.cwd_home)}
                </span>
              </ListGridCell>
              <ListGridCell className="justify-end gap-1">
                <Button size="xs" variant="outline" onClick={() => onOpenShell(row.project?.id ?? null)}>
                  {t(($) => $.terminal.open_shell)}
                </Button>
                {row.agents.map((agent) => (
                  <Button
                    key={agent.id}
                    size="xs"
                    variant="ghost"
                    className="max-w-36"
                    onClick={() => onStartAgent(row.project?.id ?? null, agent.id)}
                  >
                    <ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />
                    <span className="truncate">{agent.name}</span>
                  </Button>
                ))}
              </ListGridCell>
            </ListGridRow>
          ))}
        </ListGridBody>
      </ListGrid>
    </div>
  );
}
