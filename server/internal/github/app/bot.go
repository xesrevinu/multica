package app

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Bot is the App's GitHub bot account (<slug>[bot]), the identity commits and
// merges made with an installation token are attributed to.
type Bot struct {
	Slug  string // e.g. "multica-orchestrator"
	Login string // "<slug>[bot]"
	ID    int64  // the bot user's id (not the App id)
}

// Email is GitHub's noreply address for the bot: <id>+<slug>[bot]@users.noreply.github.com.
func (b Bot) Email() string { return fmt.Sprintf("%d+%s@users.noreply.github.com", b.ID, b.Login) }

// CoAuthoredBy is the commit trailer that credits the bot.
func (b Bot) CoAuthoredBy() string { return "Co-authored-by: " + b.Login + " <" + b.Email() + ">" }

// Bot resolves (and caches) the App's bot identity. The slug comes from
// Config.Slug or GET /app; the bot user id from GET /users/<slug>[bot] using
// the installation's token.
func (c *Client) Bot(ctx context.Context, installationID int64) (Bot, error) {
	if !c.Enabled() {
		return Bot{}, ErrDisabled
	}
	c.mu.Lock()
	if c.bot != nil {
		b := *c.bot
		c.mu.Unlock()
		return b, nil
	}
	slug := c.slug
	c.mu.Unlock()
	if slug == "" {
		jwtStr, err := c.JWT()
		if err != nil {
			return Bot{}, err
		}
		var a struct {
			Slug string `json:"slug"`
		}
		if err := c.send(ctx, 0, jwtStr, http.MethodGet, "/app", nil, &a); err != nil {
			return Bot{}, err
		}
		slug = a.Slug
	}
	login := slug + "[bot]"
	var u struct {
		ID int64 `json:"id"`
	}
	if err := c.Do(ctx, installationID, http.MethodGet, "/users/"+url.PathEscape(login), nil, &u); err != nil {
		return Bot{}, err
	}
	b := Bot{Slug: slug, Login: login, ID: u.ID}
	c.mu.Lock()
	c.bot = &b
	c.mu.Unlock()
	return b, nil
}
