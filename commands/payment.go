package commands

import (
	"fmt"
	"os"

	"github.com/shopspring/decimal"
	"github.com/urfave/cli"
	"github.com/xenaex/daccs-cli/clients"
)

// Payment commands definition
var Payment = cli.Command{
	Name:    "payment",
	Aliases: []string{"p"},
	Usage:   "Payment processing commands",
	Subcommands: []cli.Command{
		{
			Name:   "list",
			Usage:  "List last payments with paging support",
			Action: paymentList,
			Flags: []cli.Flag{
				cli.IntFlag{Name: "offset", Value: 0},
				cli.IntFlag{Name: "limit", Value: 10},
			},
		},
		{
			Name:   "send",
			Usage:  "Send payment to specified account with specified amount",
			Action: paymentSend,
			Flags: []cli.Flag{
				cli.Int64Flag{Name: "account"},
				cli.StringFlag{Name: "amount"},
			},
		},
	},
}

// paymentList command handler
func paymentList(c *cli.Context) error {
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}
	res, err := lncli.Payments(c.Int("offset"), c.Int("limit"))
	if err != nil {
		return err
	}
	ResponseJSON(res)
	return nil
}

// paymentSend command handler
func paymentSend(c *cli.Context) error {
	// Show command help if no arguments provided
	if c.NumFlags() == 0 {
		cli.ShowCommandHelp(c, "send")
		return nil
	}

	// Parse and validate account and amount
	// TODO: get limits from API
	account := c.Int64("account")
	if account <= 0 {
		return fmt.Errorf("Invalid account")
	}
	amount, err := decimal.NewFromString(c.String("amount"))
	if err != nil {
		return fmt.Errorf("Invalid amount value")
	}
	if amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("Amount should be greater than zero")
	}

	restcli, err := clients.NewRestClient(c)
	if err != nil {
		return err
	}
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}

	// Request API for invoices to pay
	invoices, err := restcli.IssueInvoices(account)
	if err != nil {
		return fmt.Errorf("Error %s on getting invoices to pay", err)
	}

	// Distribute amount between channels proportional to their free capacity
	channels, err := lncli.ActiveChannels()
	if err != nil {
		return fmt.Errorf("Error %s on getting active channels", err)
	}

	if len(channels) == 0 {
		return fmt.Errorf("Opened channels not found")
	}

	nodeCapacity := make(map[string]decimal.Decimal)
	totalCapacity := decimal.Zero
	for _, c := range channels {
		nodeCapacity[c.Node] = nodeCapacity[c.Node].Add(c.LocalBalance)
		totalCapacity = totalCapacity.Add(c.LocalBalance)
	}
	paymentAmounts := make(map[string]decimal.Decimal)
	amountLeft := amount
	for i, inv := range invoices {
		ca := amount.Mul(nodeCapacity[inv.NodeID].Div(totalCapacity))
		paymentAmounts[inv.NodeID] = ca
		if i < len(invoices)-1 {
			amountLeft = amountLeft.Sub(ca)
		} else {
			paymentAmounts[inv.NodeID] = amountLeft
		}
	}

	// Send payments
	errors := []Error{}
	for _, i := range invoices {
		a := paymentAmounts[i.NodeID]
		err := lncli.SendPayment(i.PaymentRequest, a)
		if err != nil {
			err = fmt.Errorf("Error %s on sending payment on %s to %s", err, a, i.NodeID)
			errors = append(errors, Error{Error: err.Error()})
		}
	}
	if len(errors) > 0 {
		ResponseError(errors)
		os.Exit(1)
	}
	return nil
}
