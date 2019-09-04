package commands

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
				cli.Uint64Flag{Name: "channel-id"},
				cli.StringFlag{Name: "channel-point"},
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
	account := c.Int64("account")
	if account <= 0 {
		return fmt.Errorf("Invalid account")
	}
	amount, err := decimal.NewFromString(c.String("amount"))
	if err != nil {
		return fmt.Errorf("Invalid amount value")
	}
	// Parse channel
	chanID := c.Uint64("channel-id")
	chanPoint := c.String("channel-point")
	if chanID == 0 && chanPoint == "" {
		return fmt.Errorf("Either channel-id or channel-point required")
	}

	// Get clients
	restcli, err := clients.NewRestClient(c)
	if err != nil {
		return err
	}
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}

	// Find and validate a channel to pay to
	channels, err := lncli.ActiveChannels()
	if err != nil {
		return fmt.Errorf("Error %s on getting active channels", err)
	}
	var channel *clients.ChannelStatus
	for _, c := range channels {
		if (chanID != 0 && c.ID == chanID) || (chanPoint != "" && c.ChannelPoint == chanPoint) {
			channel = c
			break
		}
	}
	if channel == nil {
		p := chanPoint
		if p == "" {
			p = strconv.FormatUint(chanID, 10)
		}
		return fmt.Errorf("Channel %s not found", p)
	}
	// Check if it's a channel with Xena lnd node
	addrs, err := restcli.RemoteAddresses()
	if err != nil {
		return fmt.Errorf("Error %s on getting RemoteAddresses", err)
	}
	found := false
	for _, a := range addrs {
		if strings.Contains(a, channel.Node) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("Specified channel should be an open active channel with Xena lnd node")
	}

	// Get limits from API
	limits, err := restcli.Limits()
	if err != nil {
		return fmt.Errorf("Error %s on getting Limits", err)
	}
	if amount.LessThan(limits.MinPaymentAmount) {
		return fmt.Errorf("Amount should be greater or equal to min payment amount %s", limits.MinPaymentAmount)
	}
	// Check channel's local balance
	reserved := channel.LocalReserved.Mul(limits.ChannelReserveMultiplier)
	maxPaymentAmount := channel.LocalBalance.Sub(reserved)
	if amount.GreaterThan(maxPaymentAmount) {
		return fmt.Errorf("Amount %s is greater than (local_balance %s - reserved %s) = %s",
			amount, channel.LocalBalance, reserved, maxPaymentAmount)
	}

	// Request API for invoice for specified channel
	invoices, err := restcli.IssueInvoices(account, []string{channel.ChannelPoint})
	if err != nil {
		return fmt.Errorf("Error %s on getting invoices to pay", err)
	}
	if len(invoices) == 0 {
		return fmt.Errorf("No invoices were returned from IssueInvoices")
	}
	inv := invoices[0]

	// Send payment
	err = lncli.SendPayment(inv.PaymentRequest, amount, channel.ID)
	if err != nil {
		fmt.Println(fmt.Sprintf("%#v", err))
		msg := fmt.Sprintf("Error %s on sending payment on %s to %s %s", err, amount, inv.NodeID, channel.ChannelPoint)
		ResponseError(Error{Error: msg})
		os.Exit(1)
	}
	return nil
}
