package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var githubTokenCmd = &cobra.Command{
	Use:    "github-token",
	Short:  "Print the running task's short-lived GitHub App token (for agents inside a daemon task)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   runGitHubToken,
}

func init() {
	githubTokenCmd.Flags().Bool("git-credential", false, "Answer a git credential-helper `get` on stdin")
}

func runGitHubToken(cmd *cobra.Command, _ []string) error {
	port := os.Getenv("MULTICA_DAEMON_PORT")
	taskToken := os.Getenv("MULTICA_TOKEN")
	if port == "" || taskToken == "" {
		return fmt.Errorf("github-token runs only inside a daemon task (MULTICA_DAEMON_PORT and MULTICA_TOKEN)")
	}
	token, err := fetchTaskGitHubToken(cmd.Context(), "http://127.0.0.1:"+port+"/github/token", taskToken)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if ok, _ := cmd.Flags().GetBool("git-credential"); ok {
		_, err = fmt.Fprintf(out, "username=x-access-token\npassword=%s\n", token)
		return err
	}
	_, err = fmt.Fprintln(out, token)
	return err
}

func fetchTaskGitHubToken(parent context.Context, url, taskToken string) (string, error) {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+taskToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("connect to daemon: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("daemon refused github token: %d %s", resp.StatusCode, strings.TrimSpace(firstLineOf(string(body))))
	}
	var v struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &v); err != nil || v.Token == "" {
		return "", fmt.Errorf("daemon returned no github token")
	}
	return v.Token, nil
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
