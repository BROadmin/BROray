package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/BROadmin/broray/internal/app"
	"github.com/BROadmin/broray/internal/compat"
	"github.com/BROadmin/broray/internal/installer"
	"github.com/BROadmin/broray/internal/platform"
)

// The CLI uninstall runs after the daemon stopped, so the areas' Keenetic
// cleanup steps (3.x-format receipts) are run here in maintenance mode:
// areas register endpoints, services and steps but start nothing.
func init() {
	installer.AreaCleanup = func(ctx context.Context, root platform.Root) error {
		cfg, err := platform.LoadConfig(root)
		if err != nil {
			cfg = platform.DefaultConfig()
		}
		logger := log.New(io.MultiWriter(os.Stderr), "uninstall: ", log.LstdFlags)
		a := app.New(root, cfg, logger)
		d := compat.NewDeps(ctx, a)
		d.Maintenance = true
		if err := compat.StartAreas(d); err != nil {
			return fmt.Errorf("area setup: %w", err)
		}
		fctx := compat.WithFence(ctx) // the daemon is stopped; nothing else runs
		for _, stage := range compat.UninstallStages {
			for i, step := range compat.UninstallSteps(stage) {
				logger.Printf("area cleanup %s #%d: start", stage, i+1)
				if err := step(fctx); err != nil {
					logger.Printf("area cleanup %s #%d: FAILED: %v", stage, i+1, err)
					return fmt.Errorf("%s: %w", stage, err)
				}
				logger.Printf("area cleanup %s #%d: done", stage, i+1)
			}
		}
		return nil
	}
}
