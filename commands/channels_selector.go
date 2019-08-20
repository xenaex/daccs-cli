package commands

import (
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
	"github.com/xenaex/daccs-cli/clients"
)

// ChannelsSelector selects channels with exact amounts to pay
type ChannelsSelector struct {
	roundPrecision int32
}

// NewChannelsSelector returns new channels selector
func NewChannelsSelector(roundPrecision int32) *ChannelsSelector {
	return &ChannelsSelector{roundPrecision: roundPrecision}
}

// Select steps:
// 1. Sorts channels ascending by local balance.
// 2. Selects channels (less local balance first) with proportional (to total local balance) amounts to pay.
//    * Amounts are rounded to roundPrecision field.
// 3. Last channel will have left amount.
func (s *ChannelsSelector) Select(amount decimal.Decimal, channels []*clients.ChannelStatus) ([]*ChannelPayment, error) {
	// Calc open channels total local balance
	totalLocal := decimal.Zero
	for _, c := range channels {
		totalLocal = totalLocal.Add(c.LocalBalance)
	}
	if amount.GreaterThan(totalLocal) {
		return nil, fmt.Errorf("Open channels total local balance %s is less than amount to pay", totalLocal)
	}

	// Sort channels by local balance (less first) in order to add the rest to a channel with highest balance
	sort.Slice(channels, func(i, j int) bool {
		return channels[i].LocalBalance.LessThan(channels[j].LocalBalance)
	})

	// TODO: consider channels close amount threshold from API.
	channelPayments := make([]*ChannelPayment, 0, len(channels))
	amountLeft := amount
	for i, c := range channels {
		if amountLeft.Equal(decimal.Zero) {
			break
		}
		channelPayment := ChannelPayment{
			ID:           c.ID,
			ChannelPoint: c.ChannelPoint,
		}
		if i < len(channels)-1 {
			amountToPay := amount.Mul(c.LocalBalance.Div(totalLocal)).Round(satoshiPrecision)
			// Exclude channels with amount to pay above threshold
			if amountToPay.LessThan(minChannelPayment) {
				fmt.Printf("Channel %s amount to pay %s is less than threshold %s, excluding from payment",
					c.ChannelPoint, amountToPay, minChannelPayment)
				fmt.Println()
				continue
			}
			channelPayment.AmountToPay = amountToPay
			amountLeft = amountLeft.Sub(amountToPay)
		} else {
			// Last channel with highest local balance
			if amountLeft.GreaterThan(c.LocalBalance) {
				return nil, fmt.Errorf("Last channel balance %s is less than left amount to pay %s", c.LocalBalance, amountLeft)
			}
			channelPayment.AmountToPay = amountLeft
		}
		channelPayments = append(channelPayments, &channelPayment)
	}
	return channelPayments, nil
}
