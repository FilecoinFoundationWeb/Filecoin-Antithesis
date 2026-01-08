package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/properties/chain"
	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/properties/finalization"
	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/properties/liveness"
	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/properties/state"
	"github.com/FilecoinFoundationWeb/Filecoin-Antithesis/resources"
	"github.com/urfave/cli/v2"
)

const defaultConfigPath = "/opt/antithesis/resources/config.json"

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	app := &cli.App{
		Name:  "workload",
		Usage: "Filecoin node testing workload",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Value:   defaultConfigPath,
				Usage:   "Path to config JSON file",
				EnvVars: []string{"WORKLOAD_CONFIG"},
			},
		},
		Commands: []*cli.Command{
			propertyCommands(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatalf("[ERROR] %v", err)
	}
}

func propertyCommands() *cli.Command {
	return &cli.Command{
		Name:  "property",
		Usage: "Property checking operations",
		Subcommands: []*cli.Command{
			{
				Name:  "check-finalization",
				Usage: "Check finalized tipset consistency across nodes",
				Action: func(c *cli.Context) error {
					return withClientPool(c, func(ctx context.Context, pool *resources.ClientPool) error {
						return finalization.CheckFinalizedTipsetMatch(ctx, pool)
					})
				},
			},
			{
				Name:  "check-height",
				Usage: "Check chain height monotonicity",
				Action: func(c *cli.Context) error {
					return withClientPool(c, func(ctx context.Context, pool *resources.ClientPool) error {
						tracker := chain.NewHeightTracker()
						return tracker.CheckAllNodesMonotonic(ctx, pool)
					})
				},
			},
			{
				Name:  "check-state",
				Usage: "Check state root consistency at finalized heights",
				Action: func(c *cli.Context) error {
					return withClientPool(c, func(ctx context.Context, pool *resources.ClientPool) error {
						return state.CheckStateRootMatch(ctx, pool)
					})
				},
			},
			{
				Name:  "check-liveness",
				Usage: "Check that chain makes progress",
				Flags: []cli.Flag{
					&cli.DurationFlag{
						Name:  "window",
						Value: 60 * time.Second,
						Usage: "Time window to check for progress",
					},
				},
				Action: func(c *cli.Context) error {
					return withClientPool(c, func(ctx context.Context, pool *resources.ClientPool) error {
						return liveness.CheckAnyNodeProgress(ctx, pool, c.Duration("window"))
					})
				},
			},
			{
				Name:  "check-all",
				Usage: "Run all property checks once",
				Action: func(c *cli.Context) error {
					return withClientPool(c, func(ctx context.Context, pool *resources.ClientPool) error {
						log.Println("=== Running all property checks ===")

						log.Println("\n--- Finalization Check ---")
						finalization.CheckFinalizedTipsetMatch(ctx, pool)

						log.Println("\n--- Height Monotonicity Check ---")
						tracker := chain.NewHeightTracker()
						tracker.CheckAllNodesMonotonic(ctx, pool)

						log.Println("\n--- State Root Check ---")
						state.CheckStateRootMatch(ctx, pool)

						log.Println("\n--- Liveness Check ---")
						liveness.CheckAnyNodeProgress(ctx, pool, 30*time.Second)

						log.Println("\n=== All property checks complete ===")
						return nil
					})
				},
			},
			{
				Name:  "monitor",
				Usage: "Continuously monitor properties",
				Flags: []cli.Flag{
					&cli.DurationFlag{
						Name:  "interval",
						Value: 30 * time.Second,
						Usage: "Check interval",
					},
				},
				Action: func(c *cli.Context) error {
					return withClientPool(c, func(ctx context.Context, pool *resources.ClientPool) error {
						interval := c.Duration("interval")
						tracker := chain.NewHeightTracker()

						log.Printf("[monitor] Starting property monitoring with %v interval", interval)

						ticker := time.NewTicker(interval)
						defer ticker.Stop()

						for {
							select {
							case <-ctx.Done():
								return nil
							case <-ticker.C:
								log.Println("[monitor] Running property checks...")
								finalization.CheckFinalizedTipsetMatch(ctx, pool)
								tracker.CheckAllNodesMonotonic(ctx, pool)
								state.CheckStateRootMatch(ctx, pool)
							}
						}
					})
				},
			},
		},
	}
}

// withClientPool loads config, creates client pool, runs action, and cleans up.
func withClientPool(c *cli.Context, action func(context.Context, *resources.ClientPool) error) error {
	configPath := c.String("config")
	cfg, err := resources.LoadConfig(configPath)
	if err != nil {
		return err
	}

	ctx := c.Context
	pool, err := resources.NewClientPool(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()

	return action(ctx, pool)
}
