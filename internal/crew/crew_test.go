package crew

import "testing"

func TestParseTasks(t *testing.T) {
	good := `[{"title":"A","description":"do a"},{"title":"B","description":"do b"}]`
	if ts := parseTasks(good); len(ts) != 2 || ts[0].Title != "A" {
		t.Errorf("good = %+v", ts)
	}

	fenced := "```json\n[{\"title\":\"X\",\"description\":\"y\"}]\n```"
	if ts := parseTasks(fenced); len(ts) != 1 || ts[0].Title != "X" {
		t.Errorf("fenced = %+v", ts)
	}

	if ts := parseTasks("no json at all"); ts != nil {
		t.Errorf("garbage should yield nil, got %+v", ts)
	}

	// title-less item gets a title derived from the description
	if ts := parseTasks(`[{"description":"only desc"}]`); len(ts) != 1 || ts[0].Title == "" {
		t.Errorf("title fallback = %+v", ts)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Errorf("short string should be unchanged")
	}
	if got := truncate("hello world", 5); got != "hello…" {
		t.Errorf("truncate = %q", got)
	}
}
