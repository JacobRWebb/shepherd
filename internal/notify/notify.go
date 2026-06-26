// Package notify delivers events to the terminal, a webhook, and/or an arbitrary
// command, selected by configuration. It is intentionally simple.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/sysproc"
)

// Event is one notification.
type Event struct {
	Level   string         `json:"level"` // info | warn | error | success
	Title   string         `json:"title"`
	Message string         `json:"message,omitempty"`
	PRURL   string         `json:"pr_url,omitempty"`
	Checks  []domain.Check `json:"checks,omitempty"`
}

// Notifier delivers events.
type Notifier interface {
	Notify(ctx context.Context, e Event) error
}

// New builds a Notifier from config. The terminal channel is always available;
// when notifications are disabled, only the terminal is used.
func New(cfg config.NotificationsConfig, log *zerolog.Logger) Notifier {
	if log == nil {
		l := zerolog.Nop()
		log = &l
	}
	if !cfg.Enabled {
		return &terminal{log: log}
	}
	var ns []Notifier
	for _, ch := range cfg.Channels {
		switch strings.TrimSpace(ch) {
		case "terminal":
			ns = append(ns, &terminal{log: log})
		case "webhook":
			if url := os.Getenv(cfg.Webhook.URLEnv); url != "" {
				ns = append(ns, &webhook{url: url, format: cfg.Webhook.Format})
			}
		case "command":
			if cfg.Command != "" {
				ns = append(ns, &command{cmdline: cfg.Command})
			}
		}
	}
	if len(ns) == 0 {
		ns = append(ns, &terminal{log: log})
	}
	return Multi(ns...)
}

type terminal struct{ log *zerolog.Logger }

func (t *terminal) Notify(_ context.Context, e Event) error {
	ev := t.log.Info()
	switch e.Level {
	case "error":
		ev = t.log.Error()
	case "warn":
		ev = t.log.Warn()
	}
	if e.PRURL != "" {
		ev = ev.Str("pr", e.PRURL)
	}
	if e.Title != "" {
		ev = ev.Str("title", e.Title)
	}
	ev.Msg(e.Message)
	return nil
}

type webhook struct {
	url    string
	format string
}

func (w *webhook) Notify(ctx context.Context, e Event) error {
	var payload []byte
	if w.format == "slack" {
		text := e.Title
		if e.Message != "" {
			text += "\n" + e.Message
		}
		if e.PRURL != "" {
			text += "\n" + e.PRURL
		}
		payload, _ = json.Marshal(map[string]string{"text": text})
	} else {
		payload, _ = json.Marshal(e)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}

type command struct{ cmdline string }

func (c *command) Notify(ctx context.Context, e Event) error {
	b, _ := json.Marshal(e)
	name, args := shellCommand(c.cmdline)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(b)
	sysproc.Hide(cmd)
	return cmd.Run()
}

func shellCommand(line string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/c", line}
	}
	return "sh", []string{"-c", line}
}

type multi []Notifier

// Multi fans out to several notifiers, returning the first error (best-effort).
func Multi(ns ...Notifier) Notifier { return multi(ns) }

func (m multi) Notify(ctx context.Context, e Event) error {
	var firstErr error
	for _, n := range m {
		if err := n.Notify(ctx, e); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
