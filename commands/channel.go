package commands

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/shopspring/decimal"
	"github.com/urfave/cli"
	"github.com/xenaex/daccs-cli/clients"
)

const (
	channelFundingPrecision = int32(3)
	satoshiPrecision        = int32(8)
)

// Channel commands definition
var Channel = cli.Command{
	Name:    "channel",
	Aliases: []string{"c"},
	Usage:   "Channel manipulation commands",
	Subcommands: []cli.Command{
		{
			Name:   "list",
			Usage:  "List available channels",
			Action: channelList,
		},
		{
			Name:   "open",
			Usage:  "Open a new channel with specified capacity",
			Action: channelOpen,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "capacity"},
			},
		},
		{
			Name:   "close",
			Usage:  "Close an existing channel identified by id or channel-point",
			Action: channelClose,
			Flags: []cli.Flag{
				cli.Uint64Flag{Name: "id"},
				cli.StringFlag{Name: "channel-point"},
			},
		},
	},
}

// channelList command handler
func channelList(c *cli.Context) error {
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}
	list, err := lncli.Channels()
	if err != nil {
		return fmt.Errorf("Error %s on getting channels list", err)
	}
	ResponseJSON(list)
	return nil
}

// channelOpen command handler
func channelOpen(c *cli.Context) error {
	// Show command help if no arguments provided
	if c.NumFlags() == 0 {
		cli.ShowCommandHelp(c, "open")
		return nil
	}

	// Parse capacity
	capacity, err := decimal.NewFromString(c.String("capacity"))
	if err != nil {
		return fmt.Errorf("Invalid capacity value")
	}

	restcli, err := clients.NewRestClient(c)
	if err != nil {
		return err
	}
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}

	// Get limits from API and validate capacity
	limits, err := restcli.Limits()
	if err != nil {
		return fmt.Errorf("Error %s on getting Limits", err)
	}
	if capacity.LessThan(limits.MinChannelCapacity) {
		return fmt.Errorf("Capacity should be greater than or equal %s", limits.MinChannelCapacity)
	}

	// Ensure lnd node registration & obtain endpoints to connect to
	pubKey, err := lncli.NodePubKey()
	if err != nil {
		return fmt.Errorf("Error %s on getting NodePubKey", err)
	}
	err = restcli.RegisterNode(pubKey)
	if err != nil {
		return fmt.Errorf("Error %s on registering node", err)
	}
	addrs, err := restcli.RemoteAddresses()
	if err != nil {
		return fmt.Errorf("Error %s on getting RemoteAddresses", err)
	}

	// Ensure local node balance
	nodeBalance, err := lncli.Balance()
	if err != nil {
		return fmt.Errorf("Error %s on getting node balance", err)
	}
	if nodeBalance.LessThan(capacity) {
		// Get address for deposit
		addr, err := lncli.FundingAddress()
		if err != nil {
			return fmt.Errorf("Error %s on getting LND wallet deposit address", err)
		}
		return fmt.Errorf("Insufficient LND wallet funds (%s) to open channel for %s. Please deposit at least %s to %s",
			nodeBalance, capacity, capacity.Sub(nodeBalance), addr)
	}

	// Ensure lnd node connections
	connectedTo, err := lncli.Peers()
	if err != nil {
		return fmt.Errorf("Error %s on getting LND connected peers", err)
	}
	for _, a := range addrs {
		connected := false
		for _, p := range connectedTo {
			if strings.Contains(p, a) {
				connected = true
				break
			}
		}
		if !connected {
			err = lncli.Connect(a)
			if err != nil {
				return fmt.Errorf("Error %s on connecting to %s", err, a)
			}
		}
	}

	// Calculate distribution of funds among channels
	channelFunds := make([]decimal.Decimal, len(addrs))
	fundsTotal := decimal.Zero
	fundsPerChannel, _ := capacity.QuoRem(decimal.New(int64(len(addrs)), 0), channelFundingPrecision)
	for i := range addrs {
		channelFunds[i] = fundsPerChannel
		fundsTotal = fundsTotal.Add(fundsPerChannel)
	}
	if fundsTotal.LessThan(capacity) {
		channelFunds[len(channelFunds)-1] = channelFunds[len(channelFunds)-1].Add(capacity.Sub(fundsTotal))
	}

	// Open channel on each connection and aggregate results
	resp := struct {
		PendingChannels []clients.ChannelStatus `json:"pending_channels,omitempty"`
		Errors          []Error                 `json:"errors,omitempty"`
	}{}
	respChan := make(chan *clients.OpenChannelResult)
	defer close(respChan)
	wg := sync.WaitGroup{}
	for i, a := range addrs {
		err := lncli.OpenChannel(a, channelFunds[i], respChan)
		if err != nil {
			resp.Errors = append(resp.Errors,
				Error{Error: fmt.Sprintf("Failed to open channel with %s: %s", a, err)})
			continue
		}
		wg.Add(1)
	}
	go func() {
		for r := range respChan {
			if r.Error != nil {
				resp.Errors = append(resp.Errors,
					Error{Error: fmt.Sprintf("Failed to open channel with %s: %s", r.Node, r.Error)})
			} else {
				resp.PendingChannels = append(resp.PendingChannels, r.ChannelStatus)
			}
			wg.Done()
		}
	}()
	wg.Wait()
	ResponseJSON(resp)
	return nil
}

// channelClose command handler
func channelClose(c *cli.Context) error {
	// Show command help if no arguments provided
	if c.NumFlags() == 0 {
		cli.ShowCommandHelp(c, "close")
		return nil
	}

	// Parse and validate id or channel point
	chanID := c.Uint64("id")
	chanPoint := c.String("channel-point")
	if chanID == 0 && chanPoint == "" {
		return fmt.Errorf("Either id or channel-point required")
	}

	// Close channel
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}
	cs, err := lncli.CloseChannel(chanID, chanPoint)
	if err != nil {
		cid := chanPoint
		if cid == "" {
			cid = strconv.FormatUint(chanID, 10)
		}
		return fmt.Errorf("Error %s on closing channel %s", err, cid)
	}
	ResponseJSON(cs)
	return nil
}
