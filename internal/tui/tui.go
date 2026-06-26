// Package tui is the interactive Bubble Tea dashboard: a polled list of
// worktrees/agents and a live log viewer that tails a session's output.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/session"
	"github.com/JacobRWebb/shepherd/internal/updater"
	"github.com/JacobRWebb/shepherd/internal/worktree"
)

// Deps are the collaborators the TUI reads from.
type Deps struct {
	Worktrees worktree.Manager
	Sessions  session.SessionBackend
	Log       *zerolog.Logger
	Version   string
	Repo      string
}

// Run launches the dashboard, blocking until the user quits.
func Run(ctx context.Context, d Deps) error {
	p := tea.NewProgram(newModel(d), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	if errors.Is(err, tea.ErrProgramKilled) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

type screen int

const (
	screenDashboard screen = iota
	screenLogs
)

type wtItem struct {
	wt   domain.Worktree
	sess *session.Info
}

func (i wtItem) FilterValue() string { return i.wt.Name }
func (i wtItem) Title() string       { return i.wt.Name }
func (i wtItem) Description() string {
	st := "—"
	if i.sess != nil {
		st = string(i.sess.State)
	}
	return i.wt.Branch + "  ·  " + st
}

type model struct {
	d            Deps
	screen       screen
	list         list.Model
	vp           viewport.Model
	ready        bool
	err          error
	updateBanner string

	tailRC  io.ReadCloser
	logName string
	logBuf  strings.Builder
	follow  bool
}

type tickMsg time.Time
type refreshedMsg struct {
	items []list.Item
	err   error
}
type logChunkMsg struct{ name, data string }
type logEOFMsg struct{ name string }
type updateAvailableMsg struct{ current, latest string }

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m model) checkUpdateCmd() tea.Cmd {
	d := m.d
	return func() tea.Msg {
		info := updater.CachedCheck(context.Background(), d.Repo, d.Version)
		if info.Available {
			return updateAvailableMsg{current: info.Current, latest: info.Latest}
		}
		return updateAvailableMsg{}
	}
}

func newModel(d Deps) model {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Shepherd — worktrees & agents"
	return model{d: d, list: l, follow: true}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.refreshCmd(), tickCmd(), m.checkUpdateCmd())
}

func (m model) refreshCmd() tea.Cmd {
	d := m.d
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		wts, err := d.Worktrees.List(ctx)
		if err != nil {
			return refreshedMsg{err: err}
		}
		sessions, _ := d.Sessions.List(ctx)
		byDir := make(map[string]session.Info, len(sessions))
		for _, s := range sessions {
			byDir[filepath.Clean(s.Dir)] = s
		}
		items := []list.Item{}
		for _, w := range wts {
			if w.IsMain {
				continue
			}
			it := wtItem{wt: w}
			if s, ok := byDir[filepath.Clean(w.Path)]; ok {
				si := s
				it.sess = &si
			}
			items = append(items, it)
		}
		return refreshedMsg{items: items}
	}
}

func (m *model) startTail(name string) tea.Cmd {
	if m.tailRC != nil {
		_ = m.tailRC.Close()
		m.tailRC = nil
	}
	rc, err := m.d.Sessions.Tail(context.Background(), name, true)
	if err != nil {
		return func() tea.Msg { return logEOFMsg{name: name} }
	}
	m.tailRC = rc
	m.logName = name
	m.logBuf.Reset()
	return m.readNextCmd(name)
}

func (m model) readNextCmd(name string) tea.Cmd {
	rc := m.tailRC
	return func() tea.Msg {
		if rc == nil {
			return logEOFMsg{name: name}
		}
		buf := make([]byte, 8192)
		n, err := rc.Read(buf)
		if n > 0 {
			return logChunkMsg{name: name, data: string(buf[:n])}
		}
		if err != nil {
			return logEOFMsg{name: name}
		}
		return logChunkMsg{name: name, data: ""}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.list.SetSize(msg.Width, msg.Height-1)
		m.vp = viewport.New(msg.Width, msg.Height-3)
		m.ready = true
		if m.screen == screenLogs {
			m.vp.SetContent(m.logBuf.String())
		}
		return m, nil

	case tickMsg:
		if m.screen == screenDashboard {
			return m, tea.Batch(m.refreshCmd(), tickCmd())
		}
		return m, tickCmd()

	case refreshedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		return m, m.list.SetItems(msg.items)

	case logChunkMsg:
		if msg.name != m.logName {
			return m, nil
		}
		if msg.data != "" {
			m.logBuf.WriteString(msg.data)
			m.vp.SetContent(m.logBuf.String())
			if m.follow {
				m.vp.GotoBottom()
			}
		}
		return m, m.readNextCmd(msg.name)

	case logEOFMsg:
		if msg.name == m.logName {
			m.logBuf.WriteString("\n— session ended —\n")
			m.vp.SetContent(m.logBuf.String())
			if m.follow {
				m.vp.GotoBottom()
			}
		}
		return m, nil

	case updateAvailableMsg:
		if msg.latest != "" {
			m.updateBanner = fmt.Sprintf("update available: %s → %s — run `shepherd update`", msg.current, msg.latest)
		}
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case screenDashboard:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "enter":
				if it, ok := m.list.SelectedItem().(wtItem); ok && it.sess != nil {
					m.screen = screenLogs
					cmd := m.startTail(it.sess.Name)
					return m, cmd
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd

		case screenLogs:
			switch msg.String() {
			case "q", "ctrl+c":
				return m, tea.Quit
			case "esc":
				if m.tailRC != nil {
					_ = m.tailRC.Close()
					m.tailRC = nil
				}
				m.logName = ""
				m.screen = screenDashboard
				return m, nil
			case "f":
				m.follow = !m.follow
				return m, nil
			}
			var cmd tea.Cmd
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
	}

	// non-key messages route to the active screen
	switch m.screen {
	case screenLogs:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	default:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	faintStyle  = lipgloss.NewStyle().Faint(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	updateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
)

func (m model) View() string {
	if !m.ready {
		return "loading…"
	}
	if m.screen == screenLogs {
		foll := "follow:on"
		if !m.follow {
			foll = "follow:off"
		}
		return headerStyle.Render("logs: "+m.logName) + "\n" +
			m.vp.View() + "\n" +
			faintStyle.Render(foll+"  ·  f toggle  ·  esc back  ·  q quit")
	}
	body := m.list.View()
	if m.updateBanner != "" {
		body = updateStyle.Render("  "+m.updateBanner) + "\n" + body
	}
	if m.err != nil {
		body = errStyle.Render("error: "+m.err.Error()) + "\n" + body
	}
	return body
}
