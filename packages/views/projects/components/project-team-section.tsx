"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useT } from "../../i18n";

const TEAMS_KEY = ["orchestrator", "teams"] as const;

/**
 * Orchestrator team for this project: spec summary (preset, roles, limits)
 * and the pause / resume kill switch. Hidden when the project is not
 * orchestrated. Editing the spec stays in YAML (`multica team apply`).
 */
export function ProjectTeamSection({ projectId }: { projectId: string }) {
  const { t } = useT("issues");
  const qc = useQueryClient();
  const { data } = useQuery({
    queryKey: TEAMS_KEY,
    queryFn: () => api.listOrchestratorTeams().catch(() => ({ teams: [] })),
    refetchInterval: 15000,
  });
  const toggle = useMutation({
    mutationFn: (paused: boolean) => api.setOrchestratorTeamPaused(projectId, paused),
    onSettled: () => qc.invalidateQueries({ queryKey: TEAMS_KEY }),
  });
  const team = data?.teams.find((x) => x.project_id === projectId);
  if (!team) return null;
  const roles = Object.entries(team.spec.roles ?? {});
  const limits = Object.entries(team.spec.limits ?? {});

  return (
    <section className="space-y-2 text-xs" data-testid="orchestrator-team-section">
      <div className="flex items-center justify-between">
        <span className="font-medium text-muted-foreground">{t(($) => $.orchestrator.teams)}</span>
        <button
          type="button"
          className="rounded border px-2 py-0.5 disabled:opacity-50"
          disabled={toggle.isPending}
          onClick={() => toggle.mutate(!team.paused)}
        >
          {team.paused ? t(($) => $.orchestrator.resume) : t(($) => $.orchestrator.pause)}
        </button>
      </div>
      {team.paused && (
        <div className="text-destructive">
          {t(($) => $.orchestrator.paused)}: {team.paused_reason}
        </div>
      )}
      <ul className="space-y-0.5">
        {team.spec.preset && <li className="text-muted-foreground">{team.spec.preset}</li>}
        {roles.map(([role, v]) => (
          <li key={role}>
            <span className="text-muted-foreground">{role}: </span>
            {Array.isArray(v) ? v.join(", ") : typeof v === "string" ? v : JSON.stringify(v)}
          </li>
        ))}
        {limits.length > 0 && (
          <li className="text-muted-foreground">{limits.map(([k, v]) => `${k}=${v}`).join(" · ")}</li>
        )}
      </ul>
    </section>
  );
}
