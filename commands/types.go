package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/shopspring/decimal"
	"github.com/xenaex/daccs-cli/clients"
)

// Error descriptor
type Error struct {
	Error string `json:"error"`
}

// ChannelPayment is a single channel payment within a "payment send" command
type ChannelPayment struct {
	ID           uint64          `json:"id"`
	ChannelPoint string          `json:"channel_point"`
	Node         string          `json:"node"`
	Amount       decimal.Decimal `json:"amount"`
	Error        string          `json:"error,omitempty"`
}

// PaymentResult contains successful and error channel payments within a "payment send" command
type PaymentResult struct {
	Successful []*ChannelPayment `json:"successful"`
	Errors     []*ChannelPayment `json:"errors"`
}

// FundChannelsError contains details about channels funding error
type FundChannelsError struct {
	ErrorMessage     string                   `json:"errorMessage"`
	MinPaymentAmount string                   `json:"minPaymentAmount"`
	FundingChannels  []*clients.ChannelStatus `json:"fundingChannels"`
}

// ResponseJSON formatter
func ResponseJSON(res interface{}) {
	data, err := json.Marshal(res)
	if err != nil {
		ResponseError(err)
		return
	}
	buf := bytes.Buffer{}
	json.Indent(&buf, data, "", "\t")
	buf.WriteString("\n")
	buf.WriteTo(os.Stdout)
}

// ResponseError error handler
func ResponseError(res interface{}) {
	data, e := json.Marshal(res)
	if e != nil {
		fmt.Fprintf(os.Stderr, "%v (%v)\n", e, res)
		return
	}
	buf := bytes.Buffer{}
	json.Indent(&buf, data, "", "\t")
	buf.WriteString("\n")
	buf.WriteTo(os.Stderr)
}
