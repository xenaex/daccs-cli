package commands

import (
	"fmt"
	"strings"

	"github.com/urfave/cli"
	"github.com/xenaex/daccs-cli/clients"
)

// Payment commands definition
var Api = cli.Command{
	Name:    "api",
	Aliases: []string{"a"},
	Usage:   "Interaction with Xena API commands",
	Subcommands: []cli.Command{
		{
			Name:   "nodes",
			Usage:  "List Xena lnd nodes available to open channels with",
			Action: nodesList,
		},
	},
}

// nodesList command handler
func nodesList(c *cli.Context) error {
	restcli, err := clients.NewRestClient(c)
	if err != nil {
		return err
	}
	res, err := restcli.RemoteNodes()
	if err != nil {
		return err
	}
	nodes := make([]*RemoteNode, 0, len(res))
	for _, n := range res {
		addrParts := strings.Split(n.Address, "@")
		if len(addrParts) != 2 {
			return fmt.Errorf("Received from API node address is in invalid format")
		}
		nodes = append(nodes, &RemoteNode{ID: n.ID, PubKey: addrParts[0]})
	}
	ResponseJSON(nodes)
	return nil
}

type RemoteNode struct {
	ID     string `json:"id"`
	PubKey string `json:"pubKey"`
}
