package commands

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/xenaex/daccs-cli/clients"
)

type testCase struct {
	name        string
	amount      decimal.Decimal
	channels    []*clients.ChannelStatus
	expected    []*ChannelPayment
	expectedErr string
}

func (c testCase) RunSuccess(t *testing.T) {
	selected, err := selector().Select(c.amount, c.channels)
	assert.Nil(t, err)
	assert.Len(t, selected, len(c.expected))

	selectedMap := map[string]*ChannelPayment{}
	for _, ch := range selected {
		selectedMap[ch.ChannelPoint] = ch
	}

	for _, expected := range c.expected {
		actual, ok := selectedMap[expected.ChannelPoint]
		if assert.True(t, ok) {
			assert.Equal(t, expected.ID, actual.ID)
			assert.Equal(t, expected.ChannelPoint, actual.ChannelPoint)
			assert.Equal(t, expected.AmountToPay.String(), actual.AmountToPay.String())
		}
	}
}

func (c testCase) RunError(t *testing.T) {
	selected, err := selector().Select(c.amount, c.channels)
	assert.Equal(t, c.expectedErr, err.Error())
	assert.Nil(t, selected)
}

func TestSelectSuccess(t *testing.T) {
	cases := []*testCase{
		newCase("1Channel_ThresholdAmt", 0.00000001,
			channels(channel(1, "1", 0.00000001)),
			expected(payment(1, "1", 0.00000001)),
		),
		newCase("2Channels_ThresholdAmt_1", 0.00000001,
			channels(channel(1, "1", 0.00999798), channel(2, "2", 0.00999798)),
			expected(payment(1, "1", 0.00000001)),
		),
		newCase("2Channels_ThresholdAmt_2", 0.00000001,
			channels(channel(1, "1", 0.00999797), channel(2, "2", 0.00999798)),
			expected(payment(2, "2", 0.00000001)),
		),
		newCase("2Channels_FillEntirely", 0.00000003,
			channels(channel(1, "1", 0.00000001), channel(2, "2", 0.00000002)),
			expected(payment(1, "1", 0.00000001), payment(2, "2", 0.00000002)),
		),
		newCase("3SameChannels1", 0.001,
			channels(channel(1, "1", 0.009), channel(2, "2", 0.009), channel(3, "3", 0.009)),
			expected(payment(1, "1", 0.00033333), payment(2, "2", 0.00033333), payment(3, "3", 0.00033334)),
		),
		newCase("3SameChannels2", 0.003,
			channels(channel(1, "1", 0.009), channel(2, "2", 0.009), channel(3, "3", 0.009)),
			expected(payment(1, "1", 0.001), payment(2, "2", 0.001), payment(3, "3", 0.001)),
		),
		newCase("3Channels", 0.0009, // 0.0001 + 0.0003 + 0.0005
			channels(channel(1, "1", 0.01), channel(2, "2", 0.03), channel(3, "3", 0.05)),
			expected(payment(1, "1", 0.0001), payment(2, "2", 0.0003), payment(3, "3", 0.0005)),
		),
	}
	for _, c := range cases {
		t.Run(c.name, c.RunSuccess)
	}
}
func TestSelectError(t *testing.T) {
	cases := []*testCase{
		newErrCase("AmountGreaterThanTotalLocalBalance", 0.03000001,
			channels(channel(1, "1", 0.01), channel(1, "1", 0.02)),
			"Open channels total local balance 0.03 is less than amount to pay",
		),
	}
	for _, c := range cases {
		t.Run(c.name, c.RunError)
	}
}

func selector() *ChannelsSelector {
	return NewChannelsSelector(satoshiPrecision)
}

func d(f float64) decimal.Decimal {
	return decimal.NewFromFloat(f)
}

func newCase(name string, amt float64, channels []*clients.ChannelStatus, expected []*ChannelPayment) *testCase {
	return &testCase{
		name:     name,
		amount:   d(amt),
		channels: channels,
		expected: expected,
	}
}

func newErrCase(name string, amt float64, channels []*clients.ChannelStatus, err string) *testCase {
	return &testCase{
		name:        name,
		amount:      d(amt),
		channels:    channels,
		expectedErr: err,
	}
}

func channels(statuses ...*clients.ChannelStatus) []*clients.ChannelStatus {
	res := []*clients.ChannelStatus{}
	for _, s := range statuses {
		res = append(res, s)
	}
	return res
}

func channel(id uint64, point string, local float64) *clients.ChannelStatus {
	return &clients.ChannelStatus{ID: id, ChannelPoint: point, LocalBalance: d(local)}
}

func expected(payments ...*ChannelPayment) []*ChannelPayment {
	res := []*ChannelPayment{}
	for _, s := range payments {
		res = append(res, s)
	}
	return res
}

func payment(id uint64, point string, amt float64) *ChannelPayment {
	return &ChannelPayment{ID: id, ChannelPoint: point, AmountToPay: d(amt)}
}
