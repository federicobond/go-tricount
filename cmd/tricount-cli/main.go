// Command tricount-cli inspects tricounts and provisions the device that
// reads them.
//
// There is no login. The first run generates a device identity and saves it
// under the OS config directory — ~/Library/Application Support/tricount on
// macOS, ~/.config/tricount on Linux — or wherever $TRICOUNT_CREDENTIALS
// points. Keep that file: it is what your synced tricounts hang off, and
// whoami prints its path.
//
// Usage:
//
//	tricount-cli list
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	tricount "github.com/federicobond/go-tricount"
)

const (
	exitOK    = 0
	exitErr   = 1
	exitUsage = 2
)

// app holds everything a run needs, so tests can supply their own writers,
// credentials path and client options.
type app struct {
	stdout   io.Writer
	stderr   io.Writer
	credPath string
	opts     []tricount.Option

	// jsonOut is set by the --json flag every command accepts.
	jsonOut bool
}

func main() {
	a := &app{stdout: os.Stdout, stderr: os.Stderr, credPath: defaultCredentialsPath()}
	os.Exit(a.run(os.Args[1:]))
}

// defaultCredentialsPath is credentials.json under the OS config directory,
// overridable by TRICOUNT_CREDENTIALS.
func defaultCredentialsPath() string {
	if p := os.Getenv("TRICOUNT_CREDENTIALS"); p != "" {
		return p
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "credentials.json"
	}
	return filepath.Join(dir, "tricount", "credentials.json")
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		a.usage()
		return exitUsage
	}
	switch args[0] {
	case "list":
		return a.status(a.list(args[1:]))
	case "show":
		return a.status(a.show(args[1:]))
	case "balances":
		return a.status(a.balances(args[1:]))
	case "settle":
		return a.status(a.settle(args[1:]))
	case "join":
		return a.status(a.join(args[1:]))
	case "leave":
		return a.status(a.leave(args[1:]))
	case "link":
		return a.status(a.link(args[1:]))
	case "whoami":
		return a.status(a.whoami(args[1:]))
	default:
		fmt.Fprintf(a.stderr, "unknown command %q\n", args[0])
		a.usage()
		return exitUsage
	}
}

func (a *app) usage() {
	fmt.Fprint(a.stderr, `usage: tricount-cli <command> [arguments]

commands:
  list              the tricounts this device follows
  show <id>         members and transactions of one tricount
  balances <id>     each member's net position
  settle <id>       a plan of transfers that clears every balance
  join <token>      follow a tricount, by the tXXXX part of its sharing link
                    --as <name> to be that member, created if absent
  leave <id>        stop following a tricount
  link <id> <name>  set which member this device counts as, --create to add them
  whoami            this device's identity and its member in each tricount
`)
}

// flags builds a command's flag set, with the --json switch every command
// shares bound to the app.
func (a *app) flags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.BoolVar(&a.jsonOut, "json", false, "emit JSON instead of a table")
	return fs
}

// encode writes v as indented JSON.
func (a *app) encode(v any) error {
	enc := json.NewEncoder(a.stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// status turns an error into an exit code, reporting it on stderr.
func (a *app) status(err error) int {
	if err != nil {
		fmt.Fprintf(a.stderr, "tricount-cli: %v\n", err)
		return exitErr
	}
	return exitOK
}

// client loads the device identity, generating one on first use.
func (a *app) client() (*tricount.Client, error) {
	c, _, err := a.clientAndCredentials()
	return c, err
}

func (a *app) clientAndCredentials() (*tricount.Client, tricount.Credentials, error) {
	if dir := filepath.Dir(a.credPath); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, tricount.Credentials{}, err
		}
	}
	creds, err := tricount.LoadOrGenerateCredentials(a.credPath)
	if err != nil {
		return nil, tricount.Credentials{}, err
	}
	return tricount.NewClient(creds, a.opts...), creds, nil
}

func (a *app) list(args []string) error {
	fs := a.flags("list")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, err := a.client()
	if err != nil {
		return err
	}
	all, err := c.ListTricounts(context.Background())
	if err != nil {
		return err
	}

	if a.jsonOut {
		type row struct {
			ID       int64
			Title    string
			Currency string
			Archived bool
		}
		rows := make([]row, 0, len(all))
		for _, t := range all {
			rows = append(rows, row{t.ID, t.Title, t.Currency, t.IsArchived()})
		}
		return a.encode(rows)
	}

	w := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tCURRENCY\tSTATUS")
	for _, t := range all {
		status := "active"
		if t.IsArchived() {
			status = "archived"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\n", t.ID, t.Title, t.Currency, status)
	}
	return w.Flush()
}

// tricountArg parses the single numeric id these commands take and fetches it.
func (a *app) tricountArg(name string, args []string) (*tricount.Tricount, error) {
	fs := a.flags(name)
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() != 1 {
		return nil, fmt.Errorf("usage: tricount-cli %s <id>", name)
	}
	id, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not a tricount id; use the ID column from `tricount-cli list`", fs.Arg(0))
	}

	c, err := a.client()
	if err != nil {
		return nil, err
	}
	return c.GetTricountByID(context.Background(), id)
}

func (a *app) show(args []string) error {
	t, err := a.tricountArg("show", args)
	if err != nil {
		return err
	}

	if a.jsonOut {
		return a.encode(t)
	}

	fmt.Fprintf(a.stdout, "%s (%s)\n", t.Title, t.Currency)
	if me := t.LinkedMember(); me != nil {
		fmt.Fprintf(a.stdout, "this device is %s\n", me.DisplayName)
	}

	w := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "\nMEMBERS")
	for _, m := range t.Members {
		fmt.Fprintf(w, "  %s\n", m.DisplayName)
	}
	for _, m := range t.FormerMembers {
		fmt.Fprintf(w, "  %s\t(removed)\n", m.DisplayName)
	}

	fmt.Fprintln(w, "\nTRANSACTIONS\n  DATE\tDESCRIPTION\tAMOUNT\tPAID BY")
	for _, tx := range t.Transactions {
		payer := "?"
		if m := t.MemberByUUID(tx.PayerUUID); m != nil {
			payer = m.DisplayName
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
			tx.Date.Format("2006-01-02"), tx.Description, tx.Amount, payer)
	}
	return w.Flush()
}

func (a *app) balances(args []string) error {
	t, err := a.tricountArg("balances", args)
	if err != nil {
		return err
	}
	balances, err := t.Balances()
	if err != nil {
		return err
	}

	if a.jsonOut {
		return a.encode(balances)
	}

	names := make([]string, 0, len(balances))
	for name := range balances {
		names = append(names, name)
	}
	sort.Strings(names)

	w := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "MEMBER\tBALANCE")
	for _, name := range names {
		fmt.Fprintf(w, "%s\t%s\n", name, balances[name])
	}
	return w.Flush()
}

func (a *app) settle(args []string) error {
	t, err := a.tricountArg("settle", args)
	if err != nil {
		return err
	}
	transfers, err := t.Settle()
	if err != nil {
		return err
	}
	if a.jsonOut {
		type row struct {
			From   string
			To     string
			Amount tricount.Amount
		}
		rows := make([]row, 0, len(transfers))
		for _, tr := range transfers {
			rows = append(rows, row{tr.From.DisplayName, tr.To.DisplayName, tr.Amount})
		}
		return a.encode(rows)
	}
	if len(transfers) == 0 {
		fmt.Fprintln(a.stdout, "everyone is square")
		return nil
	}

	w := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "FROM\tTO\tAMOUNT")
	for _, tr := range transfers {
		fmt.Fprintf(w, "%s\t%s\t%s\n", tr.From.DisplayName, tr.To.DisplayName, tr.Amount)
	}
	return w.Flush()
}

func (a *app) join(args []string) error {
	fs := a.flags("join")
	as := fs.String("as", "", "the member this device counts as, created if absent")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: tricount-cli join [--as <name>] <token>")
	}

	c, err := a.client()
	if err != nil {
		return err
	}
	ctx := context.Background()
	t, err := c.JoinTricount(ctx, fs.Arg(0))
	if err != nil {
		return err
	}

	// Joining auto-links to the member with the lowest id, which is whoever
	// happened to be created first rather than whoever is holding this device.
	if *as != "" {
		m, err := memberFor(ctx, c, t, *as, true)
		if err != nil {
			return err
		}
		if err := c.LinkToMember(ctx, t, m); err != nil {
			return err
		}
	}

	if a.jsonOut {
		return a.encode(t)
	}

	fmt.Fprintf(a.stdout, "joined %d %s (%s)\n", t.ID, t.Title, t.Currency)
	if me := t.LinkedMember(); me != nil {
		if *as != "" {
			fmt.Fprintf(a.stdout, "this device is now %s\n", me.DisplayName)
		} else {
			fmt.Fprintf(a.stdout, "this device is %s; `tricount-cli link %d <name>` to change it\n",
				me.DisplayName, t.ID)
		}
	}
	return nil
}

func (a *app) leave(args []string) error {
	t, err := a.tricountArg("leave", args)
	if err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	if err := c.LeaveTricount(context.Background(), t); err != nil {
		return err
	}
	if a.jsonOut {
		return a.encode(t)
	}
	fmt.Fprintf(a.stdout, "left %d %s; rejoining with its sharing token restores access\n", t.ID, t.Title)
	return nil
}

// resolveMember finds a member by display name, case-insensitively. Unlike
// Tricount.MemberByName it refuses to pick when a name is ambiguous.
func resolveMemberMatches(t *tricount.Tricount, name string) (matches []*tricount.Member, ambiguous bool) {
	for _, m := range t.Members {
		if strings.EqualFold(m.DisplayName, name) {
			matches = append(matches, m)
		}
	}
	return matches, len(matches) > 1
}

func resolveMember(t *tricount.Tricount, name string) (*tricount.Member, error) {
	matches, _ := resolveMemberMatches(t, name)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		names := make([]string, 0, len(t.Members))
		for _, m := range t.Members {
			names = append(names, m.DisplayName)
		}
		return nil, fmt.Errorf("no member called %q; this tricount has %s. Use --create to add them",
			name, strings.Join(names, ", "))
	default:
		uuids := make([]string, 0, len(matches))
		for _, m := range matches {
			uuids = append(uuids, m.UUID)
		}
		return nil, fmt.Errorf("%q is ambiguous; %d members share that name (%s)",
			name, len(matches), strings.Join(uuids, ", "))
	}
}

// memberFor resolves name to a member, creating it when create is set and no
// member has that name.
func memberFor(ctx context.Context, c *tricount.Client, t *tricount.Tricount, name string, create bool) (*tricount.Member, error) {
	m, err := resolveMember(t, name)
	if err == nil {
		return m, nil
	}
	if !create {
		return nil, err
	}
	// Only an absent name is worth creating; an ambiguous one still is not.
	if _, ambiguous := resolveMemberMatches(t, name); ambiguous {
		return nil, err
	}
	if err := c.AddMembers(ctx, t, name); err != nil {
		return nil, err
	}
	return resolveMember(t, name)
}

func (a *app) link(args []string) error {
	fs := a.flags("link")
	create := fs.Bool("create", false, "add the member if no one by that name exists")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: tricount-cli link [--create] <id> <member>")
	}
	id, err := strconv.ParseInt(fs.Arg(0), 10, 64)
	if err != nil {
		return fmt.Errorf("%q is not a tricount id; use the ID column from `tricount-cli list`", fs.Arg(0))
	}

	c, err := a.client()
	if err != nil {
		return err
	}
	ctx := context.Background()
	t, err := c.GetTricountByID(ctx, id)
	if err != nil {
		return err
	}
	m, err := memberFor(ctx, c, t, fs.Arg(1), *create)
	if err != nil {
		return err
	}
	if err := c.LinkToMember(ctx, t, m); err != nil {
		return err
	}
	if a.jsonOut {
		return a.encode(m)
	}
	fmt.Fprintf(a.stdout, "this device is now %s in %s\n", m.DisplayName, t.Title)
	return nil
}

func (a *app) whoami(args []string) error {
	fs := a.flags("whoami")
	if err := fs.Parse(args); err != nil {
		return err
	}

	c, creds, err := a.clientAndCredentials()
	if err != nil {
		return err
	}
	ctx := context.Background()
	userID, err := c.Authenticate(ctx)
	if err != nil {
		return err
	}
	all, err := c.ListTricounts(ctx)
	if err != nil {
		return err
	}

	if a.jsonOut {
		type link struct {
			ID       int64
			Title    string
			LinkedAs string
		}
		out := struct {
			Device      string
			User        int64
			Credentials string
			Tricounts   []link
		}{Device: creds.AppID, User: userID, Credentials: a.credPath, Tricounts: []link{}}
		for _, t := range all {
			var name string
			if m := t.LinkedMember(); m != nil {
				name = m.DisplayName
			}
			out.Tricounts = append(out.Tricounts, link{t.ID, t.Title, name})
		}
		return a.encode(out)
	}

	fmt.Fprintf(a.stdout, "device %s (user %d)\ncredentials %s\n\n", creds.AppID, userID, a.credPath)
	w := tabwriter.NewWriter(a.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTRICOUNT\tLINKED AS")
	for _, t := range all {
		linked := "(unlinked)"
		if m := t.LinkedMember(); m != nil {
			linked = m.DisplayName
		}
		fmt.Fprintf(w, "%d\t%s\t%s\n", t.ID, t.Title, linked)
	}
	return w.Flush()
}
