package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	tricount "github.com/federicobond/go-tricount"
)

// parseMixed parses flags wherever they sit among the positionals, which it
// returns in order. FlagSet alone stops at the first positional, forcing
// `add --on 2026-10-10 80.00 "Internet"` where the flag reads better last.
func parseMixed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		rest := fs.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// resolveTricount picks the tricount a write goes to: by id if the selector
// is a number, by title otherwise, or the only tricount followed when it is
// empty. Archived tricounts count as followed, so two of any kind means ask.
func resolveTricount(ctx context.Context, c *tricount.Client, selector string) (*tricount.Tricount, error) {
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
		return nil, fmt.Errorf("no tricount called %q; use --in with an ID or TITLE from `tricount-cli list`", selector)
	}
	names := make([]string, 0, len(matches))
	for _, t := range matches {
		names = append(names, fmt.Sprintf("%d %s", t.ID, t.Title))
	}
	if selector == "" {
		return nil, fmt.Errorf("this device follows %d tricounts; say which with --in <id>: %s",
			len(matches), strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("%q names %d tricounts; use --in with the id instead: %s",
		selector, len(matches), strings.Join(names, ", "))
}

// parseSplit reads --split: empty for everyone equally, names for those
// members equally, or name=shares pairs for a ratio.
func parseSplit(t *tricount.Tricount, spec string) (tricount.Split, error) {
	if spec == "" {
		return tricount.SplitEqually(t.Members...), nil
	}
	var members []*tricount.Member
	var shares []tricount.MemberShare
	for _, item := range strings.Split(spec, ",") {
		name, value, hasValue := strings.Cut(strings.TrimSpace(item), "=")
		m, err := resolveMember(t, name)
		if err != nil {
			return tricount.Split{}, err
		}
		if !hasValue {
			members = append(members, m)
			continue
		}
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 {
			return tricount.Split{}, fmt.Errorf("--split %s: shares are whole numbers, as in %s=2; use --exact for amounts", item, name)
		}
		shares = append(shares, tricount.MemberShare{Member: m, Share: n})
	}
	if len(members) > 0 && len(shares) > 0 {
		return tricount.Split{}, fmt.Errorf("--split mixes bare names with name=shares; give every member a share")
	}
	if len(shares) > 0 {
		return tricount.SplitByShares(shares), nil
	}
	return tricount.SplitEqually(members...), nil
}

// parseExact reads --exact: name=amount pairs in the tricount's currency.
// The library checks that they sum to the total.
func parseExact(t *tricount.Tricount, spec string) (tricount.Split, error) {
	var amounts []tricount.MemberAmount
	for _, item := range strings.Split(spec, ",") {
		name, value, hasValue := strings.Cut(strings.TrimSpace(item), "=")
		if !hasValue {
			return tricount.Split{}, fmt.Errorf("--exact %s: every member needs an amount, as in %s=17.80", item, name)
		}
		m, err := resolveMember(t, name)
		if err != nil {
			return tricount.Split{}, err
		}
		amt, err := tricount.ParseAmount(value, t.Currency)
		if err != nil {
			return tricount.Split{}, fmt.Errorf("--exact %s: %w", item, err)
		}
		amounts = append(amounts, tricount.MemberAmount{Member: m, Amount: amt})
	}
	return tricount.SplitExactly(amounts), nil
}

// parseCategory returns an app category when the name is one, else the
// name as a custom label.
func parseCategory(name string) (tricount.Category, string) {
	key := strings.ToUpper(strings.NewReplacer(" ", "_", "&", "AND").Replace(strings.TrimSpace(name)))
	if c := tricount.Category(key); c.Valid() {
		return c, ""
	}
	return "", name
}

// parseDate reads YYYY-MM-DD as local noon, so the day survives conversion
// to UTC in any time zone.
func parseDate(s string) (time.Time, error) {
	if s == "" {
		s = time.Now().Format("2006-01-02")
	}
	d, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("--on %q: want a date like 2026-10-10", s)
	}
	return d.Add(12 * time.Hour), nil
}

// payerOr resolves a --paid-by name, defaulting to the linked member.
func payerOr(t *tricount.Tricount, name string) (*tricount.Member, error) {
	if name != "" {
		return resolveMember(t, name)
	}
	if me := t.LinkedMember(); me != nil {
		return me, nil
	}
	return nil, fmt.Errorf("this device is not linked to a member of %s; use --paid-by or `tricount-cli link %d <name>`",
		t.Title, t.ID)
}

func (a *app) add(args []string) error {
	fs := a.flags("add")
	in := fs.String("in", "", "tricount id or title, when the device follows several")
	paidBy := fs.String("paid-by", "", "who paid; default: the linked member")
	on := fs.String("on", "", "YYYY-MM-DD; default: today")
	split := fs.String("split", "", "names, or name=shares pairs; default: everyone equally")
	exact := fs.String("exact", "", "name=amount pairs summing to the total")
	category := fs.String("category", "", "an app category, or any label")
	uuid := fs.String("uuid", "", "idempotency key; a retry cannot post twice")
	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 2 {
		return fmt.Errorf("usage: tricount-cli add <amount> <description> [--paid-by <member>] [--split <spec> | --exact <spec>] [--on <date>] [--category <name>] [--in <tricount>]")
	}
	if *split != "" && *exact != "" {
		return fmt.Errorf("--split and --exact both divide the amount; use one")
	}

	c, err := a.client()
	if err != nil {
		return err
	}
	ctx := context.Background()
	t, err := resolveTricount(ctx, c, *in)
	if err != nil {
		return err
	}
	amount, err := tricount.ParseAmount(positional[0], t.Currency)
	if err != nil {
		return fmt.Errorf("amount %q: %w", positional[0], err)
	}
	payer, err := payerOr(t, *paidBy)
	if err != nil {
		return err
	}
	var division tricount.Split
	if *exact != "" {
		division, err = parseExact(t, *exact)
	} else {
		division, err = parseSplit(t, *split)
	}
	if err != nil {
		return err
	}
	date, err := parseDate(*on)
	if err != nil {
		return err
	}
	cat, custom := parseCategory(*category)

	id, err := c.CreateExpense(ctx, t, tricount.Expense{
		UUID:           *uuid,
		Description:    positional[1],
		Amount:         amount,
		Payer:          payer,
		Split:          division,
		Date:           date,
		Category:       cat,
		CategoryCustom: custom,
	})
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.encode(struct {
			ID int64 `json:"id"`
		}{id})
	}
	fmt.Fprintf(a.stdout, "added %d: %s %s, paid by %s, in %s\n", id, positional[1], amount, payer.DisplayName, t.Title)
	return nil
}

func (a *app) pay(args []string) error {
	fs := a.flags("pay")
	in := fs.String("in", "", "tricount id or title, when the device follows several")
	from := fs.String("from", "", "who paid; default: the linked member")
	to := fs.String("to", "", "who was paid")
	on := fs.String("on", "", "YYYY-MM-DD; default: today")
	description := fs.String("description", "", "a note; default: Reimbursement")
	uuid := fs.String("uuid", "", "idempotency key; a retry cannot post twice")
	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 || *to == "" {
		return fmt.Errorf("usage: tricount-cli pay <amount> --to <member> [--from <member>] [--on <date>] [--in <tricount>]")
	}

	c, err := a.client()
	if err != nil {
		return err
	}
	ctx := context.Background()
	t, err := resolveTricount(ctx, c, *in)
	if err != nil {
		return err
	}
	amount, err := tricount.ParseAmount(positional[0], t.Currency)
	if err != nil {
		return fmt.Errorf("amount %q: %w", positional[0], err)
	}
	payer, err := payerOr(t, *from)
	if err != nil {
		return err
	}
	recipient, err := resolveMember(t, *to)
	if err != nil {
		return err
	}
	date, err := parseDate(*on)
	if err != nil {
		return err
	}

	id, err := c.CreateReimbursement(ctx, t, tricount.Reimbursement{
		UUID:        *uuid,
		Description: *description,
		Amount:      amount,
		From:        payer,
		To:          recipient,
		Date:        date,
	})
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.encode(struct {
			ID int64 `json:"id"`
		}{id})
	}
	fmt.Fprintf(a.stdout, "added %d: %s paid %s to %s, in %s\n", id, payer.DisplayName, amount, recipient.DisplayName, t.Title)
	return nil
}

func (a *app) remove(args []string) error {
	fs := a.flags("remove")
	in := fs.String("in", "", "tricount id or title, when the device follows several")
	positional, err := parseMixed(fs, args)
	if err != nil {
		return err
	}
	if len(positional) != 1 {
		return fmt.Errorf("usage: tricount-cli remove <transaction-id> [--in <tricount>]")
	}
	id, err := strconv.ParseInt(positional[0], 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a transaction id; add and pay print it, show --json lists them", positional[0])
	}

	c, err := a.client()
	if err != nil {
		return err
	}
	ctx := context.Background()
	t, err := resolveTricount(ctx, c, *in)
	if err != nil {
		return err
	}
	if err := c.DeleteTransaction(ctx, t, id); err != nil {
		return err
	}
	if a.jsonOut {
		return a.encode(struct {
			ID int64 `json:"id"`
		}{id})
	}
	fmt.Fprintf(a.stdout, "removed %d from %s\n", id, t.Title)
	return nil
}
