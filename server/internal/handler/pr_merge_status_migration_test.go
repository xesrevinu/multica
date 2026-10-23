package handler

import (

)

// TestPRMergeStatusMigration runs migration 551 against workspaces with each
// kind of PR history (MUL-7726). Workspaces whose merges never all completed
// their issues are pinned to "none"; the rest keep the Done default. The
// migration runs inside a rolled-back transaction, twice, to prove a replay is
// harmless.
