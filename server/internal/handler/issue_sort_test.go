package handler

import (
)



// TestListIssuesStatusSortFollowsCatalogOrder pins that `sort=status` ranks by
// the CONCRETE status key in catalog order, not by lifecycle category
// (MUL-7379).
//
// Sibling test TestListIssuesSortsByStatusAndUpdatedAt cannot catch the
// regression this guards: it uses one issue per lifecycle category, so a
// four-bucket ranking and a per-status ranking produce the same output. Here
// the unstarted group holds backlog AND todo, and the started group holds
// in_progress, in_review and blocked — collapsing to categories ties every one
// of those and lets the created_at tiebreak decide.
//
// The custom Started status is wedged BETWEEN two built-ins by position, which
// is what separates "ranks by the catalog" from "ranks by a hardcoded list of
// the seven built-ins": no static ordering of the built-ins can produce it.
