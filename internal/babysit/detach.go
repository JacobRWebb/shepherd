package babysit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/JacobRWebb/shepherd/internal/session"
)

// StartDetached launches `shepherd babysit <pr>` as a persistent background
// session that watches the PR until it is merged/closed or explicitly stopped
// (via `shepherd stop`). It is how `deliver` keeps the loop pending without
// holding a terminal. self is the path to the shepherd executable; dir is the
// repo root (so the spawned process discovers config and worktrees).
func StartDetached(ctx context.Context, sessions session.SessionBackend, self, dir string, pr int, interval time.Duration) (session.Info, error) {
	if sessions == nil {
		return session.Info{}, fmt.Errorf("no session backend available to babysit in the background")
	}
	if self == "" {
		return session.Info{}, fmt.Errorf("could not resolve the shepherd executable path")
	}
	args := []string{"babysit", strconv.Itoa(pr), "--max-iterations", "0"}
	if interval > 0 {
		args = append(args, "--interval", interval.String())
	}
	return sessions.Start(ctx, session.Spec{
		Name:    fmt.Sprintf("babysit-pr-%d", pr),
		Dir:     dir,
		Program: self,
		Args:    args,
		Labels:  map[string]string{"kind": "babysit", "pr": strconv.Itoa(pr)},
	})
}
