//go:build live

package tricount

import (
	"bytes"
	"context"
	"testing"
)

// tinyPNG is a valid 1x1 PNG, small enough to embed.
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

func TestLiveTransactionAttachment(t *testing.T) {
	c := liveClient(t)
	cleanupThisRun(t)
	tri := liveTricount(t)
	ctx := context.Background()
	alice, bob := liveTwoMembers(t, c, tri)

	attachmentID, err := c.UploadTransactionAttachment(ctx, tri, bytes.NewReader(tinyPNG), "image/png")
	if err != nil {
		t.Fatalf("UploadTransactionAttachment: %v", err)
	}
	if attachmentID == 0 {
		t.Fatal("upload returned attachment id 0")
	}
	t.Logf("uploaded attachment %d", attachmentID)

	txID, err := c.CreateExpense(ctx, tri, Expense{
		Description:   tagged("Receipt"),
		Amount:        MustParseAmount("12.00", "EUR"),
		Payer:         alice,
		Split:         SplitEqually(alice, bob),
		AttachmentIDs: []int64{attachmentID},
	})
	if err != nil {
		t.Fatalf("CreateExpense with an attachment: %v", err)
	}

	reread, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading: %v", err)
	}
	tx := reread.TransactionByID(txID)
	if tx == nil {
		t.Fatalf("transaction %d missing", txID)
	}
	var attached bool
	for _, id := range tx.AttachmentIDs {
		if id == attachmentID {
			attached = true
		}
	}
	if !attached {
		t.Errorf("attachment %d is not on the transaction; ids are %v", attachmentID, tx.AttachmentIDs)
	}

	// Detach and confirm.
	if err := c.RemoveTransactionAttachment(ctx, reread, txID, attachmentID); err != nil {
		t.Fatalf("RemoveTransactionAttachment: %v", err)
	}
	after, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading after detach: %v", err)
	}
	if tx = after.TransactionByID(txID); tx == nil {
		t.Fatalf("transaction %d vanished", txID)
	}
	for _, id := range tx.AttachmentIDs {
		if id == attachmentID {
			t.Errorf("attachment %d is still attached", attachmentID)
		}
	}

	// Re-attach, exercising the add path too.
	if err := c.AddTransactionAttachment(ctx, after, txID, attachmentID); err != nil {
		t.Fatalf("AddTransactionAttachment: %v", err)
	}
	final, err := c.GetTricountByID(ctx, tri.ID)
	if err != nil {
		t.Fatalf("re-reading after re-attach: %v", err)
	}
	tx = final.TransactionByID(txID)
	if tx == nil || len(tx.AttachmentIDs) != 1 || tx.AttachmentIDs[0] != attachmentID {
		t.Errorf("re-attach did not take; ids are %v", tx.AttachmentIDs)
	}
}

// TestLiveGalleryAttachment probes the gallery endpoints rather than asserting
// they work. The upload returns a UUID and the delete returns 200, but nothing
// has ever been observed to appear in the listing, so a failure here is a
// finding to record, not a regression. Transaction attachments are the
// supported path and TestLiveTransactionAttachment does assert those.
func TestLiveGalleryAttachment(t *testing.T) {
	c := liveClient(t)
	tri := liveTricount(t)
	ctx := context.Background()

	uuid, err := c.UploadGalleryAttachment(ctx, tri, bytes.NewReader(tinyPNG), "image/png")
	if err != nil {
		t.Logf("PROBE RESULT: UploadGalleryAttachment failed outright: %v", err)
		return
	}
	t.Logf("PROBE RESULT: upload accepted, returned uuid %s", uuid)

	// Gallery images carry no description, so they cannot be run-tagged;
	// delete this one here rather than in the shared cleanup.
	t.Cleanup(func() {
		if err := c.DeleteGalleryAttachment(context.Background(), tri, uuid); err != nil {
			t.Logf("PROBE RESULT: delete failed: %v", err)
		}
	})

	list, err := c.ListGalleryAttachments(ctx, tri)
	if err != nil {
		t.Fatalf("ListGalleryAttachments: %v", err)
	}
	for _, g := range list {
		if g.UUID == uuid {
			t.Logf("PROBE RESULT: the gallery endpoints work; attachment %s id=%d url=%s",
				g.UUID, g.AttachmentID, g.OriginalURL)
			if g.ContentType != "image/png" {
				t.Errorf("ContentType = %q, want image/png", g.ContentType)
			}
			if g.OriginalURL == "" {
				t.Error("no ORIGINAL url came back")
			}
			return
		}
	}
	t.Logf("PROBE RESULT: upload returned a uuid but the listing holds %d entries and none is %s. "+
		"The gallery endpoints do not persist for an anonymous device; "+
		"UploadGalleryAttachment's doc comment records this.", len(list), uuid)
}
