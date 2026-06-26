// Package bitbucket implements the forge operations over the Bitbucket Cloud
// REST API v2. It authenticates with HTTP Basic auth using an Atlassian account
// email + API token (app passwords were retired in 2026). It depends only on
// internal/domain and is wired in by forge.Select.
package bitbucket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/domain"
)

// Client talks to the Bitbucket Cloud REST API.
type Client struct {
	http      *http.Client
	base      string
	email     string
	token     string
	workspace string
	repoSlug  string
}

// New reads the email/token from the configured env vars and returns a Client.
func New(cfg config.BitbucketConfig) (*Client, error) {
	emailEnv := orStr(cfg.EmailEnv, "BITBUCKET_EMAIL")
	tokenEnv := orStr(cfg.TokenEnv, "BITBUCKET_API_TOKEN")
	email := os.Getenv(emailEnv)
	token := os.Getenv(tokenEnv)
	if email == "" || token == "" {
		return nil, fmt.Errorf("bitbucket forge requires %s (Atlassian account email) and %s (API token) env vars", emailEnv, tokenEnv)
	}
	base := strings.TrimRight(orStr(cfg.BaseURL, "https://api.bitbucket.org/2.0"), "/")
	return &Client{
		http:      &http.Client{Timeout: 30 * time.Second},
		base:      base,
		email:     email,
		token:     token,
		workspace: cfg.Workspace,
		repoSlug:  cfg.RepoSlug,
	}, nil
}

func (c *Client) Name() string { return "bitbucket" }

func (c *Client) ws(r domain.Repo) string {
	if r.Owner != "" {
		return r.Owner
	}
	return c.workspace
}

func (c *Client) slug(r domain.Repo) string {
	if r.Name != "" {
		return r.Name
	}
	return c.repoSlug
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return domain.NotFoundf("bitbucket %s", path)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("bitbucket %s %s: %s: %s", method, path, resp.Status, truncate(string(data), 300))
	}
	if out != nil && len(data) > 0 {
		return json.Unmarshal(data, out)
	}
	return nil
}

// getAll follows the `next` pagination links, invoking each on every value.
func (c *Client) getAll(ctx context.Context, path string, each func(json.RawMessage) error) error {
	next := c.base + path
	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return err
		}
		req.SetBasicAuth(c.email, c.token)
		req.Header.Set("Accept", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 {
			return fmt.Errorf("bitbucket GET %s: %s", next, resp.Status)
		}
		var page struct {
			Values []json.RawMessage `json:"values"`
			Next   string            `json:"next"`
		}
		if err := json.Unmarshal(data, &page); err != nil {
			return err
		}
		for _, v := range page.Values {
			if err := each(v); err != nil {
				return err
			}
		}
		next = page.Next
	}
	return nil
}

// ---- pull requests ----------------------------------------------------------

type bbPR struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	State       string `json:"state"`
	Source      struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
		Commit struct {
			Hash string `json:"hash"`
		} `json:"commit"`
	} `json:"source"`
	Destination struct {
		Branch struct {
			Name string `json:"name"`
		} `json:"branch"`
	} `json:"destination"`
	Author struct {
		DisplayName string `json:"display_name"`
	} `json:"author"`
	Links struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
	CreatedOn time.Time `json:"created_on"`
	UpdatedOn time.Time `json:"updated_on"`
}

func (p bbPR) toDomain() domain.PullRequest {
	pr := domain.PullRequest{
		Number: p.ID, ID: fmt.Sprint(p.ID), Title: p.Title, Body: p.Description,
		URL: p.Links.HTML.Href, HeadRef: p.Source.Branch.Name, BaseRef: p.Destination.Branch.Name,
		HeadSHA: p.Source.Commit.Hash, Author: p.Author.DisplayName, CreatedAt: p.CreatedOn, UpdatedAt: p.UpdatedOn,
	}
	switch strings.ToUpper(p.State) {
	case "OPEN":
		pr.State = domain.PRStateOpen
	case "MERGED":
		pr.State = domain.PRStateMerged
	default: // DECLINED, SUPERSEDED
		pr.State = domain.PRStateClosed
	}
	return pr
}

func (c *Client) GetPR(ctx context.Context, r domain.Repo, number int) (domain.PullRequest, error) {
	var p bbPR
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d", c.ws(r), c.slug(r), number)
	if err := c.do(ctx, http.MethodGet, path, nil, &p); err != nil {
		return domain.PullRequest{}, err
	}
	return p.toDomain(), nil
}

func (c *Client) PRForBranch(ctx context.Context, r domain.Repo, branch string) (*domain.PullRequest, error) {
	q := url.QueryEscape(fmt.Sprintf(`source.branch.name="%s"`, branch))
	var page struct {
		Values []bbPR `json:"values"`
	}
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests?q=%s&state=OPEN&pagelen=1", c.ws(r), c.slug(r), q)
	if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	if len(page.Values) == 0 {
		page.Values = nil
		path = fmt.Sprintf("/repositories/%s/%s/pullrequests?q=%s&pagelen=1", c.ws(r), c.slug(r), q)
		if err := c.do(ctx, http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		if len(page.Values) == 0 {
			return nil, nil
		}
	}
	pr := page.Values[0].toDomain()
	return &pr, nil
}

func (c *Client) OpenPR(ctx context.Context, r domain.Repo, o domain.OpenPROpts) (domain.PullRequest, error) {
	body := map[string]any{
		"title":       o.Title,
		"description": o.Body,
		"source":      map[string]any{"branch": map[string]string{"name": o.Head}},
	}
	if o.Base != "" {
		body["destination"] = map[string]any{"branch": map[string]string{"name": o.Base}}
	}
	if len(o.Reviewers) > 0 {
		revs := make([]map[string]string, 0, len(o.Reviewers))
		for _, rv := range o.Reviewers {
			revs = append(revs, map[string]string{"uuid": rv})
		}
		body["reviewers"] = revs
	}
	var p bbPR
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests", c.ws(r), c.slug(r))
	if err := c.do(ctx, http.MethodPost, path, body, &p); err != nil {
		return domain.PullRequest{}, err
	}
	return p.toDomain(), nil
}

// ---- checks (commit statuses) ----------------------------------------------

func mapBBState(s string) domain.CheckBucket {
	switch strings.ToUpper(s) {
	case "SUCCESSFUL":
		return domain.CheckPass
	case "FAILED":
		return domain.CheckFail
	case "STOPPED":
		return domain.CheckCancel
	default: // INPROGRESS, etc.
		return domain.CheckPending
	}
}

func (c *Client) ListChecks(ctx context.Context, r domain.Repo, number int) ([]domain.Check, error) {
	pr, err := c.GetPR(ctx, r, number)
	if err != nil {
		return nil, err
	}
	if pr.HeadSHA == "" {
		return nil, nil
	}
	var checks []domain.Check
	path := fmt.Sprintf("/repositories/%s/%s/commit/%s/statuses?pagelen=50", c.ws(r), c.slug(r), pr.HeadSHA)
	err = c.getAll(ctx, path, func(v json.RawMessage) error {
		var s struct {
			State       string `json:"state"`
			Key         string `json:"key"`
			Name        string `json:"name"`
			URL         string `json:"url"`
			Description string `json:"description"`
		}
		if err := json.Unmarshal(v, &s); err != nil {
			return nil // skip malformed
		}
		checks = append(checks, domain.Check{
			Name: orStr(s.Name, s.Key), Bucket: mapBBState(s.State), RawState: s.State,
			Link: s.URL, Description: s.Description,
		})
		return nil
	})
	return checks, err
}

func (c *Client) WatchChecks(ctx context.Context, r domain.Repo, number int, onTick func(domain.CheckSummary)) (domain.CheckSummary, error) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		checks, err := c.ListChecks(ctx, r, number)
		if err != nil {
			return domain.CheckSummary{}, err
		}
		sum := domain.Summarize(checks)
		if onTick != nil {
			onTick(sum)
		}
		if sum.Pending == 0 {
			return sum, nil
		}
		select {
		case <-ctx.Done():
			return sum, ctx.Err()
		case <-ticker.C:
		}
	}
}

// ---- comments ---------------------------------------------------------------

type bbComment struct {
	ID      int `json:"id"`
	Content struct {
		Raw string `json:"raw"`
	} `json:"content"`
	User struct {
		DisplayName string `json:"display_name"`
	} `json:"user"`
	CreatedOn time.Time `json:"created_on"`
	Links     struct {
		HTML struct {
			Href string `json:"href"`
		} `json:"html"`
	} `json:"links"`
	Inline *struct {
		Path string `json:"path"`
		To   int    `json:"to"`
	} `json:"inline"`
}

func (cm bbComment) toDomain() domain.Comment {
	c := domain.Comment{
		ID: fmt.Sprint(cm.ID), Author: cm.User.DisplayName, Body: cm.Content.Raw,
		URL: cm.Links.HTML.Href, CreatedAt: cm.CreatedOn,
	}
	if cm.Inline != nil {
		c.Path = cm.Inline.Path
		c.Line = cm.Inline.To
		c.IsReview = true
	}
	return c
}

func (c *Client) ListComments(ctx context.Context, r domain.Repo, number int) ([]domain.Comment, error) {
	var out []domain.Comment
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/comments?pagelen=50", c.ws(r), c.slug(r), number)
	err := c.getAll(ctx, path, func(v json.RawMessage) error {
		var cm bbComment
		if err := json.Unmarshal(v, &cm); err != nil {
			return nil
		}
		out = append(out, cm.toDomain())
		return nil
	})
	return out, err
}

func (c *Client) PostComment(ctx context.Context, r domain.Repo, number int, body string) (domain.Comment, error) {
	payload := map[string]any{"content": map[string]string{"raw": body}}
	var cm bbComment
	path := fmt.Sprintf("/repositories/%s/%s/pullrequests/%d/comments", c.ws(r), c.slug(r), number)
	if err := c.do(ctx, http.MethodPost, path, payload, &cm); err != nil {
		return domain.Comment{}, err
	}
	return cm.toDomain(), nil
}

// ---- issues -----------------------------------------------------------------

func (c *Client) GetIssue(ctx context.Context, r domain.Repo, id string) (domain.Issue, error) {
	var bi struct {
		ID      int    `json:"id"`
		Title   string `json:"title"`
		State   string `json:"state"`
		Content struct {
			Raw string `json:"raw"`
		} `json:"content"`
		Reporter struct {
			DisplayName string `json:"display_name"`
		} `json:"reporter"`
		Links struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	}
	path := fmt.Sprintf("/repositories/%s/%s/issues/%s", c.ws(r), c.slug(r), strings.TrimPrefix(id, "#"))
	if err := c.do(ctx, http.MethodGet, path, nil, &bi); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Issue{}, domain.ErrIssueTrackerDisabled
		}
		return domain.Issue{}, err
	}
	return domain.Issue{
		ID: fmt.Sprint(bi.ID), Number: bi.ID, Title: bi.Title, Body: bi.Content.Raw,
		State: bi.State, Author: bi.Reporter.DisplayName, URL: bi.Links.HTML.Href,
	}, nil
}

// ---- helpers ----------------------------------------------------------------

func orStr(v, fallback string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
