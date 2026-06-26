package babysit

import (
	"context"
	"strings"
	"testing"

	"github.com/JacobRWebb/shepherd/internal/domain"
)

// fakeForge implements forge.Forge with only ListComments meaningfully wired.
type fakeForge struct{ comments []domain.Comment }

func (f *fakeForge) Name() string { return "fake" }
func (f *fakeForge) OpenPR(context.Context, domain.Repo, domain.OpenPROpts) (domain.PullRequest, error) {
	return domain.PullRequest{}, nil
}
func (f *fakeForge) GetPR(context.Context, domain.Repo, int) (domain.PullRequest, error) {
	return domain.PullRequest{}, nil
}
func (f *fakeForge) PRForBranch(context.Context, domain.Repo, string) (*domain.PullRequest, error) {
	return nil, nil
}
func (f *fakeForge) ListChecks(context.Context, domain.Repo, int) ([]domain.Check, error) {
	return nil, nil
}
func (f *fakeForge) WatchChecks(context.Context, domain.Repo, int, func(domain.CheckSummary)) (domain.CheckSummary, error) {
	return domain.CheckSummary{}, nil
}
func (f *fakeForge) ListComments(context.Context, domain.Repo, int) ([]domain.Comment, error) {
	return f.comments, nil
}
func (f *fakeForge) PostComment(context.Context, domain.Repo, int, string) (domain.Comment, error) {
	return domain.Comment{}, nil
}
func (f *fakeForge) GetIssue(context.Context, domain.Repo, string) (domain.Issue, error) {
	return domain.Issue{}, nil
}

func TestFreshFeedbackSkipsOwnAndDedupes(t *testing.T) {
	fake := &fakeForge{comments: []domain.Comment{
		{ID: "1", Author: "alice", Body: "please rename Foo to Bar"},
		{ID: "2", Author: "alice", Body: "🐑 shepherd addressed the review feedback"}, // shepherd's own
		{ID: "3", Author: "bob", Body: "add a test for the edge case"},
		{ID: "4", Author: "alice", Body: "   "}, // empty
	}}
	d := Deps{Forge: fake, Repo: domain.Repo{Owner: "o", Name: "r"}}
	processed := map[string]bool{}

	fresh := d.freshFeedback(context.Background(), domain.PullRequest{Number: 7}, processed)
	if len(fresh) != 2 {
		t.Fatalf("want 2 fresh comments (skip own + empty), got %d: %+v", len(fresh), fresh)
	}
	if fresh[0].ID != "1" || fresh[1].ID != "3" {
		t.Fatalf("unexpected fresh comments: %+v", fresh)
	}
	// Every inspected comment is marked processed, so a second pass yields none.
	if again := d.freshFeedback(context.Background(), domain.PullRequest{Number: 7}, processed); len(again) != 0 {
		t.Fatalf("second pass should be empty, got %d", len(again))
	}
}

func TestFreshFeedbackSkipsMarkedReply(t *testing.T) {
	// Simulate shepherd having posted a reply and marked it processed by URL key;
	// freshFeedback must not surface it as feedback even on a fresh fetch.
	reply := domain.Comment{URL: "https://x/pull/3#issuecomment-9", Author: "JacobRWebb", Body: "🐑 shepherd addressed the feedback"}
	fake := &fakeForge{comments: []domain.Comment{
		{URL: "https://x/pull/3#issuecomment-1", Author: "bob", Body: "do X"},
		reply,
	}}
	d := Deps{Forge: fake, Repo: domain.Repo{Owner: "o", Name: "r"}}
	processed := map[string]bool{commentKey(reply): true}

	fresh := d.freshFeedback(context.Background(), domain.PullRequest{Number: 3}, processed)
	if len(fresh) != 1 || fresh[0].Body != "do X" {
		t.Fatalf("expected only bob's comment, got %+v", fresh)
	}
}

func TestIsShepherdComment(t *testing.T) {
	if !isShepherdComment(domain.Comment{Body: "🐑 shepherd did a thing"}) {
		t.Fatal("expected shepherd comment to be detected")
	}
	if isShepherdComment(domain.Comment{Body: "looks good to me"}) {
		t.Fatal("human comment misdetected as shepherd's")
	}
}

func TestFeedbackPromptIncludesComments(t *testing.T) {
	pr := domain.PullRequest{Number: 12, Title: "Add caching"}
	p := feedbackPrompt(pr, []domain.Comment{
		{Body: "this allocates on every call", Path: "cache.go", Line: 42},
		{Body: "missing a test"},
	})
	for _, want := range []string{"#12", "Add caching", "cache.go:42", "allocates on every call", "missing a test"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q\n---\n%s", want, p)
		}
	}
}
