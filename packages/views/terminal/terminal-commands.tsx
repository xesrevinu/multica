"use client";

import { useState } from "react";
import { Bot, FolderKanban, Home, Plus, SquareTerminal } from "lucide-react";
import type { Agent, Project } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Command,
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@multica/ui/components/ui/command";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "../common/actor-avatar";
import { useT } from "../i18n";
import { ProjectIcon } from "../projects/components/project-icon";

export function TerminalNewMenu({
  disabled,
  projects,
  agents,
  onNewShell,
  onOpenProject,
  onStartAgent,
}: {
  disabled?: boolean;
  projects: Project[];
  agents: Agent[];
  onNewShell: () => void;
  onOpenProject: (projectId: string) => void;
  onStartAgent: (agentId: string) => void;
}) {
  const { t } = useT("layout");
  const [projectOpen, setProjectOpen] = useState(false);
  const [agentOpen, setAgentOpen] = useState(false);

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              type="button"
              size="icon-xs"
              variant="ghost"
              disabled={disabled}
              aria-label={t(($) => $.terminal.new_session)}
            >
              <Plus className="size-3.5" />
            </Button>
          }
        />
        <DropdownMenuContent align="end" className="min-w-48">
          <DropdownMenuItem onClick={onNewShell}>
            <SquareTerminal className="size-3.5" />
            {t(($) => $.terminal.new_shell)}
          </DropdownMenuItem>
          <DropdownMenuItem onClick={() => setProjectOpen(true)}>
            <FolderKanban className="size-3.5" />
            {t(($) => $.terminal.open_project)}
          </DropdownMenuItem>
          <DropdownMenuItem disabled={agents.length === 0} onClick={() => setAgentOpen(true)}>
            <Bot className="size-3.5" />
            {t(($) => $.terminal.start_agent)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      {projectOpen ? (
      <CommandDialog
        open
        onOpenChange={setProjectOpen}
        title={t(($) => $.terminal.open_project)}
        description={t(($) => $.terminal.search_projects)}
      >
        <Command>
          <CommandInput placeholder={t(($) => $.terminal.search_projects)} />
          <CommandList>
            <CommandEmpty>{t(($) => $.terminal.empty_sessions)}</CommandEmpty>
            <CommandGroup>
              <CommandItem
                value="home"
                onSelect={() => {
                  setProjectOpen(false);
                  onNewShell();
                }}
              >
                <Home className="size-3.5" />
                {t(($) => $.terminal.home)}
              </CommandItem>
              {projects.map((project) => (
                <CommandItem
                  key={project.id}
                  value={`${project.title} ${project.id}`}
                  onSelect={() => {
                    setProjectOpen(false);
                    onOpenProject(project.id);
                  }}
                >
                  <ProjectIcon project={project} size="sm" />
                  {project.title}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </CommandDialog>
      ) : null}

      {agentOpen ? (
      <CommandDialog
        open
        onOpenChange={setAgentOpen}
        title={t(($) => $.terminal.start_agent)}
        description={t(($) => $.terminal.search_agents)}
      >
        <Command>
          <CommandInput placeholder={t(($) => $.terminal.search_agents)} />
          <CommandList>
            <CommandEmpty>{t(($) => $.terminal.no_online_agents)}</CommandEmpty>
            <CommandGroup>
              {agents.map((agent) => (
                <CommandItem
                  key={agent.id}
                  value={`${agent.name} ${agent.id}`}
                  onSelect={() => {
                    setAgentOpen(false);
                    onStartAgent(agent.id);
                  }}
                >
                  <ActorAvatar actorType="agent" actorId={agent.id} size="sm" showStatusDot />
                  {agent.name}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </CommandDialog>
      ) : null}
    </>
  );
}
