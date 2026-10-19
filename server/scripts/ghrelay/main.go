// Command ghrelay is a dev-only GitHub App helper for a local stack that
// GitHub cannot reach. It authenticates as the App (GITHUB_APP_ID +
// GITHUB_APP_PRIVATE_KEY) and only reads from GitHub.
//
//	ghrelay installations                 list installations (id, account)
//	ghrelay relay -to URL -repo o/r       poll the App's webhook deliveries and
//	                                      replay those for repo to URL, signed
//	                                      with GITHUB_WEBHOOK_SECRET
//
// Nothing is exposed to the internet and the App's webhook URL is untouched:
// production keeps receiving every delivery; the relay replays copies.
package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func appJWT() string {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(strings.TrimSpace(os.Getenv("GITHUB_APP_PRIVATE_KEY"))))
	if err != nil {
		fail("parse GITHUB_APP_PRIVATE_KEY failed (key not shown)")
	}
	now := time.Now()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iat": now.Add(-60 * time.Second).Unix(), "exp": now.Add(9 * time.Minute).Unix(), "iss": os.Getenv("GITHUB_APP_ID"),
	}).SignedString(key)
	if err != nil {
		fail(err.Error())
	}
	return tok
}

func fail(msg string) { fmt.Fprintln(os.Stderr, "ghrelay:", msg); os.Exit(1) }

func get(path string, out any) {
	req, _ := http.NewRequest("GET", "https://api.github.com"+path, nil)
	req.Header.Set("Authorization", "Bearer "+appJWT())
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		fail(err.Error())
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		fail(fmt.Sprintf("GET %s: %d %s", path, res.StatusCode, string(b[:min(len(b), 300)])))
	}
	if err := json.Unmarshal(b, out); err != nil {
		fail(err.Error())
	}
}

type delivery struct {
	ID          int64  `json:"id"`
	GUID        string `json:"guid"`
	Event       string `json:"event"`
	Action      string `json:"action"`
	Repository  int64  `json:"repository_id"`
	DeliveredAt string `json:"delivered_at"`
}

func main() {
	if len(os.Args) < 2 {
		fail("usage: ghrelay installations | relay -to URL -repo owner/name [-every 10s] [-once]")
	}
	switch os.Args[1] {
	case "installations":
		var xs []struct {
			ID      int64 `json:"id"`
			Account struct {
				Login string `json:"login"`
			} `json:"account"`
			Selection string `json:"repository_selection"`
		}
		get("/app/installations", &xs)
		for _, x := range xs {
			fmt.Printf("%d\t%s\t%s\n", x.ID, x.Account.Login, x.Selection)
		}
	case "relay":
		fs := flag.NewFlagSet("relay", flag.ExitOnError)
		to := fs.String("to", "http://localhost:18899/api/webhooks/github", "local webhook endpoint")
		repo := fs.String("repo", "", "owner/name to relay (required)")
		every := fs.Duration("every", 10*time.Second, "poll interval")
		once := fs.Bool("once", false, "one pass, then exit")
		since := fs.Duration("since", 10*time.Minute, "on start, replay deliveries newer than this")
		_ = fs.Parse(os.Args[2:])
		if *repo == "" {
			fail("-repo required")
		}
		secret := os.Getenv("GITHUB_WEBHOOK_SECRET")
		seen := map[string]bool{}
		cutoff := time.Now().Add(-*since)
		for {
			var ds []delivery
			get("/app/hook/deliveries?per_page=50", &ds)
			for i := len(ds) - 1; i >= 0; i-- { // oldest first
				d := ds[i]
				at, _ := time.Parse(time.RFC3339, d.DeliveredAt)
				if seen[d.GUID] || at.Before(cutoff) {
					continue
				}
				seen[d.GUID] = true
				var full struct {
					Request struct {
						Payload json.RawMessage `json:"payload"`
					} `json:"request"`
				}
				get(fmt.Sprintf("/app/hook/deliveries/%d", d.ID), &full)
				var probe struct {
					Repository struct {
						FullName string `json:"full_name"`
					} `json:"repository"`
				}
				_ = json.Unmarshal(full.Request.Payload, &probe)
				if !strings.EqualFold(probe.Repository.FullName, *repo) && d.Event != "installation" && d.Event != "installation_repositories" {
					continue
				}
				body := []byte(full.Request.Payload)
				mac := hmac.New(sha256.New, []byte(secret))
				mac.Write(body)
				req, _ := http.NewRequest("POST", *to, bytes.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-GitHub-Event", d.Event)
				req.Header.Set("X-GitHub-Delivery", d.GUID)
				req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
				res, err := http.DefaultClient.Do(req)
				code := 0
				if err == nil {
					code = res.StatusCode
					res.Body.Close()
				}
				fmt.Printf("%s relay %s %s.%s %s -> %d %v\n", time.Now().Format("15:04:05"), d.GUID[:8], d.Event, d.Action, probe.Repository.FullName, code, err)
			}
			if *once {
				return
			}
			time.Sleep(*every)
		}
	default:
		fail("unknown command " + os.Args[1])
	}
}
