package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tricount "github.com/federicobond/go-tricount"
)

// currentPath is where `use` keeps the current tricount's id: a file beside
// credentials.json, so it follows the device identity without touching the
// library's file.
func (a *app) currentPath() string {
	return filepath.Join(filepath.Dir(a.credPath), "current")
}

// current returns the id set by `use`, or 0 when none is.
func (a *app) current() (int64, error) {
	raw, err := os.ReadFile(a.currentPath())
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	id, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s does not hold a tricount id; `tricount-cli use <tricount>` rewrites it", a.currentPath())
	}
	return id, nil
}

// currentTricount fetches the tricount set by `use`, or nil when none is.
// One that is no longer followed is an error, not a fallback: silently
// acting on another tricount is the failure `use` must never produce.
func (a *app) currentTricount(ctx context.Context, c *tricount.Client) (*tricount.Tricount, error) {
	id, err := a.current()
	if err != nil || id == 0 {
		return nil, err
	}
	t, err := c.GetTricountByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("the current tricount, %d, is not followed any more; `tricount-cli use --unset` or pick another: %w", id, err)
	}
	return t, nil
}

func (a *app) setCurrent(id int64) error {
	return os.WriteFile(a.currentPath(), []byte(strconv.FormatInt(id, 10)+"\n"), 0o600)
}

func (a *app) unsetCurrent() error {
	err := os.Remove(a.currentPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (a *app) use(args []string) error {
	fs := a.flags("use")
	unset := fs.Bool("unset", false, "forget the current tricount")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 || (*unset && fs.NArg() > 0) {
		return fmt.Errorf("usage: tricount-cli use [<tricount> | --unset]")
	}
	if *unset {
		if err := a.unsetCurrent(); err != nil {
			return err
		}
		if a.jsonOut {
			return a.encode(nil)
		}
		fmt.Fprintln(a.stdout, "no current tricount; commands fall back to the only one followed")
		return nil
	}

	c, err := a.client()
	if err != nil {
		return err
	}
	ctx := context.Background()

	if fs.NArg() == 0 {
		t, err := a.currentTricount(ctx, c)
		if err != nil {
			return err
		}
		if t == nil {
			if a.jsonOut {
				return a.encode(nil)
			}
			fmt.Fprintln(a.stdout, "no current tricount; `tricount-cli use <tricount>` sets one")
			return nil
		}
		return a.reportCurrent(t)
	}

	// Resolve against what is followed, never against the current setting,
	// or `use` could only ever confirm itself.
	t, err := a.resolveFollowed(ctx, c, fs.Arg(0))
	if err != nil {
		return err
	}
	if err := a.setCurrent(t.ID); err != nil {
		return err
	}
	return a.reportCurrent(t)
}

func (a *app) reportCurrent(t *tricount.Tricount) error {
	if a.jsonOut {
		return a.encode(struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		}{t.ID, t.Title})
	}
	fmt.Fprintf(a.stdout, "current tricount is %d %s\n", t.ID, t.Title)
	return nil
}
