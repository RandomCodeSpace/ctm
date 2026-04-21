package cmd

import (
	"context"
	"errors"
	"log/slog"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/serve"
	"github.com/RandomCodeSpace/ctm/internal/serve/attention"
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

		// Load config for Serve sub-struct (webhook URL/auth + attention
		// thresholds). A failure here is non-fatal — fall back to zero
		// values so the daemon still starts with built-in defaults.
		cfg, cfgErr := config.Load(config.ConfigPath())
		if cfgErr != nil {
			slog.Warn("serve: config load failed, using defaults", "err", cfgErr)
		}

		opts := serve.Options{
			Port:        servePort,
			Version:     Version,
			WebhookURL:  cfg.Serve.WebhookURL,
			WebhookAuth: cfg.Serve.WebhookAuth,
			AttentionThresholds: attentionThresholdsFrom(cfg.Serve.Attention),
			// Thread the loaded config through so /api/doctor can
			// surface required_env / required_in_path without re-
			// reading from disk inside the handler.
			Config: cfg,
		}
		// Let the config override the dump dir when set; otherwise
		// serve.New falls back to /tmp/ctm-statusline.
		if cfg.Serve.StatuslineDumpDir != "" {
			opts.StatuslineDumpDir = cfg.Serve.StatuslineDumpDir
		}

		srv, err := serve.New(opts)
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

// attentionThresholdsFrom maps the config-layer thresholds (with zero
// fall-back to defaults via Resolved()) into the attention package's
// own type, keeping cmd/ as the integration seam.
func attentionThresholdsFrom(c config.AttentionThresholds) attention.Thresholds {
	r := c.Resolved()
	return attention.Thresholds{
		ErrorRatePct:         r.ErrorRatePct,
		ErrorRateWindow:      r.ErrorRateWindow,
		IdleMinutes:          r.IdleMinutes,
		QuotaPct:             r.QuotaPct,
		ContextPct:           r.ContextPct,
		YoloUncheckedMinutes: r.YoloUncheckedMinutes,
	}
}
