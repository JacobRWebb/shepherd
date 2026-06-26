package cli

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/sysproc"
)

// toolReport captures the result of probing one external CLI on PATH.
type toolReport struct {
	Name    string `json:"name"`
	Found   bool   `json:"found"`
	Missing bool   `json:"missing"`
	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// configReport summarizes config loading + validation.
type configReport struct {
	OK       bool     `json:"ok"`
	Source   string   `json:"source,omitempty"`
	Problems []string `json:"problems,omitempty"`
}

// ghAuthReport summarizes `gh auth status`.
type ghAuthReport struct {
	Available     bool   `json:"available"`
	Authenticated bool   `json:"authenticated"`
	Detail        string `json:"detail,omitempty"`
}

// doctorRow is one line of the human-readable diagnostics table.
type doctorRow struct {
	Check  string `json:"check"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// doctorReport is the machine-readable diagnostics object emitted via --json.
type doctorReport struct {
	Tools          []toolReport      `json:"tools"`
	Runtime        map[string]string `json:"runtime"`
	Config         configReport      `json:"config"`
	SessionBackend string            `json:"session_backend"`
	GHAuth         ghAuthReport      `json:"gh_auth"`
	Rows           []doctorRow       `json:"rows"`
}

// toolSpec names an external CLI and the args that print its version.
type toolSpec struct {
	name        string
	versionArgs []string
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the Shepherd environment (tools, config, backends)",
		Long:  "Probes external CLIs, the Go runtime, configuration, the selected session backend, and GitHub auth. Always exits 0.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			st := stateFrom(cmd)
			ctx := cmd.Context()
			rep := buildDoctorReport(ctx)

			headers := []string{"CHECK", "STATUS", "DETAIL"}
			var table [][]string
			for _, r := range rep.Rows {
				table = append(table, []string{r.Check, r.Status, r.Detail})
			}
			st.Out.Table(headers, table, rep)
			return nil
		},
	}
}

// buildDoctorReport gathers all diagnostics. It never returns an error: probe
// failures are recorded as data so the table/JSON stay complete.
func buildDoctorReport(ctx context.Context) doctorReport {
	specs := []toolSpec{
		{name: "git", versionArgs: []string{"--version"}},
		{name: "gh", versionArgs: []string{"--version"}},
		{name: "claude", versionArgs: []string{"--version"}},
		{name: "tmux", versionArgs: []string{"-V"}},
	}

	rep := doctorReport{
		Runtime: map[string]string{
			"go":   runtime.Version(),
			"os":   runtime.GOOS,
			"arch": runtime.GOARCH,
		},
	}

	tmuxFound := false
	for _, spec := range specs {
		tr := probeTool(ctx, spec)
		rep.Tools = append(rep.Tools, tr)
		if spec.name == "tmux" {
			tmuxFound = tr.Found
		}
		rep.Rows = append(rep.Rows, toolRow(tr))
	}

	// Runtime row.
	rep.Rows = append(rep.Rows, doctorRow{
		Check:  "runtime",
		Status: "ok",
		Detail: fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH),
	})

	// Config.
	rep.Config = checkConfig()
	if rep.Config.OK {
		detail := "configuration valid"
		if rep.Config.Source != "" {
			detail = "valid (" + rep.Config.Source + ")"
		}
		rep.Rows = append(rep.Rows, doctorRow{Check: "config", Status: "ok", Detail: detail})
	} else {
		rep.Rows = append(rep.Rows, doctorRow{
			Check:  "config",
			Status: "problem",
			Detail: strings.Join(rep.Config.Problems, "; "),
		})
	}

	// Session backend selection (tmux when on PATH and non-windows, else native).
	rep.SessionBackend = selectedSessionBackend(tmuxFound)
	rep.Rows = append(rep.Rows, doctorRow{
		Check:  "session backend",
		Status: "ok",
		Detail: rep.SessionBackend,
	})

	// gh auth.
	rep.GHAuth = checkGHAuth(ctx)
	rep.Rows = append(rep.Rows, ghAuthRow(rep.GHAuth))

	return rep
}

// probeTool looks up bin on PATH, then runs its version command.
func probeTool(ctx context.Context, spec toolSpec) toolReport {
	tr := toolReport{Name: spec.name}
	path, err := exec.LookPath(spec.name)
	if err != nil {
		tr.Missing = true
		return tr
	}
	tr.Found = true
	tr.Path = path

	vcmd := exec.CommandContext(ctx, path, spec.versionArgs...)
	sysproc.Hide(vcmd)
	out, verr := vcmd.CombinedOutput()
	if verr != nil {
		tr.Error = verr.Error()
		if v := firstLine(string(out)); v != "" {
			tr.Version = v
		}
		return tr
	}
	tr.Version = firstLine(string(out))
	return tr
}

func toolRow(tr toolReport) doctorRow {
	if tr.Missing {
		return doctorRow{Check: tr.Name, Status: "missing", Detail: "not found on PATH"}
	}
	if tr.Error != "" {
		detail := tr.Error
		if tr.Version != "" {
			detail = tr.Version + " (" + tr.Error + ")"
		}
		return doctorRow{Check: tr.Name, Status: "error", Detail: detail}
	}
	return doctorRow{Check: tr.Name, Status: "ok", Detail: tr.Version}
}

// checkConfig loads config from the default discovery path and validates it.
func checkConfig() configReport {
	cfg, err := config.Load("")
	if err != nil {
		return configReport{OK: false, Problems: []string{err.Error()}}
	}
	if verr := cfg.Validate(); verr != nil {
		return configReport{OK: false, Source: cfg.SourcePath, Problems: []string{verr.Error()}}
	}
	return configReport{OK: true, Source: cfg.SourcePath}
}

// selectedSessionBackend mirrors the auto-selection rule: tmux when present on
// PATH and not on Windows, otherwise native.
func selectedSessionBackend(tmuxFound bool) string {
	if tmuxFound && runtime.GOOS != "windows" {
		return "tmux"
	}
	return "native"
}

// checkGHAuth runs `gh auth status` (when gh is available) to report auth state.
func checkGHAuth(ctx context.Context) ghAuthReport {
	path, err := exec.LookPath("gh")
	if err != nil {
		return ghAuthReport{Available: false, Detail: "gh not found on PATH"}
	}
	acmd := exec.CommandContext(ctx, path, "auth", "status")
	sysproc.Hide(acmd)
	out, runErr := acmd.CombinedOutput()
	detail := firstLine(string(out))
	if runErr != nil {
		if detail == "" {
			detail = runErr.Error()
		}
		return ghAuthReport{Available: true, Authenticated: false, Detail: detail}
	}
	return ghAuthReport{Available: true, Authenticated: true, Detail: detail}
}

func ghAuthRow(r ghAuthReport) doctorRow {
	if !r.Available {
		return doctorRow{Check: "gh auth", Status: "missing", Detail: r.Detail}
	}
	if !r.Authenticated {
		return doctorRow{Check: "gh auth", Status: "not authenticated", Detail: r.Detail}
	}
	detail := r.Detail
	if detail == "" {
		detail = "authenticated"
	}
	return doctorRow{Check: "gh auth", Status: "ok", Detail: detail}
}

// firstLine returns the first non-empty trimmed line of s.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
