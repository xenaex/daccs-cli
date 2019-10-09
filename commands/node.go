package commands

import (
	"fmt"

	"github.com/shopspring/decimal"
	"github.com/urfave/cli"
	"github.com/xenaex/daccs-cli/clients"
)

// Node commands definition
var Node = cli.Command{
	Name:    "node",
	Aliases: []string{"n"},
	Usage:   "Node manipulation commands",
	Subcommands: []cli.Command{
		{
			Name:   "unlock",
			Usage:  "Unlock local LND node to bring it up and running",
			Action: nodeUnlock,
			Flags: []cli.Flag{
				cli.StringFlag{Name: "password"},
			},
		},
		{
			Name:   "status",
			Usage:  "Get local LND node status",
			Action: nodeStatus,
		},
		{
			Name:   "peers",
			Usage:  "Get local LND node peers",
			Action: nodePeers,
		},
		{
			Name:   "disconnect",
			Usage:  "Disconnect local LND node from dAccs infrastructure",
			Action: nodeDisconnect,
		},
		{
			Name:   "balance",
			Usage:  "Get local LND node balance",
			Action: nodeBalance,
		},
		{
			Name:   "deposit",
			Usage:  "Get local LND node deposit address",
			Action: nodeDeposit,
		},
		{
			Name:   "transactions",
			Usage:  "List local LND wallet transactions",
			Action: transactionList,
			Flags: []cli.Flag{
				cli.IntFlag{Name: "offset", Value: 0},
				cli.IntFlag{Name: "limit", Value: 10},
			},
		},
	},
}

func nodeUnlock(c *cli.Context) error {
	// Show command help if no arguments provided
	if c.NumFlags() == 0 {
		cli.ShowCommandHelp(c, "unlock")
		return nil
	}
	pwd := c.String("password")
	if pwd == "" {
		return fmt.Errorf("Unlock password required")
	}
	lncli, err := clients.NewLndClient(c, false)
	if err != nil {
		return err
	}
	err = lncli.Unlock(pwd)
	if err != nil {
		return fmt.Errorf("Error %s on unlocking LND node", err)
	}
	return nil
}

func nodeStatus(c *cli.Context) error {
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}
	info, err := lncli.Status()
	if err != nil {
		return fmt.Errorf("Error %s on getting LND node status", err)
	}
	ResponseJSON(info)
	return nil
}

func nodePeers(c *cli.Context) error {
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}
	peers, err := lncli.Peers()
	if err != nil {
		return fmt.Errorf("Error %s on getting LND node peers", err)
	}
	ResponseJSON(peers)
	return nil
}

func nodeDisconnect(c *cli.Context) error {
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}
	peers, err := lncli.Peers()
	if err != nil {
		return fmt.Errorf("Error %s on getting LND node peers", err)
	}
	for _, p := range peers {
		err = lncli.Disconnect(p)
		if err != nil {
			return fmt.Errorf("Error %s on disconnecting from %s", err, p)
		}
	}

	return nil
}

func nodeBalance(c *cli.Context) error {
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}
	bal, err := lncli.Balance()
	if err != nil {
		return fmt.Errorf("Error %s on getting LND node balance", err)
	}
	resp := struct {
		Balance decimal.Decimal `json:"balance"`
	}{bal}
	ResponseJSON(resp)
	return nil
}

func nodeDeposit(c *cli.Context) error {
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}
	addr, err := lncli.FundingAddress()
	if err != nil {
		return fmt.Errorf("Error %s on getting LND node balance", err)
	}
	resp := struct {
		Address string `json:"address"`
	}{addr}
	ResponseJSON(resp)
	return nil
}

// transactionList command handler
func transactionList(c *cli.Context) error {
	lncli, err := clients.NewLndClient(c, true)
	if err != nil {
		return err
	}
	res, err := lncli.Transactions(c.Int("offset"), c.Int("limit"))
	if err != nil {
		return err
	}
	ResponseJSON(res)
	return nil
}
