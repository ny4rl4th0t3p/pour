package main

import (
	"fmt"

	"github.com/alecthomas/kong"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type CLI struct {
	Serve   ServeCmd   `cmd:"" help:"Start the faucet daemon."`
	Keys    KeysCmd    `cmd:"" help:"Key management."`
	Version VersionCmd `cmd:"" help:"Print version information."`
}

type VersionCmd struct{}

func (*VersionCmd) Run() error {
	fmt.Printf("pour %s (commit: %s, built: %s)\n", version, commit, date)
	return nil
}

type ServeCmd struct{}

func (*ServeCmd) Run() error {
	return fmt.Errorf("not implemented — coming in M6/M10")
}

type KeysCmd struct {
	Generate KeysGenerateCmd `cmd:"" help:"Generate a new BIP39 mnemonic."`
	Show     KeysShowCmd     `cmd:"" help:"Show derived addresses for a chain."`
}

type KeysGenerateCmd struct{}

func (*KeysGenerateCmd) Run() error {
	return fmt.Errorf("not implemented — coming in M2/M10")
}

type KeysShowCmd struct {
	Chain string `arg:"" required:"" help:"Chain ID to derive addresses for."`
}

func (*KeysShowCmd) Run() error {
	return fmt.Errorf("not implemented — coming in M2/M10")
}

func main() {
	cli := CLI{}
	ctx := kong.Parse(&cli,
		kong.Name("pour"),
		kong.Description("A pure-Go, multi-chain Cosmos SDK faucet."),
		kong.UsageOnError(),
	)
	ctx.FatalIfErrorf(ctx.Run())
}
