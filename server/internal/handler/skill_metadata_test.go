package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// newLargeSkillFixture builds a skill whose file bodies dwarf its metadata,
// which is the condition GH #7498 reports: the response was large because it
// inlined every body, and the command that would have said so was the one that
// could not finish.
func newLargeSkillFixture(t *testing.T) (skillID string, bodies map[string]string) {
	t.Helper()

	skillID = dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "skill-metadata-fixture",
		"description":  "fixture for include= tests",
		"content":      strings.Repeat("s", 4096),
		"created_by":   testUserID,
	})

	bodies = map[string]string{
		"reference.md":     strings.Repeat("r", 60_000),
		"scripts/setup.sh": strings.Repeat("x", 128),
	}
	for path, body := range bodies {
		dbfx.Insert(t, "skill_file", testutil.Cols{
			"skill_id": skillID,
			"path":     path,
			"content":  body,
		})
	}
	return skillID, bodies
}

func skillRequest(t *testing.T, skillID, path string) *http.Request {
	t.Helper()
	return withURLParam(newRequest(http.MethodGet, path, nil), "id", skillID)
}


func TestListSkillFilesIncludeContentReturnsBodies(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID, bodies := newLargeSkillFixture(t)

	req := skillRequest(t, skillID, "/api/skills/"+skillID+"/files?include=content")
	resp := testutil.Decode[[]SkillFileResponse](t, testHandler.ListSkillFiles, req, http.StatusOK)

	if len(resp) != len(bodies) {
		t.Fatalf("got %d files, want %d", len(resp), len(bodies))
	}
	for _, f := range resp {
		if want := bodies[f.Path]; f.Content != want {
			t.Errorf("%s: content length = %d, want %d", f.Path, len(f.Content), len(want))
		}
	}
}

// Neither endpoint may change what a request without `?include=` receives.
// Their callers are installed software — desktop builds, and older CLI
// versions whose `skill files list --output json` scripts read `content` — so
// a server deploy that flipped a default would silently take content away from
// clients that have no way to ask for it back.
func TestSkillEndpointsWithoutIncludeStillReturnContent(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID, bodies := newLargeSkillFixture(t)

	t.Run("files listing", func(t *testing.T) {
		req := skillRequest(t, skillID, "/api/skills/"+skillID+"/files")
		resp := testutil.Decode[[]SkillFileResponse](t, testHandler.ListSkillFiles, req, http.StatusOK)
		if len(resp) != len(bodies) {
			t.Fatalf("got %d files, want %d", len(resp), len(bodies))
		}
		for _, f := range resp {
			if want := bodies[f.Path]; f.Content != want {
				t.Errorf("%s: content length = %d, want %d", f.Path, len(f.Content), len(want))
			}
		}
	})

	t.Run("skill detail", func(t *testing.T) {
		req := skillRequest(t, skillID, "/api/skills/"+skillID)
		resp := testutil.Decode[SkillWithFilesResponse](t, testHandler.GetSkill, req, http.StatusOK)
		if len(resp.Content) != 4096 {
			t.Errorf("content length = %d, want 4096", len(resp.Content))
		}
		for _, f := range resp.Files {
			if want := bodies[f.Path]; f.Content != want {
				t.Errorf("%s: content length = %d, want %d", f.Path, len(f.Content), len(want))
			}
		}
	})
}


// A metadata response must stay small no matter how big the skill gets — that
// is the property the fix is actually about. Ten more files of 60KB each add
// rows, not megabytes.

// The size and hash are computed in SQL, so they must agree with Go's view of
// the same bytes for every body a skill can legally hold.
//
// `sha256(content::bytea)` did not: that cast runs the bytea *input* parser
// over the text, so `\x41` hashed as the single byte `A`, and any bare
// backslash — a regex, a Windows path — failed the whole request with "invalid
// input syntax for type bytea". Skill files are full of both.

func TestSkillIncludeRejectsUnknownValue(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}
	skillID, _ := newLargeSkillFixture(t)

	testutil.Call(t, testHandler.GetSkill,
		skillRequest(t, skillID, "/api/skills/"+skillID+"?include=everything")).
		Want(http.StatusBadRequest)

	testutil.Call(t, testHandler.ListSkillFiles,
		skillRequest(t, skillID, "/api/skills/"+skillID+"/files?include=everything")).
		Want(http.StatusBadRequest)
}
