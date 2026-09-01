"use client";

import { useMemo, useState } from "react";
import { Bot, Monitor } from "lucide-react";
import type { Agent } from "@multica/core/types";
import { ActorAvatar } from "../common/actor-avatar";
import { PillButton } from "../common/pill-button";
import {
  PickerEmpty,
  PickerItem,
  PropertyPicker,
} from "../issues/components/pickers/property-picker";
import { matchesPinyin } from "../editor/extensions/pinyin-match";
import { useT } from "../i18n";
import type { RuntimeMachine } from "../runtimes/components/runtime-machines";

export function TerminalMachinePicker({
  machines,
  machineId,
  onChange,
}: {
  machines: RuntimeMachine[];
  machineId: string | null;
  onChange: (id: string | null) => void;
}) {
  const { t } = useT("layout");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const current = machines.find((machine) => machine.id === machineId);
  const query = filter.trim().toLowerCase();
  const filtered = machines.filter(
    (machine) =>
      !query ||
      machine.title.toLowerCase().includes(query) ||
      matchesPinyin(machine.title, query),
  );

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setFilter("");
      }}
      width="w-56"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.terminal.search_machines)}
      onSearchChange={setFilter}
      triggerRender={<PillButton aria-label={t(($) => $.terminal.machine)} />}
      trigger={
        current ? (
          <>
            <span className="size-1.5 shrink-0 rounded-full bg-success" />
            <Monitor className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{current.title}</span>
          </>
        ) : (
          <>
            <Monitor className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate text-muted-foreground">
              {machines.length === 0
                ? t(($) => $.terminal.no_online_machines)
                : t(($) => $.terminal.machine)}
            </span>
          </>
        )
      }
    >
      {filtered.length === 0 ? <PickerEmpty /> : null}
      {filtered.map((machine) => (
        <PickerItem
          key={machine.id}
          selected={machine.id === machineId}
          onClick={() => {
            onChange(machine.id);
            setOpen(false);
          }}
        >
          <span className="size-1.5 shrink-0 rounded-full bg-success" />
          <Monitor className="size-3.5 shrink-0 text-muted-foreground" />
          <span className="min-w-0 truncate">{machine.title}</span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}

export function TerminalAgentPicker({
  agents,
  agentId,
  onChange,
}: {
  agents: Agent[];
  agentId: string | null;
  onChange: (id: string | null) => void;
}) {
  const { t } = useT("layout");
  const [open, setOpen] = useState(false);
  const [filter, setFilter] = useState("");
  const current = useMemo(
    () => agents.find((agent) => agent.id === agentId),
    [agents, agentId],
  );
  const query = filter.trim().toLowerCase();
  const filtered = agents.filter(
    (agent) =>
      !query ||
      agent.name.toLowerCase().includes(query) ||
      matchesPinyin(agent.name, query),
  );

  return (
    <PropertyPicker
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setFilter("");
      }}
      width="w-56"
      align="start"
      searchable
      searchPlaceholder={t(($) => $.terminal.search_agents)}
      onSearchChange={setFilter}
      triggerRender={<PillButton aria-label={t(($) => $.terminal.agent)} />}
      trigger={
        current ? (
          <>
            <ActorAvatar actorType="agent" actorId={current.id} size="sm" showStatusDot />
            <span className="truncate">{current.name}</span>
          </>
        ) : (
          <>
            <Bot className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate text-muted-foreground">
              {agents.length === 0
                ? t(($) => $.terminal.no_online_agents)
                : t(($) => $.terminal.agent)}
            </span>
          </>
        )
      }
    >
      <PickerItem
        emptyValue
        selected={!agentId}
        onClick={() => {
          onChange(null);
          setOpen(false);
        }}
      >
        <Bot className="size-3.5 text-muted-foreground" />
        <span className="text-muted-foreground">{t(($) => $.terminal.none)}</span>
      </PickerItem>
      {filtered.length === 0 ? <PickerEmpty /> : null}
      {filtered.map((agent) => (
        <PickerItem
          key={agent.id}
          selected={agent.id === agentId}
          onClick={() => {
            onChange(agent.id);
            setOpen(false);
          }}
        >
          <ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />
          <span className="min-w-0 truncate">{agent.name}</span>
        </PickerItem>
      ))}
    </PropertyPicker>
  );
}
