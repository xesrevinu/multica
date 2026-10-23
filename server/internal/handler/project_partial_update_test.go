package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"database/sql"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type projectReadPauseDB struct {
	db.DBTX
	paused, release chan struct{}
}

func (d projectReadPauseDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	row := d.DBTX.QueryRowContext(ctx, query, args...)
	if strings.HasPrefix(query, "-- name: GetProjectInWorkspace :one") {
		close(d.paused)
		<-d.release
	}
	return row
}

func projectNullableValues(p ProjectResponse) map[string]*string {
	return map[string]*string{
		"description": p.Description, "icon": p.Icon,
		"lead_type": p.LeadType, "lead_id": p.LeadID,
		"start_date": p.StartDate, "due_date": p.DueDate,
	}
}


func TestUpdateProjectNullableFieldPresence(t *testing.T) {
	agentID := dbfx.Agent(t, "project lead", testRuntimeID)
	initial := map[string]string{
		"description": "original description", "icon": "original icon",
		"lead_type": "member", "lead_id": testUserID,
		"start_date": "2026-03-01", "due_date": "2026-03-31",
	}
	values := map[string]string{
		"description": "new description", "icon": "new icon",
		"lead_type": "agent", "lead_id": agentID,
		"start_date": "2026-04-01", "due_date": "2026-04-30",
	}
	for field, value := range values {
		modes := []string{"omitted", "set", "null"}
		if field == "start_date" || field == "due_date" {
			modes = append(modes, "empty")
		}
		for _, mode := range modes {
			t.Run(field+"/"+mode, func(t *testing.T) {
				cols := testutil.Cols{}
				for key, value := range initial {
					cols[key] = value
				}
				projectID := dbfx.Project(t, "original title", cols)
				body := map[string]any{"title": "updated title", "status": nil, "priority": nil}
				switch mode {
				case "set":
					body[field] = value
				case "null":
					body[field] = nil
				case "empty":
					body[field] = ""
				}
				var updated ProjectResponse
				testutil.Call(t, testHandler.UpdateProject, withURLParam(newRequest("PUT", "/api/projects/"+projectID, body), "id", projectID)).Want(http.StatusOK).JSON(&updated)
				stored, err := testHandler.Queries.GetProjectInWorkspace(context.Background(), db.GetProjectInWorkspaceParams{
					ID: parseUUID(projectID), WorkspaceID: parseUUID(testWorkspaceID),
				})
				if err != nil {
					t.Fatal(err)
				}
				for label, p := range map[string]ProjectResponse{"response": updated, "database": projectToResponse(stored)} {
					if p.Title != "updated title" || p.Status != "planned" || p.Priority != "none" {
						t.Errorf("%s nonnullable fields = (%q, %q, %q)", label, p.Title, p.Status, p.Priority)
					}
					for key, got := range projectNullableValues(p) {
						want := initial[key]
						if key == field {
							if mode == "null" || mode == "empty" {
								if got != nil {
									t.Errorf("%s %s = %q, want null", label, key, *got)
								}
								continue
							}
							if mode == "set" {
								want = value
							}
						}
						if got == nil || *got != want {
							t.Errorf("%s %s = %v, want %q", label, key, got, want)
						}
					}
				}
			})
		}
	}
}
