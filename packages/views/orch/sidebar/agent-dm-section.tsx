"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, MessageSquarePlus } from "lucide-react";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { ChatSession } from "@multica/core/types";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import {
  SidebarGroup,
  SidebarGroupAction,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@multica/ui/components/ui/sidebar";
import { cn } from "@multica/ui/lib/utils";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";

const ITEM =
  "text-muted-foreground transition-colors duration-75 hover:not-data-active:bg-sidebar-accent/70 data-active:bg-sidebar-accent data-active:text-sidebar-accent-foreground";

export type AgentDmSectionProps = {
  wsId: string;
  chatHref: string;
  chatSessions: readonly ChatSession[];
  /** Session the user is reading right now (its unread is not counted). */
  viewedSessionId: string | null;
  /** Currently open session from the URL when on the chat route. */
  activeSessionId: string | null;
  activeAgentId: string | null;
  busyAgentIds: ReadonlySet<string>;
};

// Agent DMs at the top of the sidebar: one row per active agent, linking to
// its latest chat session (or starting one). Reuses the existing chat
// sessions; nothing new on the server.
export function AgentDmSection({ wsId, chatHref, chatSessions, viewedSessionId, activeSessionId, activeAgentId, busyAgentIds }: AgentDmSectionProps) {
  const { t } = useT("orch");
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled: !!wsId });
  const rows = useMemo(() => {
    const byAgent = new Map<string, { latest?: ChatSession; unread: number; at: number }>();
    for (const s of chatSessions) {
      if (s.status === "archived") continue;
      const cur = byAgent.get(s.agent_id) ?? { unread: 0, at: 0 };
      const at = new Date(s.last_message?.created_at ?? s.updated_at ?? s.created_at ?? 0).getTime() || 0;
      if (!cur.latest || at > cur.at) {
        cur.latest = s;
        cur.at = at;
      }
      if (s.id !== viewedSessionId) cur.unread += s.unread_count ?? 0;
      byAgent.set(s.agent_id, cur);
    }
    return agents
      .filter((a) => !a.archived_at)
      .map((a) => ({ agent: a, ...(byAgent.get(a.id) ?? { unread: 0, at: 0 }) }))
      .sort((x, y) => y.unread - x.unread || y.at - x.at || x.agent.name.localeCompare(y.agent.name));
  }, [agents, chatSessions, viewedSessionId]);

  return (
    <Collapsible defaultOpen>
      <SidebarGroup className="group/dms">
        <SidebarGroupLabel
          render={<CollapsibleTrigger />}
          className="group/trigger cursor-pointer hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground"
        >
          <span>{t(($) => $.sidebar.agents)}</span>
          <ChevronRight className="!size-3 ml-1 stroke-[2.5] transition-transform duration-200 group-data-[panel-open]/trigger:rotate-90" />
        </SidebarGroupLabel>
        <SidebarGroupAction render={<AppLink href={chatHref} aria-label={t(($) => $.sidebar.new_chat)} />} title={t(($) => $.sidebar.new_chat)}>
          <MessageSquarePlus />
        </SidebarGroupAction>
        <CollapsibleContent>
          <SidebarGroupContent>
            <SidebarMenu className="gap-0.5" data-testid="orch-dm-list">
              {rows.length === 0 && (
                <p className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.sidebar.no_agents)}</p>
              )}
              {rows.map(({ agent, latest, unread }) => {
                const href = latest
                  ? `${chatHref}?session=${encodeURIComponent(latest.id)}`
                  : `${chatHref}?agent=${encodeURIComponent(agent.id)}`;
                const active = (!!latest && latest.id === activeSessionId) || (!activeSessionId && activeAgentId === agent.id);
                const busy = busyAgentIds.has(agent.id);
                return (
                  <SidebarMenuItem key={agent.id}>
                    <SidebarMenuButton isActive={active} render={<AppLink href={href} title={agent.name} />} className={cn(ITEM, "h-9")}>
                      <span className="relative">
                        <ActorAvatar name={agent.name} initials={agent.name.charAt(0).toUpperCase()} avatarUrl={agent.avatar_url ?? undefined} size="xs" />
                        <span
                          className={cn(
                            "absolute -right-0.5 -bottom-0.5 size-1.5 rounded-full ring-1 ring-sidebar",
                            busy ? "bg-emerald-500 animate-pulse" : agent.runtime_availability === "online" ? "bg-emerald-500/60" : "bg-muted-foreground/40",
                          )}
                        />
                      </span>
                      <span className={cn("truncate", unread > 0 && "font-medium text-foreground")}>{agent.name}</span>
                      {unread > 0 && (
                        <span className="ml-auto rounded-full bg-brand px-1.5 text-micro font-medium text-white tabular-nums">{unread > 99 ? "99+" : unread}</span>
                      )}
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </CollapsibleContent>
      </SidebarGroup>
    </Collapsible>
  );
}
