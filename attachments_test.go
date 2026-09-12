package tricount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

var fakeJPEG = []byte{0xFF, 0xD8, 0xFF, 0xE0, 'f', 'a', 'k', 'e'}

func TestUploadTransactionAttachment(t *testing.T) {
	var method, path, contentType string
	var hasDescription bool
	var body []byte
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		contentType = r.Header.Get("Content-Type")
		_, hasDescription = r.Header["X-Bunq-Attachment-Description"]
		body, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"Response":[{"Id":{"id":12345}}]}`))
	})
	tri := entryTestTricount()

	id, err := c.UploadTransactionAttachment(context.Background(), tri,
		bytes.NewReader(fakeJPEG), "image/jpeg")
	if err != nil {
		t.Fatalf("UploadTransactionAttachment: %v", err)
	}
	if id != 12345 {
		t.Errorf("id = %d", id)
	}
	if method != http.MethodPost {
		t.Errorf("method = %s, want POST", method)
	}
	if path != "/v1/user/79290957/registry/1/attachment" {
		t.Errorf("path = %q", path)
	}
	if contentType != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", contentType)
	}
	// The header must be present even though it is empty, which is what the
	// app sends.
	if !hasDescription {
		t.Error("X-Bunq-Attachment-Description was not sent")
	}
	if !bytes.Equal(body, fakeJPEG) {
		t.Errorf("uploaded %d bytes, want the %d given", len(body), len(fakeJPEG))
	}
}

func TestUploadAttachmentValidation(t *testing.T) {
	var called bool
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})
	tri := entryTestTricount()
	ctx := context.Background()

	if _, err := c.UploadTransactionAttachment(ctx, tri, nil, "image/jpeg"); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a nil reader should be rejected")
	}
	if _, err := c.UploadTransactionAttachment(ctx, tri, bytes.NewReader(fakeJPEG), ""); !errors.Is(err, ErrInvalidRequest) {
		t.Error("an empty content type should be rejected")
	}
	if _, err := c.UploadTransactionAttachment(ctx, tri, bytes.NewReader(fakeJPEG), "text/plain"); !errors.Is(err, ErrInvalidRequest) {
		t.Error("a non-image content type should be rejected")
	}
	if _, err := c.UploadTransactionAttachment(ctx, tri, bytes.NewReader(nil), "image/png"); !errors.Is(err, ErrInvalidRequest) {
		t.Error("an empty body should be rejected")
	}
	if called {
		t.Error("validation failures must not reach the network")
	}
}

// attachedTricount is a tricount holding one expense with the given
// attachments.
func attachedTricount(attachmentIDs ...int64) *Tricount {
	tri := entryTestTricount()
	tri.Transactions = []*Transaction{{
		ID: 900, Description: "Ramen", Kind: KindExpense, Status: TransactionActive,
		Amount: MustParseAmount("25.50", "EUR"), PayerUUID: "u-alice",
		Allocations: []Allocation{
			{MemberUUID: "u-alice", Amount: MustParseAmount("12.75", "EUR"), Type: AllocationAmount},
			{MemberUUID: "u-bob", Amount: MustParseAmount("12.75", "EUR"), Type: AllocationAmount},
		},
		AttachmentIDs: attachmentIDs,
	}}
	return tri
}

func TestAddTransactionAttachmentUpdatesTheEntry(t *testing.T) {
	// The API has no endpoint for associating an attachment, so the client
	// rewrites the whole transaction with a longer attachment array.
	var got capturedEntry
	var path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"Response":[{"Id":{"id":900}}]}`))
	})

	tri := attachedTricount(7)
	if err := c.AddTransactionAttachment(context.Background(), tri, 900, 12345); err != nil {
		t.Fatalf("AddTransactionAttachment: %v", err)
	}
	if path != "PUT /v1/user/79290957/registry/1/registry-entry/900" {
		t.Errorf("request = %q", path)
	}
	if len(got.Attachment) != 2 {
		t.Fatalf("sent %d attachments, want 2", len(got.Attachment))
	}
	if got.Attachment[0].ID != 7 || got.Attachment[1].ID != 12345 {
		t.Errorf("attachment ids = %d, %d, want 7 and 12345",
			got.Attachment[0].ID, got.Attachment[1].ID)
	}
	// The rest of the transaction must survive the rewrite untouched.
	if got.Description != "Ramen" || got.Amount.Value != "-25.50" {
		t.Errorf("the rewrite changed the transaction: %+v", got)
	}
	if len(got.Allocations) != 2 {
		t.Errorf("the rewrite lost allocations: %+v", got.Allocations)
	}
}

func TestAddTransactionAttachmentIsIdempotent(t *testing.T) {
	var calls int
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(`{"Response":[{"Id":{"id":900}}]}`))
	})
	tri := attachedTricount(12345)

	if err := c.AddTransactionAttachment(context.Background(), tri, 900, 12345); err != nil {
		t.Fatalf("AddTransactionAttachment: %v", err)
	}
	if calls != 0 {
		t.Error("adding an attachment that is already present should make no request")
	}
}

func TestRemoveTransactionAttachment(t *testing.T) {
	var got capturedEntry
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"Response":[{"Id":{"id":900}}]}`))
	})
	tri := attachedTricount(7, 12345)

	if err := c.RemoveTransactionAttachment(context.Background(), tri, 900, 7); err != nil {
		t.Fatalf("RemoveTransactionAttachment: %v", err)
	}
	if len(got.Attachment) != 1 || got.Attachment[0].ID != 12345 {
		t.Errorf("attachments = %+v, want just 12345", got.Attachment)
	}
}

func TestTransactionAttachmentUnknownTransaction(t *testing.T) {
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {})
	tri := entryTestTricount()
	if err := c.AddTransactionAttachment(context.Background(), tri, 999, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound for a transaction not in t.Transactions", err)
	}
}

func TestListGalleryAttachments(t *testing.T) {
	var path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Write([]byte(`{"Response":[{"RegistryGalleryAttachment":{
			"membership_uuid":"u-alice",
			"attachment":{"id":12345,"uuid":"att-uuid","content_type":"image/jpeg",
			  "urls":[{"type":"THUMBNAIL","url":"https://example.invalid/t.jpg"},
			          {"type":"ORIGINAL","url":"https://example.invalid/o.jpg"}]}
		}}]}`))
	})
	tri := entryTestTricount()

	list, err := c.ListGalleryAttachments(context.Background(), tri)
	if err != nil {
		t.Fatalf("ListGalleryAttachments: %v", err)
	}
	if path != "/v1/user/79290957/registry/1/gallery-attachment" {
		t.Errorf("path = %q", path)
	}
	if len(list) != 1 {
		t.Fatalf("got %d attachments, want 1", len(list))
	}
	g := list[0]
	if g.AttachmentID != 12345 || g.UUID != "att-uuid" {
		t.Errorf("attachment = %+v", g)
	}
	if g.OriginalURL != "https://example.invalid/o.jpg" {
		t.Errorf("OriginalURL = %q, want the ORIGINAL entry, not the thumbnail", g.OriginalURL)
	}
	if g.UploaderUUID != "u-alice" {
		t.Errorf("UploaderUUID = %q", g.UploaderUUID)
	}
}

func TestUploadGalleryAttachment(t *testing.T) {
	var path, contentType string
	var body []byte
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		contentType = r.Header.Get("Content-Type")
		body, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"Response":[{"UUID":{"uuid":"gallery-uuid"}}]}`))
	})
	tri := entryTestTricount()

	uuid, err := c.UploadGalleryAttachment(context.Background(), tri,
		bytes.NewReader(fakeJPEG), "image/jpeg")
	if err != nil {
		t.Fatalf("UploadGalleryAttachment: %v", err)
	}
	if uuid != "gallery-uuid" {
		t.Errorf("uuid = %q, want the server's", uuid)
	}
	// The path carries a client-generated UUID.
	prefix := "/v1/user/79290957/registry/1/gallery-attachment/"
	if !strings.HasPrefix(path, prefix) {
		t.Fatalf("path = %q, want the %q prefix", path, prefix)
	}
	if pathUUID := strings.TrimPrefix(path, prefix); len(pathUUID) != 36 {
		t.Errorf("path uuid = %q, want a 36-character uuid", pathUUID)
	}
	if contentType != "image/jpeg" {
		t.Errorf("Content-Type = %q", contentType)
	}
	if !bytes.Equal(body, fakeJPEG) {
		t.Error("the uploaded bytes differ from what was given")
	}
}

func TestDeleteGalleryAttachment(t *testing.T) {
	var method, path string
	c, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		w.Write([]byte(`{"Response":[{"Id":{"id":12345}}]}`))
	})
	tri := entryTestTricount()

	if err := c.DeleteGalleryAttachment(context.Background(), tri, "att-uuid"); err != nil {
		t.Fatalf("DeleteGalleryAttachment: %v", err)
	}
	if method != http.MethodDelete {
		t.Errorf("method = %s, want DELETE", method)
	}
	if path != "/v1/user/79290957/registry/1/gallery-attachment/att-uuid" {
		t.Errorf("path = %q", path)
	}

	if err := c.DeleteGalleryAttachment(context.Background(), tri, ""); !errors.Is(err, ErrInvalidRequest) {
		t.Error("an empty uuid should be rejected")
	}
}

func TestDecodeUUID(t *testing.T) {
	uuid, err := decodeUUID([]byte(`{"Response":[{"UUID":{"uuid":"abc-123"}}]}`))
	if err != nil {
		t.Fatalf("decodeUUID: %v", err)
	}
	if uuid != "abc-123" {
		t.Errorf("uuid = %q", uuid)
	}
	if _, err := decodeUUID([]byte(`{"Response":[]}`)); err == nil {
		t.Error("an empty Response should error")
	}
}
