package cmd

import (
	"context"
	"errors"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/RandomCodeSpace/ctm/internal/serve"
)

var servePort int

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().IntVar(&servePort, "port", serve.DefaultPort,
		"Loopback port to bind")
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run the ctm web UI HTTP daemon on 127.0.0.1:37778",
	Long: `ctm serve runs a long-lived HTTP daemon that powers the web UI.

Normally this is auto-spawned by ctm attach / ctm new / ctm yolo and you
do not need to run it manually. Bound to loopback only.

Single-instance: if another ctm serve already owns the port, this
command exits silently with status 0. If a non-ctm-serve process owns
the port, it exits non-zero without disturbing the foreign listener.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(context.Background(),
			syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		srv, err := serve.New(serve.Options{
			Port:    servePort,
			Version: Version,
		})
		if err != nil {
			if errors.Is(err, serve.ErrAlreadyRunning) {
				// Another ctm serve owns the port — silent success.
				return nil
			}
			return err
		}

		return srv.Run(ctx)
	},
}
