"use client";

import type { ReactNode } from "react";
import { ChevronRight, Cloud, Monitor, Plus } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useWorkspacePaths } from "@multica/core/paths";
import { PAGE_GUTTER } from "../../layout/page-header";
import { useT } from "../../i18n";
import { HealthDot } from "./shared";
import type { RuntimeMachine } from "./runtime-machines";

export function MachineRail({
  machines,
  selectedId,
  onSelect,
  onConnectRemote,
  bootstrapping,
}: {
  machines: RuntimeMachine[];
  selectedId: string | null;
  onSelect: (id: string | null) => void;
  onConnectRemote: () => void;
  bootstrapping?: boolean;
}) {
  const { t } = useT("runtimes");
  const paths = useWorkspacePaths();

  return (
    <div
      className={cn(
        "flex shrink-0 items-center gap-2 overflow-x-auto border-b py-2",
        PAGE_GUTTER,
      )}
    >
      <RailChip
        pressed={selectedId === null}
        onClick={() => onSelect(null)}
        label={t(($) => $.rail.all)}
      />
      {machines.map((machine) => {
        const Icon = machine.section === "cloud" ? Cloud : Monitor;
        const busyCount = machine.runningCount + machine.queuedCount;
        return (
          <RailChip
            key={machine.id}
            pressed={selectedId === machine.id}
            onClick={() =>
              onSelect(selectedId === machine.id ? null : machine.id)
            }
            label={machine.title}
            leading={
              <span className="relative flex size-5 shrink-0 items-center justify-center">
                <Icon aria-hidden="true" className="size-3.5" />
                <HealthDot
                  health={machine.health}
                  className="absolute -bottom-0.5 -right-0.5 ring-1 ring-background"
                />
              </span>
            }
            meta={[
              t(($) => $.machine.runtime_count, {
                count: machine.runtimes.length,
              }),
              busyCount > 0
                ? t(($) => $.machine.metrics.workload_hint, {
                    running: machine.runningCount,
                    queued: machine.queuedCount,
                  })
                : null,
            ]}
            href={paths.runtimeDetail(machine.id)}
            openLabel={t(($) => $.rail.open_machine, { machine: machine.title })}
          />
        );
      })}
      {bootstrapping && machines.length === 0 ? (
        <span className="shrink-0 px-1 text-caption text-muted-foreground">
          {t(($) => $.page.bootstrapping.title)}
        </span>
      ) : null}
      <button
        type="button"
        onClick={onConnectRemote}
        className="inline-flex h-8 shrink-0 items-center gap-1 rounded-md px-2 text-caption text-muted-foreground transition-colors hover:bg-accent/70 hover:text-foreground"
      >
        <Plus aria-hidden="true" className="size-3" />
        {t(($) => $.page.connect_remote)}
      </button>
    </div>
  );
}

function RailChip({
  pressed,
  onClick,
  label,
  leading,
  meta,
  href,
  openLabel,
}: {
  pressed: boolean;
  onClick: () => void;
  label: string;
  leading?: ReactNode;
  meta?: Array<string | null>;
  href?: string;
  openLabel?: string;
}) {
  const details = (meta ?? []).filter((value): value is string => Boolean(value));
  return (
    <span
      className={cn(
        "inline-flex h-8 shrink-0 items-center rounded-md border text-caption transition-colors",
        pressed
          ? "border-border bg-accent text-accent-foreground"
          : "border-transparent bg-muted/50 text-muted-foreground transition-colors duration-75 hover:bg-surface-hover hover:text-foreground active:bg-surface-selected",
      )}
    >
      <button
        type="button"
        aria-pressed={pressed}
        onClick={onClick}
        className="inline-flex h-full max-w-[16rem] items-center gap-1.5 px-2.5"
      >
        {leading}
        <span className="truncate font-medium text-foreground">{label}</span>
        {details.length > 0 ? (
          <span className="hidden truncate text-muted-foreground sm:inline">
            {details.join(" · ")}
          </span>
        ) : null}
      </button>
      {href ? (
        <AppLink
          href={href}
          aria-label={openLabel}
          className="inline-flex h-full items-center border-l border-border/60 px-1.5 text-faint-foreground hover:text-foreground"
        >
          <ChevronRight aria-hidden="true" className="size-3.5" />
        </AppLink>
      ) : null}
    </span>
  );
}
