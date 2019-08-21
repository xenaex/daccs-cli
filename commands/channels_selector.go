package commands

import (
	"fmt"
	"sort"

	"github.com/shopspring/decimal"
	"github.com/xenaex/daccs-cli/clients"
)

// ChannelsSelector selects channels with exact amounts to pay
type ChannelsSelector struct {
	minPaymentAmount       decimal.Decimal
	doubleMinPaymentAmount decimal.Decimal
	roundPrecision         int32
}

// NewChannelsSelector returns new channels selector
func NewChannelsSelector(minPaymentAmount decimal.Decimal, roundPrecision int32) *ChannelsSelector {
	return &ChannelsSelector{
		minPaymentAmount:       minPaymentAmount,
		doubleMinPaymentAmount: minPaymentAmount.Mul(decimal.New(2, 0)),
		roundPrecision:         roundPrecision,
	}
}

// FundPayment funds payment between open active channels with LocalBalance >= minPaymentAmount.
//
// 1. Removes from funding channels with LocalBalance < minPaymentAmount.
// 2. Sorts channels ascending by LocalBalance.
// 3. Funds channels (less LocalBalance first) with proportional (LocalBalance/totalLocal) amounts to pay.
//    * Amounts are rounded to roundPrecision field.
//	  * Proportional amounts < minPaymentAmount are set to minPaymentAmount.
//	  * If amountLeft < 2 * minPaymentAmount tries to find last channel to fund amountLeft. Otherwise returns error.
// 	  * Last channel will have the rest amount.
//	  * If somehow amountLeft > last channel LocalBalance returns error.
func (s *ChannelsSelector) FundPayment(amount decimal.Decimal, channels []*clients.ChannelStatus) ([]*ChannelPayment, error) {
	// Filter channels and calc total local balance
	filteredChannels := make([]*clients.ChannelStatus, 0, len(channels))
	totalLocal := decimal.Zero
	for _, c := range channels {
		if c.LocalBalance.LessThan(s.minPaymentAmount) {
			continue
		}
		filteredChannels = append(filteredChannels, c)
		totalLocal = totalLocal.Add(c.LocalBalance)
	}
	if amount.GreaterThan(totalLocal) {
		return nil, fmt.Errorf("Open channels total local balance %s is less than amount to pay", totalLocal)
	}

	// Sort channels by local balance (less first) in order to add the rest to a channel with highest balance
	sort.Slice(filteredChannels, func(i, j int) bool {
		return filteredChannels[i].LocalBalance.LessThan(filteredChannels[j].LocalBalance)
	})

	// TODO: consider channels close amount threshold from API.
	channelPayments := make([]*ChannelPayment, 0, len(filteredChannels))
	amountLeft := amount
	for i, c := range filteredChannels {
		if amountLeft.Equal(decimal.Zero) {
			break
		}
		if amountLeft.LessThan(s.doubleMinPaymentAmount) {
			// Left amount < 2 * minPaymentAmount
			// Should find the last channel with LocalBalance >= amountLeft and finish funding
			found := false
			for j := i; j < len(filteredChannels); j++ {
				c = filteredChannels[j]
				if amountLeft.GreaterThan(c.LocalBalance) {
					continue
				}
				channelPayments = append(channelPayments, &ChannelPayment{
					ID:           c.ID,
					ChannelPoint: c.ChannelPoint,
					Node:         c.Node,
					Amount:       amountLeft,
				})
				found = true
				break
			}
			if !found {
				return nil, fmt.Errorf("Unable to find last channel to distribute rest amount %s", amountLeft)
			}
			break
		}
		payment := ChannelPayment{
			ID:           c.ID,
			ChannelPoint: c.ChannelPoint,
			Node:         c.Node,
		}
		if i < len(filteredChannels)-1 {
			// Non-last channels
			payment.Amount = amount.Mul(c.LocalBalance.Div(totalLocal)).Round(satoshiPrecision)
			if payment.Amount.LessThan(s.minPaymentAmount) {
				payment.Amount = s.minPaymentAmount
			}
			amountLeft = amountLeft.Sub(payment.Amount)
		} else {
			// Last channel with highest local balance
			if amountLeft.GreaterThan(c.LocalBalance) {
				return nil, fmt.Errorf("Last channel LocalBalance %s is less than left amount %s", c.LocalBalance, amountLeft)
			}
			payment.Amount = amountLeft
		}
		channelPayments = append(channelPayments, &payment)
	}
	return channelPayments, nil
}
