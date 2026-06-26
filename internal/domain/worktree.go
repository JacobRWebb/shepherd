package domain

// Worktree is a git worktree known to the repository. The main working tree is
// included in listings and flagged with IsMain; Shepherd refuses to perform
// mutating operations against it.
type Worktree struct {
	Name     string `json:"name"`     // logical name (directory basename)
	Path     string `json:"path"`     // absolute path on disk
	Branch   string `json:"branch"`   // branch name (empty if detached)
	Head     string `json:"head"`     // checked-out commit SHA
	IsMain   bool   `json:"is_main"`  // true for the primary working tree
	Detached bool   `json:"detached"` // HEAD is detached (no branch)
	Locked   bool   `json:"locked"`
	Prunable bool   `json:"prunable"`
}
