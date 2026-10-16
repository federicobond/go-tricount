package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// twoTricountApp follows Taiwan and Trip, so nothing can be defaulted.
func twoTricountApp(t *testing.T) (*app, *fakeAPI, *strings.Builder) {
	t.Helper()
	a, f, stderr := newFakeApp(t)
	f.extra = secondRegistry
	return a, f, stderr
}

func TestUseMakesLaterCommandsDefaultToIt(t *testing.T) {
	a, _, stderr := twoTricountApp(t)
	if code := a.run(t.Context(), []string{"show"}); code == 0 {
		t.Fatal("exit code = 0; before `use`, two tricounts followed must mean asking")
	}
	stdoutOf(t, a, stderr, "use", "taiwan")
	out := stdoutOf(t, a, stderr, "show")
	if !strings.Contains(out, "Taiwan") {
		t.Errorf("show should default to the used tricount: %q", out)
	}
}

func TestUseSurvivesANewProcess(t *testing.T) {
	a, _, stderr := twoTricountApp(t)
	stdoutOf(t, a, stderr, "use", "102257091")

	// A second app on the same credentials is the next invocation.
	b := &app{stdout: &strings.Builder{}, stderr: stderr, credPath: a.credPath, opts: a.opts}
	out := stdoutOf(t, b, stderr, "show")
	if !strings.Contains(out, "Taiwan") {
		t.Errorf("the setting should persist on disk: %q", out)
	}
}

func TestUseAloneReportsTheCurrentTricount(t *testing.T) {
	a, _, stderr := twoTricountApp(t)
	if out := stdoutOf(t, a, stderr, "use"); !strings.Contains(out, "no current tricount") {
		t.Errorf("before any use: %q", out)
	}
	stdoutOf(t, a, stderr, "use", "taiwan")
	out := stdoutOf(t, a, stderr, "use")
	if !strings.Contains(out, "102257091") || !strings.Contains(out, "Taiwan") {
		t.Errorf("use alone should name the current tricount: %q", out)
	}
}

func TestUseUnsetGoesBackToAsking(t *testing.T) {
	a, _, stderr := twoTricountApp(t)
	stdoutOf(t, a, stderr, "use", "taiwan")
	stdoutOf(t, a, stderr, "use", "--unset")
	if code := a.run(t.Context(), []string{"show"}); code == 0 {
		t.Fatal("exit code = 0; after --unset, two tricounts followed must mean asking again")
	}
}

func TestUseRefusesAnUnknownTricount(t *testing.T) {
	a, _, stderr := twoTricountApp(t)
	if code := a.run(t.Context(), []string{"use", "Nonesuch"}); code == 0 {
		t.Fatal("exit code = 0, want non-zero for a tricount that is not followed")
	}
	if !strings.Contains(stderr.String(), "Nonesuch") {
		t.Errorf("stderr should name the bad argument: %q", stderr.String())
	}
	if out := stdoutOf(t, a, stderr, "use"); !strings.Contains(out, "no current tricount") {
		t.Errorf("a failed use must not set anything: %q", out)
	}
}

func TestUseAppliesToWritesToo(t *testing.T) {
	a, f, stderr := twoTricountApp(t)
	stdoutOf(t, a, stderr, "use", "taiwan")
	stdoutOf(t, a, stderr, "add", "1000", "Taxi")
	if len(f.entries) != 1 {
		t.Errorf("entries posted = %d, want 1", len(f.entries))
	}
}

func TestUseJSON(t *testing.T) {
	a, _, stderr := twoTricountApp(t)
	out := stdoutOf(t, a, stderr, "use", "--json", "taiwan")
	var got struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if got.ID != fakeTricountID || got.Title != "Taiwan" {
		t.Errorf("got %+v", got)
	}
}
