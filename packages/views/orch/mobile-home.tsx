"use client";

import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../navigation";
import { IssueAttentionList } from "./sidebar/issue-attention-list";

// Compact landing: on a phone the sidebar is a sheet, so the chat list page
// doubles as home and leads with the attention-sorted issues (needs-you
// first), then the agent conversations below it.
export function MobileIssueHome() {
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const { pathname } = useNavigation();
  return (
    <div className="border-b border-border/60 py-1" data-testid="orch-mobile-home">
      <IssueAttentionList wsId={wsId} pathname={pathname} issueHref={(key) => p.issueDetail(key)} />
    </div>
  );
}
