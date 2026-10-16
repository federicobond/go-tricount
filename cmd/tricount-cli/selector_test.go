package main

import (
	"strings"
	"testing"
)

// The read commands pick the tricount the way the write commands do: the
// only one followed when the argument is left out, else an id or a title.

func TestShowDefaultsToTheOnlyTricountFollowed(t *testing.T) {
	a, _, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "show")
	if !strings.Contains(out, "Taiwan") {
		t.Errorf("output should show the only tricount: %q", out)
	}
}

func TestShowAcceptsATitle(t *testing.T) {
	a, _, stderr := newFakeApp(t)
	out := stdoutOf(t, a, stderr, "show", "taiwan")
	if !strings.Contains(out, "Taiwan") {
		t.Errorf("output should show the tricount named: %q", out)
	}
}

func TestBalancesRefusesToGuessBetweenSeveralTricounts(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	f.extra = secondRegistry
	if code := a.run(t.Context(), []string{"balances"}); code == 0 {
		t.Fatal("exit code = 0; with two tricounts followed the command must ask which")
	}
	msg := stderr.String()
	for _, want := range []string{"Taiwan", "Trip", "102257091", "777000001"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q: %q", want, msg)
		}
	}
}

func TestLinkWithOnlyAMemberUsesTheOnlyTricount(t *testing.T) {
	a, f, stderr := newFakeApp(t)
	stdoutOf(t, a, stderr, "link", "Bob")
	bob, _ := f.memberNamed("Bob")
	if f.linked != bob.UUID {
		t.Errorf("linked = %q, want Bob's %q", f.linked, bob.UUID)
	}
}

func TestLeaveDefaultsToTheOnlyTricount(t *testing.T) {
	a, _, stderr, _, bodies := routedApp(t)
	if code := a.run(t.Context(), []string{"leave"}); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	var sawDeleted bool
	for _, b := range *bodies {
		if strings.Contains(b, "all_registry_deleted") && strings.Contains(b, "tABC123xyz") {
			sawDeleted = true
		}
	}
	if !sawDeleted {
		t.Errorf("leave must send the token as deleted: %v", *bodies)
	}
}
