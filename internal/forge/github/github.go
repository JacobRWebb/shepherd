// Package github implements the forge operations over the gh CLI. It depends
// only on internal/domain and is wired in by forge.Select.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/sysproc"
)

const prFields = "number,id,title,body,state,isDraft,url,headRefName,baseRefName,headRefOid,author,mergeable,reviewDecision,createdAt,updatedAt"

// Client talks to GitHub through the gh CLI.
type Client struct {
	host             string
	defaultReviewers []string
	draft            bool
}

// New verifies gh is available and returns a Client.
func New(cfg config.GitHubConfig) (*Client, error) {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("github forge requires the gh CLI on PATH (install GitHub CLI): %w", err)
	}
	return &Client{host: cfg.Host, defaultReviewers: cfg.DefaultReviewers, draft: cfg.DraftPRs}, nil
}

func (c *Client) Name() string { return "github" }

// gh runs the gh CLI, returning stdout, stderr, and the exit code. A non-zero
// exit is not a Go error here (callers decide); only a failure to launch gh is.
func (c *Client) gh(ctx context.Context, stdin string, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	sysproc.Hide(cmd)
	env := os.Environ()
	if c.host != "" {
		env = append(env, "GH_HOST="+c.host)
	}
	cmd.Env = env
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return out.String(), errb.String(), ee.ExitCode(), nil
		}
		return out.String(), errb.String(), -1, err
	}
	return out.String(), errb.String(), 0, nil
}

func (c *Client) ghJSON(ctx context.Context, dst any, args ...string) error {
	out, errs, code, err := c.gh(ctx, "", args...)
	if err != nil {
		return err
	}
	if code != 0 {
		return classifyExit(code, errs, args)
	}
	out = strings.TrimSpace(out)
	if dst == nil || out == "" {
		return nil
	}
	return json.Unmarshal([]byte(out), dst)
}

func classifyExit(code int, stderr string, args []string) error {
	msg := strings.TrimSpace(stderr)
	low := strings.ToLower(msg)
	switch {
	case code == 4:
		return fmt.Errorf("gh is not authenticated (run `gh auth login`): %s", msg)
	case strings.Contains(low, "no pull requests found"), strings.Contains(low, "not found"), strings.Contains(low, "could not resolve"):
		return domain.NotFoundf("%s", msg)
	default:
		return fmt.Errorf("gh %s: exit %d: %s", strings.Join(args, " "), code, msg)
	}
}

func (c *Client) repoArgs(r domain.Repo) []string {
	if r.Owner != "" && r.Name != "" {
		return []string{"-R", r.Owner + "/" + r.Name}
	}
	return nil
}

// ---- pull requests ----------------------------------------------------------

type ghPR struct {
	Number      int    `json:"number"`
	ID          string `json:"id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	IsDraft     bool   `json:"isDraft"`
	URL         string `json:"url"`
	HeadRefName string `json:"headRefName"`
	BaseRefName string `json:"baseRefName"`
	HeadRefOid  string `json:"headRefOid"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
	Mergeable      string    `json:"mergeable"`
	ReviewDecision string    `json:"reviewDecision"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func (p ghPR) toDomain() domain.PullRequest {
	pr := domain.PullRequest{
		Number: p.Number, ID: p.ID, Title: p.Title, Body: p.Body, IsDraft: p.IsDraft,
		URL: p.URL, HeadRef: p.HeadRefName, BaseRef: p.BaseRefName, HeadSHA: p.HeadRefOid,
		Author: p.Author.Login, ReviewState: p.ReviewDecision, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	switch strings.ToUpper(p.State) {
	case "OPEN":
		pr.State = domain.PRStateOpen
	case "MERGED":
		pr.State = domain.PRStateMerged
	case "CLOSED":
		pr.State = domain.PRStateClosed
	}
	switch strings.ToUpper(p.Mergeable) {
	case "MERGEABLE":
		b := true
		pr.Mergeable = &b
	case "CONFLICTING":
		b := false
		pr.Mergeable = &b
	}
	return pr
}

func (c *Client) GetPR(ctx context.Context, r domain.Repo, number int) (domain.PullRequest, error) {
	var p ghPR
	args := append([]string{"pr", "view", strconv.Itoa(number)}, c.repoArgs(r)...)
	args = append(args, "--json", prFields)
	if err := c.ghJSON(ctx, &p, args...); err != nil {
		return domain.PullRequest{}, err
	}
	return p.toDomain(), nil
}

func (c *Client) PRForBranch(ctx context.Context, r domain.Repo, branch string) (*domain.PullRequest, error) {
	var prs []ghPR
	args := append([]string{"pr", "list"}, c.repoArgs(r)...)
	args = append(args, "--head", branch, "--state", "all", "-L", "1", "--json", prFields)
	if err := c.ghJSON(ctx, &prs, args...); err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	pr := prs[0].toDomain()
	return &pr, nil
}

func (c *Client) OpenPR(ctx context.Context, r domain.Repo, o domain.OpenPROpts) (domain.PullRequest, error) {
	args := append([]string{"pr", "create"}, c.repoArgs(r)...)
	args = append(args, "--title", o.Title, "--body-file", "-", "--head", o.Head)
	if o.Base != "" {
		args = append(args, "--base", o.Base)
	}
	if o.Draft || c.draft {
		args = append(args, "--draft")
	}
	for _, rev := range orDefault(o.Reviewers, c.defaultReviewers) {
		args = append(args, "--reviewer", rev)
	}
	for _, l := range o.Labels {
		args = append(args, "--label", l)
	}

	out, errs, code, err := c.gh(ctx, o.Body, args...)
	if err != nil {
		return domain.PullRequest{}, err
	}
	if code != 0 {
		if strings.Contains(strings.ToLower(errs), "already exists") {
			if pr, perr := c.PRForBranch(ctx, r, o.Head); perr == nil && pr != nil {
				return *pr, nil
			}
		}
		return domain.PullRequest{}, classifyExit(code, errs, args)
	}
	if num := prNumberFromURL(lastNonEmptyLine(out)); num > 0 {
		return c.GetPR(ctx, r, num)
	}
	if pr, perr := c.PRForBranch(ctx, r, o.Head); perr == nil && pr != nil {
		return *pr, nil
	}
	return domain.PullRequest{URL: lastNonEmptyLine(out), HeadRef: o.Head, BaseRef: o.Base, Title: o.Title, State: domain.PRStateOpen}, nil
}

// ---- checks -----------------------------------------------------------------

func mapBucket(b string) domain.CheckBucket {
	switch b {
	case "pass":
		return domain.CheckPass
	case "fail":
		return domain.CheckFail
	case "skipping":
		return domain.CheckSkip
	case "cancel":
		return domain.CheckCancel
	default:
		return domain.CheckPending
	}
}

func (c *Client) ListChecks(ctx context.Context, r domain.Repo, number int) ([]domain.Check, error) {
	args := append([]string{"pr", "checks", strconv.Itoa(number)}, c.repoArgs(r)...)
	args = append(args, "--json", "name,state,bucket,workflow,link,startedAt,completedAt,description")
	out, errs, code, err := c.gh(ctx, "", args...)
	if err != nil {
		return nil, err
	}
	// exit 0 = all pass, 1 = some failing, 8 = pending: all are parseable.
	if code != 0 && code != 1 && code != 8 {
		if strings.Contains(strings.ToLower(errs), "no checks") {
			return nil, nil
		}
		return nil, classifyExit(code, errs, args)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var raw []struct {
		Name        string    `json:"name"`
		State       string    `json:"state"`
		Bucket      string    `json:"bucket"`
		Workflow    string    `json:"workflow"`
		Link        string    `json:"link"`
		StartedAt   time.Time `json:"startedAt"`
		CompletedAt time.Time `json:"completedAt"`
		Description string    `json:"description"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return nil, err
	}
	checks := make([]domain.Check, 0, len(raw))
	for _, x := range raw {
		checks = append(checks, domain.Check{
			Name: x.Name, Bucket: mapBucket(x.Bucket), RawState: x.State, Workflow: x.Workflow,
			Link: x.Link, Description: x.Description, StartedAt: x.StartedAt, CompletedAt: x.CompletedAt,
		})
	}
	return checks, nil
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

func (c *Client) ListComments(ctx context.Context, r domain.Repo, number int) ([]domain.Comment, error) {
	var out []domain.Comment

	var top struct {
		Comments []struct {
			ID     string `json:"id"`
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Body      string    `json:"body"`
			URL       string    `json:"url"`
			CreatedAt time.Time `json:"createdAt"`
		} `json:"comments"`
	}
	args := append([]string{"pr", "view", strconv.Itoa(number)}, c.repoArgs(r)...)
	args = append(args, "--json", "comments")
	if err := c.ghJSON(ctx, &top, args...); err != nil {
		return nil, err
	}
	for _, cm := range top.Comments {
		id := cm.ID
		if id == "" {
			id = cm.URL // fall back to URL so each comment still has a stable key
		}
		out = append(out, domain.Comment{ID: id, Author: cm.Author.Login, Body: cm.Body, URL: cm.URL, CreatedAt: cm.CreatedAt})
	}

	// Inline review comments are not in `--json comments`; fetch via the REST API.
	if r.Owner != "" && r.Name != "" {
		var inline []struct {
			ID      int64  `json:"id"`
			Body    string `json:"body"`
			Path    string `json:"path"`
			Line    int    `json:"line"`
			HTMLURL string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			CreatedAt time.Time `json:"created_at"`
		}
		apiPath := fmt.Sprintf("repos/%s/%s/pulls/%d/comments", r.Owner, r.Name, number)
		if err := c.ghJSON(ctx, &inline, "api", apiPath, "--paginate"); err == nil {
			for _, ic := range inline {
				out = append(out, domain.Comment{
					ID: fmt.Sprintf("review-%d", ic.ID), Author: ic.User.Login, Body: ic.Body, Path: ic.Path, Line: ic.Line,
					IsReview: true, URL: ic.HTMLURL, CreatedAt: ic.CreatedAt,
				})
			}
		}
	}
	return out, nil
}

func (c *Client) PostComment(ctx context.Context, r domain.Repo, number int, body string) (domain.Comment, error) {
	args := append([]string{"pr", "comment", strconv.Itoa(number)}, c.repoArgs(r)...)
	args = append(args, "--body-file", "-")
	out, errs, code, err := c.gh(ctx, body, args...)
	if err != nil {
		return domain.Comment{}, err
	}
	if code != 0 {
		return domain.Comment{}, classifyExit(code, errs, args)
	}
	return domain.Comment{Body: body, URL: lastNonEmptyLine(out)}, nil
}

// ---- issues -----------------------------------------------------------------

func (c *Client) GetIssue(ctx context.Context, r domain.Repo, id string) (domain.Issue, error) {
	num := strings.TrimPrefix(id, "#")
	if !isNumeric(num) {
		return domain.Issue{}, fmt.Errorf("%w: %q", domain.ErrUnsupportedIssueRef, id)
	}
	var gi struct {
		Number int    `json:"number"`
		ID     string `json:"id"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		State  string `json:"state"`
		URL    string `json:"url"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	args := append([]string{"issue", "view", num}, c.repoArgs(r)...)
	args = append(args, "--json", "number,id,title,body,state,url,author,labels")
	if err := c.ghJSON(ctx, &gi, args...); err != nil {
		return domain.Issue{}, err
	}
	iss := domain.Issue{
		Number: gi.Number, ID: gi.ID, Title: gi.Title, Body: gi.Body,
		State: gi.State, URL: gi.URL, Author: gi.Author.Login,
	}
	for _, l := range gi.Labels {
		iss.Labels = append(iss.Labels, l.Name)
	}
	return iss, nil
}

// ---- helpers ----------------------------------------------------------------

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func prNumberFromURL(url string) int {
	url = strings.TrimRight(strings.TrimSpace(url), "/")
	if i := strings.LastIndex(url, "/"); i >= 0 {
		n, _ := strconv.Atoi(url[i+1:])
		return n
	}
	return 0
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

func orDefault(v, def []string) []string {
	if len(v) > 0 {
		return v
	}
	return def
}
