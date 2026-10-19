package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// ghStatus is one commit status from GET /repos/{o}/{r}/commits/{sha}/statuses
// (newest first).
type ghStatus struct {
	Context string `json:"context"`
	State   string `json:"state"`
	Creator *struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"creator"`
}

// appBotLogin is the login GitHub gives statuses created with the App's
// installation token: "<app slug>[bot]". "" when GITHUB_APP_SLUG is unset, in
// which case publishReviewVerdict asks the App client (app.Client.Bot).
func appBotLogin() string {
	slug := strings.TrimSpace(os.Getenv("GITHUB_APP_SLUG"))
	if slug == "" {
		return ""
	}
	return slug + "[bot]"
}

// appVerdict returns the state of the newest review-verdict status created by
// the App (appLogin), ignoring statuses from any other creator; "" if none.
func appVerdict(statuses []ghStatus, appLogin string) string {
	if appLogin == "" {
		return ""
	}
	for _, st := range statuses {
		if st.Context != ReviewVerdictCheck || st.Creator == nil {
			continue
		}
		if strings.EqualFold(st.Creator.Login, appLogin) && strings.EqualFold(st.Creator.Type, "Bot") {
			return strings.ToLower(st.State)
		}
	}
	return ""
}

// installationFor finds the App installation of the team's repo from the PR
// rows the App webhook keeps (the same source the PR card uses).
func (s *Service) installationFor(ctx context.Context, sp *Spec, num string) (int64, error) {
	parts := strings.SplitN(sp.Repo.Slug, "/", 2)
	if len(parts) != 2 {
		return 0, fmt.Errorf("repo.slug %q is not owner/name", sp.Repo.Slug)
	}
	var id int64
	err := s.opt.Pool.QueryRow(ctx, `SELECT installation_id FROM github_pull_request
		WHERE lower(repo_owner)=lower($1) AND lower(repo_name)=lower($2) AND installation_id IS NOT NULL
		ORDER BY (pr_number::text=$3) DESC, pr_updated_at DESC LIMIT 1`, parts[0], parts[1], num).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("no GitHub App installation known for %s (is the App installed on it?): %w", sp.Repo.Slug, err)
	}
	return id, nil
}

// publishReviewVerdict makes sure the head carries an App-created
// review-verdict=success status. It posts one with the installation token
// unless the App already did for this head; statuses by anyone else are
// ignored. The caller checked engine state (verdict=approve at this head).
func (s *Service) publishReviewVerdict(ctx context.Context, sp *Spec, num string, sn *Snapshot) error {
	gh := s.github()
	if !gh.Enabled() {
		return errors.New("GitHub App is not configured (GITHUB_APP_ID / GITHUB_APP_PRIVATE_KEY); the orchestrator publishes review-verdict only as the App")
	}
	inst, err := s.installationFor(ctx, sp, num)
	if err != nil {
		return err
	}
	login := appBotLogin()
	if login == "" {
		// The shared App client knows its own bot (GET /app when
		// GITHUB_APP_SLUG is unset, cached).
		bot, err := gh.App().Bot(ctx, inst)
		if err != nil {
			return fmt.Errorf("resolve the App's bot login (set GITHUB_APP_SLUG to skip): %w", err)
		}
		login = bot.Login
	}
	base := "/repos/" + sp.Repo.Slug
	var list []ghStatus
	if err := gh.REST(ctx, inst, "GET", base+"/commits/"+sn.Head+"/statuses?per_page=100", nil, &list); err != nil {
		return fmt.Errorf("list statuses: %w", err)
	}
	if appVerdict(list, login) == "success" {
		return nil
	}
	body := map[string]string{
		"state":       "success",
		"context":     ReviewVerdictCheck,
		"description": "multica orchestrator: approved at " + short(sn.Issue.Meta["reviewed_sha"], 12),
	}
	if err := gh.REST(ctx, inst, "POST", base+"/statuses/"+sn.Head, body, nil); err != nil {
		return fmt.Errorf("post status as the App (needs commit statuses: write): %w", err)
	}
	return nil
}
