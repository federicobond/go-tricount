package tricount_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/federicobond/go-tricount"
)

func mustCredentials() tricount.Credentials {
	creds, err := tricount.LoadOrGenerateCredentials("tricount_credentials.json")
	if err != nil {
		log.Fatal(err)
	}
	return creds
}

func Example_joinAndRead() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()

	t, err := c.JoinTricount(ctx, "tABC123xyz")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s (%s), %d members, %d transactions\n",
		t.Title, t.Currency, len(t.Members), len(t.Transactions))

	for _, tx := range t.Transactions {
		payer := t.MemberByUUID(tx.PayerUUID)
		fmt.Printf("%s  %-20s %10s  %s\n",
			tx.Date.Format("2006-01-02"), tx.Description, tx.Amount, payer.DisplayName)
	}
}

func Example_createExpenseEqualSplit() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()

	t, err := c.JoinTricount(ctx, "tABC123xyz")
	if err != nil {
		log.Fatal(err)
	}
	alice := t.MemberByName("Alice")
	if alice == nil {
		log.Fatal("no member called Alice")
	}

	id, err := c.CreateExpense(ctx, t, tricount.Expense{
		Description: "Dinner",
		Amount:      tricount.MustParseAmount("120.00", "EUR"),
		Payer:       alice,
		Split:       tricount.SplitEqually(t.Members...),
		Category:    tricount.CategoryFoodAndDrink,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("created transaction", id)
}

func Example_createExpenseRatioSplit() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()
	t, _ := c.JoinTricount(ctx, "tABC123xyz")
	ana, luis := t.MemberByName("Ana"), t.MemberByName("Luis")

	// Ana takes twice Luis's share.
	_, err := c.CreateExpense(ctx, t, tricount.Expense{
		Description: "Hotel",
		Amount:      tricount.MustParseAmount("300.00", "EUR"),
		Payer:       ana,
		Split: tricount.SplitByShares([]tricount.MemberShare{
			{Member: ana, Share: 2},
			{Member: luis, Share: 1},
		}),
	})
	if err != nil {
		log.Fatal(err)
	}
}

func Example_createExpenseExactSplit() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()
	t, _ := c.JoinTricount(ctx, "tABC123xyz")
	ana, luis := t.MemberByName("Ana"), t.MemberByName("Luis")

	// Itemizing a restaurant bill: each person pays exactly what they had. The
	// amounts must sum to the total, and the error says so if they do not.
	_, err := c.CreateExpense(ctx, t, tricount.Expense{
		Description: "Lunch",
		Amount:      tricount.MustParseAmount("41.30", "EUR"),
		Payer:       ana,
		Split: tricount.SplitExactly([]tricount.MemberAmount{
			{Member: ana, Amount: tricount.MustParseAmount("17.80", "EUR")},
			{Member: luis, Amount: tricount.MustParseAmount("23.50", "EUR")},
		}),
	})
	if err != nil {
		log.Fatal(err)
	}
}

func Example_incomeAndReimbursement() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()
	t, _ := c.JoinTricount(ctx, "tABC123xyz")
	ana, luis := t.MemberByName("Ana"), t.MemberByName("Luis")

	// Ana collected a deposit refund that belongs to both of them, so she now
	// owes Luis his half.
	if _, err := c.CreateIncome(ctx, t, tricount.Income{
		Description: "Deposit refund",
		Amount:      tricount.MustParseAmount("60.00", "EUR"),
		Receiver:    ana,
		Split:       tricount.SplitEqually(ana, luis),
	}); err != nil {
		log.Fatal(err)
	}

	// Luis settles part of what he owes with a direct transfer.
	if _, err := c.CreateReimbursement(ctx, t, tricount.Reimbursement{
		Description: "Bizum",
		Amount:      tricount.MustParseAmount("15.00", "EUR"),
		From:        luis,
		To:          ana,
	}); err != nil {
		log.Fatal(err)
	}
}

func Example_balancesAndSettling() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()
	t, _ := c.JoinTricount(ctx, "tABC123xyz")

	balances, err := t.Balances()
	if err != nil {
		log.Fatal(err)
	}
	for name, balance := range balances {
		fmt.Printf("%-15s %10s\n", name, balance)
	}

	transfers, err := t.Settle()
	if err != nil {
		log.Fatal(err)
	}
	for _, tr := range transfers {
		fmt.Printf("%s pays %s to %s\n", tr.From.DisplayName, tr.Amount, tr.To.DisplayName)
	}
}

func Example_updateATransaction() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()
	t, _ := c.JoinTricount(ctx, "tABC123xyz")

	// Updates are full replacements, so read the transaction, change what you
	// want, and write the whole thing back.
	tx := t.Transactions[0]
	payer := t.MemberByUUID(tx.PayerUUID)

	split := make([]tricount.MemberAmount, 0, len(tx.Allocations))
	for _, a := range tx.Allocations {
		split = append(split, tricount.MemberAmount{
			Member: t.MemberByUUID(a.MemberUUID),
			Amount: a.Amount,
		})
	}

	err := c.UpdateExpense(ctx, t, tx.ID, tricount.Expense{
		Description: "Corrected description",
		Amount:      tx.Amount,
		Payer:       payer,
		Split:       tricount.SplitExactly(split),
		Date:        tx.Date,
		Category:    tx.Category,
	})
	if err != nil {
		log.Fatal(err)
	}
}

func Example_attachAReceipt() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()
	t, _ := c.JoinTricount(ctx, "tABC123xyz")

	f, err := os.Open("receipt.jpg")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	attachmentID, err := c.UploadTransactionAttachment(ctx, t, f, "image/jpeg")
	if err != nil {
		log.Fatal(err)
	}
	_, err = c.CreateExpense(ctx, t, tricount.Expense{
		Description:   "Groceries",
		Amount:        tricount.MustParseAmount("53.20", "EUR"),
		Payer:         t.Members[0],
		Split:         tricount.SplitEqually(t.Members...),
		AttachmentIDs: []int64{attachmentID},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func Example_foreignCurrency() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()
	t, _ := c.JoinTricount(ctx, "tABC123xyz") // a EUR tricount

	// Spent in yen, recorded in the tricount's euros. Leaving ExchangeRate
	// empty would fetch the rate instead.
	spent := tricount.MustParseAmount("15000", "JPY")
	rate, err := c.ExchangeRate(ctx, "JPY", t.Currency)
	if err != nil {
		log.Fatal(err)
	}
	converted, err := spent.Convert(rate, t.Currency)
	if err != nil {
		log.Fatal(err)
	}

	_, err = c.CreateExpense(ctx, t, tricount.Expense{
		Description:  "Tokyo hotel",
		Amount:       converted,
		Payer:        t.Members[0],
		Split:        tricount.SplitEqually(t.Members...),
		LocalAmount:  &spent,
		ExchangeRate: rate,
	})
	if err != nil {
		log.Fatal(err)
	}
}

func Example_handlingErrors() {
	c := tricount.NewClient(mustCredentials())
	ctx := context.Background()

	_, err := c.GetTricount(ctx, "tDOESNOTEXIST")
	switch {
	case errors.Is(err, tricount.ErrNotFound):
		fmt.Println("no such tricount")
	case errors.Is(err, tricount.ErrInvalidRequest):
		fmt.Println("the request was wrong before it was sent:", err)
	case err != nil:
		var apiErr *tricount.Error
		if errors.As(err, &apiErr) {
			fmt.Printf("api error %d: %s (response %s)\n",
				apiErr.StatusCode, apiErr.Description, apiErr.ResponseID)
		}
	}
}
