package tricount

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// maxAttachmentBytes caps an upload. The API rejects much larger images
// anyway, and reading an unbounded io.Reader into memory is a poor idea.
const maxAttachmentBytes = 20 << 20

// UploadTransactionAttachment uploads an image and returns the attachment ID to
// pass in Expense.AttachmentIDs or to AddTransactionAttachment.
//
// It takes an io.Reader rather than a path so embedded and in-memory images
// work; callers with a file use os.Open.
func (c *Client) UploadTransactionAttachment(ctx context.Context, t *Tricount, r io.Reader, contentType string) (int64, error) {
	payload, err := readAttachment(t, r, contentType)
	if err != nil {
		return 0, err
	}
	body, err := c.do(ctx, request{
		method:      http.MethodPost,
		userPath:    fmt.Sprintf("/registry/%d/attachment", t.ID),
		raw:         payload,
		contentType: contentType,
		headers:     map[string]string{"X-Bunq-Attachment-Description": ""},
	})
	if err != nil {
		return 0, err
	}
	return decodeID(body)
}

// AddTransactionAttachment associates an already-uploaded attachment with a
// transaction.
//
// The API has no endpoint for this, so the whole transaction is rewritten with
// a longer attachment list. The transaction must be present in
// t.Transactions, since that is where its current contents come from. Adding
// an attachment that is already attached does nothing.
func (c *Client) AddTransactionAttachment(ctx context.Context, t *Tricount, txID, attachmentID int64) error {
	return c.reviseAttachments(ctx, t, txID, func(ids []int64) []int64 {
		for _, id := range ids {
			if id == attachmentID {
				return ids // already attached
			}
		}
		return append(ids, attachmentID)
	})
}

// RemoveTransactionAttachment detaches an attachment from a transaction,
// rewriting the transaction the same way AddTransactionAttachment does. The
// attachment itself is not deleted; the API offers no way to do that.
func (c *Client) RemoveTransactionAttachment(ctx context.Context, t *Tricount, txID, attachmentID int64) error {
	return c.reviseAttachments(ctx, t, txID, func(ids []int64) []int64 {
		out := make([]int64, 0, len(ids))
		for _, id := range ids {
			if id != attachmentID {
				out = append(out, id)
			}
		}
		return out
	})
}

func (c *Client) reviseAttachments(ctx context.Context, t *Tricount, txID int64, revise func([]int64) []int64) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	tx := t.TransactionByID(txID)
	if tx == nil {
		return fmt.Errorf("transaction %d is not in tricount %d; re-read it first: %w",
			txID, t.ID, ErrNotFound)
	}
	spec, err := specFromTransaction(t, tx)
	if err != nil {
		return err
	}
	revised := revise(spec.attachmentIDs)
	if sameIDs(revised, spec.attachmentIDs) {
		return nil
	}
	spec.attachmentIDs = revised
	return c.updateEntry(ctx, t, txID, spec)
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ListGalleryAttachments returns the images attached to the tricount itself
// rather than to a particular transaction.
//
// Against an anonymous device this has only ever been observed to return an
// empty list, including straight after a successful UploadGalleryAttachment.
// See that method for what is known.
func (c *Client) ListGalleryAttachments(ctx context.Context, t *Tricount) ([]*GalleryAttachment, error) {
	if err := checkTricount(t); err != nil {
		return nil, err
	}
	body, err := c.do(ctx, request{
		method:   http.MethodGet,
		userPath: fmt.Sprintf("/registry/%d/gallery-attachment", t.ID),
	})
	if err != nil {
		return nil, err
	}
	wires, err := decodeEnvelope[wireGalleryAttachment](body, "RegistryGalleryAttachment")
	if err != nil {
		return nil, err
	}
	out := make([]*GalleryAttachment, 0, len(wires))
	for _, w := range wires {
		out = append(out, w.toDomain())
	}
	return out, nil
}

// UploadGalleryAttachment adds an image to the tricount's gallery and returns
// its UUID, which is what DeleteGalleryAttachment takes.
//
// Whether this does anything is doubtful. Against an anonymous device the
// upload returns 200 with a UUID and the matching delete returns 200, but the
// image never appears in ListGalleryAttachments or in the registry payload, so
// nothing observable is created. It may need a real bunq account. Attachments
// on transactions, which UploadTransactionAttachment handles, do work — use
// those instead.
func (c *Client) UploadGalleryAttachment(ctx context.Context, t *Tricount, r io.Reader, contentType string) (string, error) {
	payload, err := readAttachment(t, r, contentType)
	if err != nil {
		return "", err
	}
	// The upload path carries a client-generated UUID.
	uuid, err := newUUID()
	if err != nil {
		return "", err
	}
	body, err := c.do(ctx, request{
		method:      http.MethodPost,
		userPath:    fmt.Sprintf("/registry/%d/gallery-attachment/%s", t.ID, uuid),
		raw:         payload,
		contentType: contentType,
		headers:     map[string]string{"X-Bunq-Attachment-Description": ""},
	})
	if err != nil {
		return "", err
	}
	return decodeUUID(body)
}

// DeleteGalleryAttachment permanently removes a gallery image.
func (c *Client) DeleteGalleryAttachment(ctx context.Context, t *Tricount, uuid string) error {
	if err := checkTricount(t); err != nil {
		return err
	}
	if uuid == "" {
		return fmt.Errorf("%w: gallery attachment uuid is empty", ErrInvalidRequest)
	}
	_, err := c.do(ctx, request{
		method:   http.MethodDelete,
		userPath: fmt.Sprintf("/registry/%d/gallery-attachment/%s", t.ID, uuid),
	})
	return err
}

func readAttachment(t *Tricount, r io.Reader, contentType string) ([]byte, error) {
	if err := checkTricount(t); err != nil {
		return nil, err
	}
	if r == nil {
		return nil, fmt.Errorf("%w: attachment reader is nil", ErrInvalidRequest)
	}
	if err := checkImageContentType(contentType); err != nil {
		return nil, err
	}
	payload, err := io.ReadAll(io.LimitReader(r, maxAttachmentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, fmt.Errorf("%w: attachment is empty", ErrInvalidRequest)
	}
	if len(payload) > maxAttachmentBytes {
		return nil, fmt.Errorf("%w: attachment is larger than %d bytes",
			ErrInvalidRequest, maxAttachmentBytes)
	}
	return payload, nil
}

func checkImageContentType(contentType string) error {
	if contentType == "" {
		return fmt.Errorf("%w: attachment content type is empty", ErrInvalidRequest)
	}
	if !strings.HasPrefix(contentType, "image/") {
		return fmt.Errorf("%w: attachment content type %q is not an image",
			ErrInvalidRequest, contentType)
	}
	return nil
}
