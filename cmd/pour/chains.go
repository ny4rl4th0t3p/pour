package main

import (
	"fmt"

	"github.com/ny4rl4th0t3p/pour/internal/config"
)

// ChainsCmd groups chain management subcommands.
type ChainsCmd struct {
	Validate ChainsValidateCmd `cmd:"" help:"Validate a chains.yml file offline."`
}

// ChainsValidateCmd validates a chains.yml file offline.
type ChainsValidateCmd struct {
	Config string `arg:"" type:"path" help:"Path to chains.yml."`
}

func (c *ChainsValidateCmd) Run() error {
	cfg, err := config.LoadChains(c.Config)
	if err != nil {
		return err
	}
	if _, err := cfg.ToOverrideSet(); err != nil {
		return fmt.Errorf("override set: %w", err)
	}
	if _, err := cfg.ToStandaloneInfos(); err != nil {
		return fmt.Errorf("standalone chains: %w", err)
	}
	fmt.Println("OK")
	return nil
}
