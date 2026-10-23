package handler

import (

)

// TestClaimTaskByRuntime_PopulatesIssueStatusCatalog verifies the claim
// response carries the workspace's active CUSTOM statuses — and only those —
// so the daemon can render them into the agent brief (MUL-6460). Built-ins
// stay off the wire (the daemon knows them), archived statuses stay off the
// wire (they reject writes), and entries arrive in catalog order (category
// rank first), because the daemon renders them verbatim without re-sorting.

// TestClaimTaskByRuntime_IssueStatusCatalogAbsentWithoutCustoms pins the
// compatibility contract: a workspace with no custom statuses claims with NO
// issue_statuses field at all (omitempty), which is what keeps the daemon's
// brief byte-identical to the pre-MUL-6460 form for existing deployments.

// The claim cap counts active customs only, preserves catalog order, and
// discloses server truncation independently of daemon rendering omissions.
