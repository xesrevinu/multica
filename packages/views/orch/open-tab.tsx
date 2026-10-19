"use client";

import { useCallback } from "react";
import { AppWindow } from "lucide-react";
import { useWorkspacePaths } from "@multica/core/paths";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";

// Opening an issue "as a tab" rides the app's session tabs: every tab is a
// URL, so later kinds (files, terminal) only need a route. On compact
// layouts — and anywhere session tabs are off — it degrades to navigation.
export function useOpenIssueTab() {
  const navigation = useNavigation();
  const p = useWorkspacePaths();
  return useCallback(
    (key: string, title?: string) => {
      const path = p.issueDetail(key);
      if (navigation.openInNewTab) navigation.openInNewTab(path, title ? `${key} ${title}` : key, { activate: true });
      else navigation.push(path);
    },
    [navigation, p],
  );
}

export function OpenTabButton({ issueKey, title, className }: { issueKey: string; title?: string; className?: string }) {
  const { t } = useT("orch");
  const open = useOpenIssueTab();
  const label = t(($) => $.sidebar.open_in_tab);
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      data-testid="orch-open-tab"
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        open(issueKey, title);
      }}
      className={cn("shrink-0 rounded-sm p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground", className)}
    >
      <AppWindow className="size-3.5" />
    </button>
  );
}
