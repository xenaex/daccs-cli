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
	if amount.LessThan(minChannelPayment) {
		return fmt.Errorf("Amount should be greater or equal to min payment amount %s", minChannelPayment)
	}

	restcli, err := clients.NewRestClient(c)
	if err != nil {
		return err
	}
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}

	// Get active channels from lnd node
	channels, err := lncli.ActiveChannels()
	if err != nil {
		return fmt.Errorf("Error %s on getting active channels", err)
	}
	if len(channels) == 0 {
		return fmt.Errorf("Opened channels not found")
	}

	// Distribute amount between channels proportionally to their free capacity
	channelsSelector := NewChannelsSelector(satoshiPrecision)
	channelPayments, err := channelsSelector.Select(amount, channels)
	if err != nil {
		return fmt.Errorf("Error %s on channelsSelector.SelectChannels(%s, %v)", err, amount, channels)
	}
	if len(channelPayments) == 0 {
		return fmt.Errorf("No channels for payment were selected")
	}

	// Request API for invoices for selected channels
	channelPoints := []string{}
	for _, c := range channelPayments {
		channelPoints = append(channelPoints, c.ChannelPoint)
	}
	invoices, err := restcli.IssueInvoices(account, channelPoints)
	if err != nil {
		return fmt.Errorf("Error %s on getting invoices to pay", err)
	}
	invoicesByPoint := map[string]clients.Invoice{}
	for _, i := range invoices {
		invoicesByPoint[i.ChanPoint] = i
	}

	// Send payments
	result := PaymentResult{}
	for _, cp := range channelPayments {
		inv, ok := invoicesByPoint[cp.ChannelPoint]
		if !ok {
			cp.Error = fmt.Sprintf("Invoice for channel %s not found in API response", cp.ChannelPoint)
			result.Errors = append(result.Errors, cp)
			continue
		}

		err := lncli.SendPayment(inv.PaymentRequest, cp.AmountToPay, cp.ID)
		if err != nil {
			err = fmt.Errorf("Error %s on sending payment on %s to %s %s", err, cp.AmountToPay, inv.NodeID, cp.ChannelPoint)
			cp.Error = err.Error()
			result.Errors = append(result.Errors, cp)
		} else {
			result.Successful = append(result.Successful, cp)
		}
	}

	if len(result.Errors) > 0 {
		ResponseError(result)
		os.Exit(1)
	}
	ResponseJSON(result)
	return nil
}
