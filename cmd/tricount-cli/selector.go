package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tricount "github.com/federicobond/go-tricount"
)

// Which tricount a command acts on. Every command but list, join and whoami
// goes through resolveTricount; use goes through resolveFollowed, since it
// must be able to switch away from the current one.

// resolveTricount picks the tricount a command acts on: the selector when
// given, else the one set by `use`, else the only tricount followed.
func (a *app) resolveTricount(ctx context.Context, c *tricount.Client, selector string) (*tricount.Tricount, error) {
	if selector == "" {
		t, err := a.currentTricount(ctx, c)
		if err != nil || t != nil {
			return t, err
		}
	}
	return a.resolveFollowed(ctx, c, selector)
}

// resolveFollowed resolves a selector against what the device follows: by id
// if it is a number, by title otherwise, or the only tricount followed when
// it is empty. Archived tricounts count as followed, so two of any kind
// means ask.
func (a *app) resolveFollowed(ctx context.Context, c *tricount.Client, selector string) (*tricount.Tricount, error) {
	if id, err := strconv.ParseInt(selector, 10, 64); err == nil {
		return c.GetTricountByID(ctx, id)
	}
	all, err := c.ListTricounts(ctx)
	if err != nil {
		return nil, err
	}
	var matches []*tricount.Tricount
	for _, t := range all {
		if selector == "" || strings.EqualFold(t.Title, selector) {
			matches = append(matches, t)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		if selector == "" {
			return nil, fmt.Errorf("this device follows no tricount; `tricount-cli join <token>` first")
		}
		return nil, fmt.Errorf("no tricount called %q; `tricount-cli list` shows the ids and titles", selector)
	}
	names := make([]string, 0, len(matches))
	for _, t := range matches {
		names = append(names, fmt.Sprintf("%d %s", t.ID, t.Title))
	}
	if selector == "" {
		return nil, fmt.Errorf("this device follows %d tricounts; name one by id or title, or `tricount-cli use` one: %s",
			len(matches), strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("%q names %d tricounts; use the id instead: %s",
		selector, len(matches), strings.Join(names, ", "))
}
