package commands

import (
	"fmt"
	"os"
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

	restcli, err := clients.NewRestClient(c)
	if err != nil {
		return err
	}
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}

	// Get min payment amount from API
	limits, err := restcli.Limits()
	if err != nil {
		return fmt.Errorf("Error %s on getting Limits", err)
	}
	if amount.LessThan(limits.MinPaymentAmount) {
		return fmt.Errorf("Amount should be greater or equal to min payment amount %s", limits.MinPaymentAmount)
	}

	// Get active channels from lnd node and filter with known remote nodes
	allChannels, err := lncli.ActiveChannels()
	if err != nil {
		return fmt.Errorf("Error %s on getting active channels", err)
	}
	addrs, err := restcli.RemoteAddresses()
	if err != nil {
		return fmt.Errorf("Error %s on getting RemoteAddresses", err)
	}
	channels := make([]*clients.ChannelStatus, 0, len(allChannels))
	for _, c := range allChannels {
		for _, a := range addrs {
			if strings.Contains(a, c.Node) {
				channels = append(channels, c)
				break
			}
		}
	}
	if len(channels) == 0 {
		return fmt.Errorf("Opened channels with known remote nodes not found")
	}

	// Distribute amount between channels proportionally to their free capacity
	channelsSelector := NewChannelsSelector(limits.MinPaymentAmount, satoshiPrecision)
	channelPayments, err := channelsSelector.FundPayment(amount, channels)
	if err != nil {
		fundErr := FundChannelsError{
			ErrorMessage:     err.Error(),
			MinPaymentAmount: limits.MinPaymentAmount.String(),
			FundingChannels:  channels,
		}
		ResponseError(fundErr)
		os.Exit(1)
	}
	if len(channelPayments) == 0 {
		return fmt.Errorf("No channels for payment were funded")
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

		err := lncli.SendPayment(inv.PaymentRequest, cp.Amount, cp.ID)
		if err != nil {
			err = fmt.Errorf("Error %s on sending payment on %s to %s %s", err, cp.Amount, inv.NodeID, cp.ChannelPoint)
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
