// Package crew decomposes a task description into parallel subtasks, creates one
// worktree per subtask, launches an agent in each via the session backend, and
// monitors them to completion.
package crew

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/agent"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/gitutil"
	"github.com/JacobRWebb/shepherd/internal/session"
	"github.com/JacobRWebb/shepherd/internal/worktree"
)

// Deps are crew's collaborators.
type Deps struct {
	Worktrees worktree.Manager
	Sessions  session.SessionBackend
	Agent     agent.Launcher
	RepoRoot  string
	Log       *zerolog.Logger
}

// Options configure a crew run.
type Options struct {
	Description string
	Agents      int
	TasksFile   string
	Base        string
	Model       string
	Detach      bool
	Keep        bool
}

// Task is one planned subtask.
type Task struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// AgentOutcome is the result for one crew agent.
type AgentOutcome struct {
	Index    int    `json:"index"`
	Task     Task   `json:"task"`
	Name     string `json:"name"`
	Branch   string `json:"branch"`
	Path     string `json:"path"`
	State    string `json:"state"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Summary  string `json:"summary,omitempty"`
	DiffStat string `json:"diffstat,omitempty"`
}

// Result is the crew run outcome.
type Result struct {
	CrewID   string         `json:"crew_id"`
	Tasks    []Task         `json:"tasks"`
	Agents   []AgentOutcome `json:"agents"`
	Detached bool           `json:"detached"`
}

// Run plans, launches, and (unless detached) monitors a crew.
func Run(ctx context.Context, d Deps, o Options) (Result, error) {
	crewID := uuid.NewString()[:4]
	tasks, err := planTasks(ctx, d, o)
	if err != nil {
		return Result{}, err
	}
	res := Result{CrewID: crewID, Tasks: tasks, Detached: o.Detach}

	type live struct {
		idx  int
		task Task
		wt   domain.Worktree
		name string
	}
	var lives []live

	for i, t := range tasks {
		name := fmt.Sprintf("crew-%s-agent-%d", crewID, i+1)
		wt, werr := d.Worktrees.Create(ctx, domain.Task{Raw: name, Title: name, Source: domain.TaskSourceFreeText}, o.Base)
		if werr != nil {
			return res, fmt.Errorf("creating worktree for agent %d: %w", i+1, werr)
		}
		spec := agent.HeadlessSpec{
			Spec: agent.Spec{
				Prompt:         agentPrompt(t),
				Model:          o.Model,
				PermissionMode: agent.PermissionBypass,
				SessionID:      uuid.NewString(),
			},
			OutputFormat: "stream-json", // NDJSON to the session log => live progress
		}
		if _, serr := d.Sessions.Start(ctx, session.Spec{
			Name:    name,
			Dir:     wt.Path,
			Program: d.Agent.Binary(),
			Args:    d.Agent.HeadlessArgs(spec),
			Labels: map[string]string{
				"crew_id": crewID, "task_index": fmt.Sprint(i + 1), "task": t.Title, "branch": wt.Branch,
			},
		}); serr != nil {
			return res, fmt.Errorf("launching agent %d: %w", i+1, serr)
		}
		d.Log.Info().Str("agent", name).Str("branch", wt.Branch).Str("task", t.Title).Msg("crew agent launched")
		lives = append(lives, live{idx: i, task: t, wt: wt, name: name})
	}

	if o.Detach {
		for _, lv := range lives {
			res.Agents = append(res.Agents, AgentOutcome{
				Index: lv.idx + 1, Task: lv.task, Name: lv.name, Branch: lv.wt.Branch, Path: lv.wt.Path, State: "running",
			})
		}
		return res, nil
	}

	names := make(map[string]bool, len(lives))
	for _, lv := range lives {
		names[lv.name] = true
	}

	interrupted := false
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
monitor:
	for {
		infos, _ := d.Sessions.List(ctx)
		done := 0
		for _, in := range infos {
			if names[in.Name] && (in.State == session.StateExited || in.State == session.StateStopped) {
				done++
			}
		}
		d.Log.Info().Int("done", done).Int("total", len(lives)).Msg("crew progress")
		if done >= len(lives) {
			break
		}
		select {
		case <-ctx.Done():
			interrupted = true
			break monitor
		case <-ticker.C:
		}
	}

	if interrupted {
		d.Log.Warn().Msg("crew interrupted; stopping agents")
		for _, lv := range lives {
			_ = d.Sessions.Stop(context.Background(), lv.name, false)
		}
	}

	for _, lv := range lives {
		in, _ := d.Sessions.Get(context.Background(), lv.name)
		out := AgentOutcome{
			Index: lv.idx + 1, Task: lv.task, Name: lv.name, Branch: lv.wt.Branch,
			Path: lv.wt.Path, State: string(in.State), ExitCode: in.ExitCode,
		}
		if in.LogPath != "" {
			if b, rerr := os.ReadFile(in.LogPath); rerr == nil {
				out.Summary = extractSummary(b)
			}
		}
		out.DiffStat = diffStat(ctx, lv.wt.Path)
		res.Agents = append(res.Agents, out)
	}
	return res, nil
}

func planTasks(ctx context.Context, d Deps, o Options) ([]Task, error) {
	if o.TasksFile != "" {
		b, err := os.ReadFile(o.TasksFile)
		if err != nil {
			return nil, err
		}
		var tasks []Task
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			tasks = append(tasks, Task{Title: truncate(line, 60), Description: line})
		}
		if len(tasks) == 0 {
			return nil, domain.InvalidInputf("no tasks found in %s", o.TasksFile)
		}
		return tasks, nil
	}

	n := o.Agents
	if n < 1 {
		n = 1
	}
	prompt := fmt.Sprintf(
		"Decompose the following work into at most %d INDEPENDENT, parallelizable subtasks, each doable in a separate git worktree without merge conflicts (prefer disjoint files). If the work is small or inherently sequential, return fewer (even 1).\n\n"+
			"Respond with ONLY a JSON array; each element {\"title\": string, \"description\": string}. No prose, no markdown fences.\n\nWork:\n%s",
		n, o.Description)

	res, err := d.Agent.Headless(ctx, domain.Worktree{Path: d.RepoRoot}, agent.HeadlessSpec{
		Spec:         agent.Spec{Prompt: prompt, PermissionMode: "plan"},
		OutputFormat: "json",
	})
	if err != nil {
		return nil, fmt.Errorf("planning crew tasks: %w", err)
	}
	tasks := parseTasks(res.Text)
	if len(tasks) == 0 {
		return []Task{{Title: truncate(o.Description, 60), Description: o.Description}}, nil
	}
	if len(tasks) > n {
		tasks = tasks[:n]
	}
	return tasks, nil
}

func parseTasks(text string) []Task {
	text = strings.TrimSpace(text)
	i := strings.Index(text, "[")
	j := strings.LastIndex(text, "]")
	if i < 0 || j < 0 || j < i {
		return nil
	}
	var raw []Task
	if err := json.Unmarshal([]byte(text[i:j+1]), &raw); err != nil {
		return nil
	}
	var out []Task
	for _, t := range raw {
		if strings.TrimSpace(t.Title) == "" && strings.TrimSpace(t.Description) == "" {
			continue
		}
		if t.Title == "" {
			t.Title = truncate(t.Description, 60)
		}
		if t.Description == "" {
			t.Description = t.Title
		}
		out = append(out, t)
	}
	return out
}

func agentPrompt(t Task) string {
	return fmt.Sprintf(
		"You are one agent in a crew, working in your own isolated git worktree.\n\nTask: %s\n\n%s\n\nImplement this task here only. When done, summarize what you changed.",
		t.Title, t.Description)
}

func diffStat(ctx context.Context, dir string) string {
	if out, _ := gitutil.Exec(ctx, dir, "diff", "--stat"); strings.TrimSpace(out) != "" {
		return strings.TrimSpace(out)
	}
	out, _ := gitutil.Exec(ctx, dir, "status", "--short")
	return strings.TrimSpace(out)
}

// extractSummary pulls the agent's final summary out of a session log. Logs are
// NDJSON (stream-json) or a single json envelope; in both cases the summary is
// the "result" field of the last result-bearing line.
func extractSummary(b []byte) string {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return ""
	}
	var summary string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var e struct {
			Result string `json:"result"`
		}
		if json.Unmarshal([]byte(line), &e) == nil && e.Result != "" {
			summary = e.Result // keep the last result-bearing line
		}
	}
	if summary != "" {
		return truncate(summary, 400)
	}
	return truncate(s, 300)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
