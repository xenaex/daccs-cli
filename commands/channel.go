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
				cli.StringFlag{Name: "node-id"},
				cli.StringFlag{Name: "node-pubkey"},
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

	// Parse and validate node id or pubkey
	nodeID := c.String("node-id")
	nodePubKey := c.String("node-pubkey")
	if nodeID == "" && nodePubKey == "" {
		return fmt.Errorf("Either node-id or node-pubkey required")
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

	// Get remote nodes and find the provided one
	remoteNodes, err := restcli.RemoteNodes()
	if err != nil {
		return fmt.Errorf("Error %s on getting RemoteNodes", err)
	}
	var remoteNode *clients.Node
	for _, n := range remoteNodes {
		if (nodeID != "" && n.ID == nodeID) || (nodePubKey != "" && strings.Contains(n.Address, nodePubKey)) {
			remoteNode = n
			break
		}
	}
	if remoteNode == nil {
		p := nodeID
		if p == "" {
			p = nodePubKey
		}
		return fmt.Errorf("Unknown remote node %s to open channel with", p)
	}

	// Ensure lnd node registration
	pubKey, err := lncli.NodePubKey()
	if err != nil {
		return fmt.Errorf("Error %s on getting NodePubKey", err)
	}
	err = restcli.RegisterNode(pubKey)
	if err != nil {
		return fmt.Errorf("Error %s on registering node", err)
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

	// Ensure lnd node connection
	connectedTo, err := lncli.Peers()
	if err != nil {
		return fmt.Errorf("Error %s on getting LND connected peers", err)
	}
	connected := false
	for _, p := range connectedTo {
		if strings.Contains(p, remoteNode.Address) {
			connected = true
			break
		}
	}
	if !connected {
		err = lncli.Connect(remoteNode.Address)
		if err != nil {
			return fmt.Errorf("Error %s on connecting to %s", err, remoteNode.Address)
		}
	}

	// Open channel on each connection and aggregate results
	respChan := make(chan *clients.OpenChannelResult)
	defer close(respChan)

	err = lncli.OpenChannel(remoteNode.Address, capacity, respChan)
	if err != nil {
		ResponseError(Error{Error: fmt.Sprintf("Failed to open channel with %s: %s", remoteNode.Address, err)})
		os.Exit(1)
	}
	r := <-respChan
	if r.Error != nil {
		ResponseError(Error{Error: fmt.Sprintf("Failed to open channel with %s: %s", r.Node, r.Error)})
		os.Exit(1)
	}
	ResponseJSON(r.ChannelStatus)
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
