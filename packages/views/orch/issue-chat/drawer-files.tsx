"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, FileCode2, FileMinus2, FilePlus2 } from "lucide-react";
import { orchPRFilesOptions } from "@multica/core/orchestrator";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { DiffStat } from "../parts/chips";

// FilesTab: the PR's changed files with their unified patches (GitHub App).
export function FilesTab({ wsId, issueKey, enabled }: { wsId: string; issueKey: string; enabled: boolean }) {
  const { t } = useT("orch");
  const { data, isLoading } = useQuery(orchPRFilesOptions(wsId, issueKey, enabled));
  if (isLoading) return <Skeleton className="m-4 h-24" />;
  const files = data?.files ?? [];
  if (files.length === 0) {
    return (
      <p className="px-4 py-6 text-center text-caption text-muted-foreground" data-testid="orch-files-empty">
        {data?.mode === "local" ? t(($) => $.drawer.files_local) : t(($) => $.drawer.no_files)}
      </p>
    );
  }
  const adds = files.reduce((n, f) => n + f.additions, 0);
  const dels = files.reduce((n, f) => n + f.deletions, 0);
  return (
    <div className="flex flex-col gap-2 px-4 py-3" data-testid="orch-files">
      <p className="flex items-center gap-2 text-caption text-muted-foreground">
        {t(($) => $.pr.files, { count: files.length })}
        <DiffStat additions={adds} deletions={dels} />
      </p>
      {files.map((f) => (
        <FileDiff key={f.filename} file={f} defaultOpen={files.length <= 3} />
      ))}
    </div>
  );
}

function FileDiff({
  file,
  defaultOpen,
}: {
  file: { filename: string; status: string; additions: number; deletions: number; patch?: string; previous_filename?: string };
  defaultOpen: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  const Icon = file.status === "added" ? FilePlus2 : file.status === "removed" ? FileMinus2 : FileCode2;
  return (
    <div className="overflow-hidden rounded-lg border border-border">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-center gap-1.5 bg-muted/40 px-2 py-1.5 text-left text-caption hover:bg-muted"
      >
        <ChevronRight className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
        <Icon className="size-3.5 shrink-0 text-muted-foreground" />
        <span className="min-w-0 flex-1 truncate font-mono text-micro text-foreground" title={file.filename}>
          {file.previous_filename ? `${file.previous_filename} → ` : ""}
          {file.filename}
        </span>
        <DiffStat additions={file.additions} deletions={file.deletions} className="text-micro" />
      </button>
      {open && file.patch && (
        <pre className="max-h-96 overflow-auto bg-background py-1 font-mono text-[11px] leading-[1.55]">
          {file.patch.split("\n").map((line, i) => (
            <div
              key={i}
              className={cn(
                "px-2 whitespace-pre",
                line.startsWith("+") && "bg-emerald-500/10 text-emerald-800 dark:text-emerald-300",
                line.startsWith("-") && "bg-red-500/10 text-red-800 dark:text-red-300",
                line.startsWith("@@") && "bg-sky-500/10 text-sky-800 dark:text-sky-300",
                !/^[+\-@]/.test(line) && "text-muted-foreground",
              )}
            >
              {line || " "}
            </div>
          ))}
        </pre>
      )}
    </div>
  );
}
