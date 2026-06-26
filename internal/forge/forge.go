// Package forge abstracts over git hosting providers (GitHub via the gh CLI,
// Bitbucket via REST). The Forge interface is consumed by the ship/babysit/
// status services; Select instantiates the configured implementation.
//
// Provider impls live in subpackages and depend only on internal/domain, so
// they never import this package (avoiding an import cycle). Select returns them
// as Forge values, which is where the compiler checks they satisfy the interface.
package forge

import (
	"context"
	"fmt"

	"github.com/JacobRWebb/shepherd/internal/config"
	"github.com/JacobRWebb/shepherd/internal/domain"
	"github.com/JacobRWebb/shepherd/internal/forge/bitbucket"
	"github.com/JacobRWebb/shepherd/internal/forge/github"
)

// Forge is the provider-neutral interface Shepherd uses for pull-request work.
type Forge interface {
	Name() string

	OpenPR(ctx context.Context, r domain.Repo, o domain.OpenPROpts) (domain.PullRequest, error)
	GetPR(ctx context.Context, r domain.Repo, number int) (domain.PullRequest, error)
	// PRForBranch returns the PR whose head is branch, or (nil, nil) if none.
	PRForBranch(ctx context.Context, r domain.Repo, branch string) (*domain.PullRequest, error)

	ListChecks(ctx context.Context, r domain.Repo, number int) ([]domain.Check, error)
	// WatchChecks polls until no checks are pending (or ctx is done), invoking
	// onTick (nil-safe) with each intermediate summary.
	WatchChecks(ctx context.Context, r domain.Repo, number int, onTick func(domain.CheckSummary)) (domain.CheckSummary, error)

	ListComments(ctx context.Context, r domain.Repo, number int) ([]domain.Comment, error)
	PostComment(ctx context.Context, r domain.Repo, number int, body string) (domain.Comment, error)

	GetIssue(ctx context.Context, r domain.Repo, id string) (domain.Issue, error)
}

// Select instantiates the configured forge implementation.
func Select(cfg config.ForgeConfig) (Forge, error) {
	switch cfg.Provider {
	case "github", "":
		return github.New(cfg.GitHub)
	case "bitbucket":
		return bitbucket.New(cfg.Bitbucket)
	default:
		return nil, fmt.Errorf("forge: unknown provider %q (want github|bitbucket)", cfg.Provider)
	}
}
