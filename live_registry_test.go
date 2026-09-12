//go:build live

package tricount

import (
	"context"
	"testing"
)

func TestLiveFixtureProvisioned(t *testing.T) {
	tri := liveTricount(t)

	if tri.ID == 0 {
		t.Fatal("fixture has no id")
	}
	if tri.PublicToken == "" {
		t.Fatal("fixture has no public token")
	}
	if tri.Currency != liveFixtureCurrency {
		t.Errorf("fixture currency = %q, want %q", tri.Currency, liveFixtureCurrency)
	}
	t.Logf("fixture %q id=%d https://tricount.com/%s members=%d transactions=%d",
		tri.Title, tri.ID, tri.PublicToken, len(tri.Members), len(tri.Transactions))
}

func TestLiveListIncludesFixture(t *testing.T) {
	c := liveClient(t)
	fixture := liveTricount(t)

	list, err := c.ListTricounts(context.Background())
	if err != nil {
		t.Fatalf("ListTricounts: %v", err)
	}
	for _, tri := range list {
		if tri.ID == fixture.ID {
			return
		}
	}
	t.Errorf("fixture %d is not in the %d tricounts this device follows", fixture.ID, len(list))
}

func TestLiveGetByTokenAgreesWithGetByID(t *testing.T) {
	c := liveClient(t)
	fixture := liveTricount(t)

	byToken, err := c.GetTricount(context.Background(), fixture.PublicToken)
	if err != nil {
		t.Fatalf("GetTricount by token: %v", err)
	}
	if byToken.ID != fixture.ID {
		t.Errorf("by token id = %d, by id = %d", byToken.ID, fixture.ID)
	}
	if byToken.Currency != fixture.Currency {
		t.Errorf("currency disagrees: %q vs %q", byToken.Currency, fixture.Currency)
	}
	if len(byToken.Members) != len(fixture.Members) {
		t.Errorf("member counts disagree: %d vs %d", len(byToken.Members), len(fixture.Members))
	}
}

// TestLiveScratchTricountLifecycle is one of only two places the suite creates
// and destroys a tricount. It uses its own scratch tricount and deletes it in
// the same test, so nothing accumulates and the shared fixture is never at
// risk.
func TestLiveScratchTricountLifecycle(t *testing.T) {
	c := liveClient(t)
	ctx := context.Background()

	id, err := c.CreateTricount(ctx, tagged("scratch"), "EUR", "created and deleted by one test")
	if err != nil {
		t.Fatalf("CreateTricount: %v", err)
	}
	scratch, err := c.GetTricountByID(ctx, id)
	if err != nil {
		t.Fatalf("GetTricountByID(%d): %v", id, err)
	}
	t.Cleanup(func() {
		if err := c.DeleteTricount(context.Background(), scratch); err != nil {
			t.Errorf("deleting scratch tricount %d: %v — delete it by hand", scratch.ID, err)
		}
	})

	if scratch.Title != tagged("scratch") {
		t.Errorf("Title = %q", scratch.Title)
	}

	title := tagged("scratch renamed")
	emoji := "🧪"
	if err := c.UpdateTricount(ctx, scratch, TricountUpdate{Title: &title, Emoji: &emoji}); err != nil {
		t.Fatalf("UpdateTricount: %v", err)
	}
	reread, err := c.GetTricountByID(ctx, scratch.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	if reread.Title != title {
		t.Errorf("title did not persist: %q", reread.Title)
	}
	if reread.Emoji != emoji {
		t.Errorf("emoji did not persist: %q", reread.Emoji)
	}

	if err := c.ArchiveTricount(ctx, scratch); err != nil {
		t.Fatalf("ArchiveTricount: %v", err)
	}
	if reread, err = c.GetTricountByID(ctx, scratch.ID); err != nil {
		t.Fatalf("re-reading after archive: %v", err)
	}
	if !reread.IsArchived() {
		t.Error("archiving did not persist")
	}
	if err := c.UnarchiveTricount(ctx, scratch); err != nil {
		t.Fatalf("UnarchiveTricount: %v", err)
	}
}
