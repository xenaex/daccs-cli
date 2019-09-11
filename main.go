package main

import (
	"os"

	"github.com/urfave/cli"
	"github.com/xenaex/daccs-cli/commands"
)

func main() {
	app := cli.NewApp()
	app.Name = "Xena dAccs client"
	app.Version = "0.1"
	app.Copyright = "Copyright (c) 2019 Xena Financial Systems"
	app.Usage = "control your funds directly while accessing the speed and liquidity inherent to centralized exchanges"
	// Common flags
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "api-url",
			Usage:  "URL of Xena dAccs API",
			Value:  "https://api.xena.exchange/daccs/",
			EnvVar: "XENA_DACCS_API_URL",
		},
		cli.StringFlag{
			Name:   "api-key",
			Usage:  "API Key for Xena dAccs API",
			EnvVar: "XENA_DACCS_API_KEY",
		},
		cli.StringFlag{
			Name:   "api-secret",
			Usage:  "API Secret for Xena dAccs API",
			EnvVar: "XENA_DACCS_API_SECRET",
		},
		cli.StringFlag{
			Name:   "lnd-host",
			Usage:  "Host address (optionally with :port) of local LND node",
			Value:  "127.0.0.1:10009",
			EnvVar: "XENA_DACCS_LND_HOST",
		},
		cli.StringFlag{
			Name:   "lnd-tls-cert",
			Usage:  "Path of TLS certificate for local LND node",
			Value:  "tls.cert",
			EnvVar: "XENA_DACCS_LND_TLS",
		},
		cli.StringFlag{
			Name:   "lnd-macaroon",
			Usage:  "Path of macaroon file for local LND node",
			Value:  "admin.macaroon",
			EnvVar: "XENA_DACCS_LND_MACAROON",
		},
	}

	// Commands
	app.Commands = []cli.Command{
		commands.Channel,
		commands.Node,
		commands.Payment,
		commands.Api,
	}

	err := app.Run(os.Args)
	if err != nil {
		commands.ResponseError(&commands.Error{Error: err.Error()})
		os.Exit(1)
	}
}
