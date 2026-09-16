package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/0xProgress/webhookd/config"
	"github.com/0xProgress/webhookd/output"
	"github.com/0xProgress/webhookd/providers"
	_ "github.com/0xProgress/webhookd/providers/mock"
	"github.com/0xProgress/webhookd/server"
)

// shutdownGrace is the maximum time the server waits for in-flight
// requests to complete after a SIGINT or SIGTERM. Beyond it, Shutdown
// returns with an error and the process exits anyway; the HTTP
// listener is already closed by then, so nothing new arrives.
const shutdownGrace = 5 * time.Second

var mockCmd = &cobra.Command{
	Use:   "mock",
	Short: "Run the mock provider (reference implementation, not for production)",
	Long: `Run a webhook receiver that accepts any request carrying the header
X-Mock-Signature: valid and rejects everything else.

The mock provider has no secret and does not compute a MAC. It exists so
contributors can see the full pipeline end to end and so the server test
suite has a provider to exercise.`,
	Args: cobra.NoArgs,
	RunE: runMock,
}

func init() {
	rootCmd.AddCommand(mockCmd)
}

func runMock(cmd *cobra.Command, _ []string) error {
	cfg, err := config.Load(cmd, "mock")
	if err != nil {
		return err
	}

	// Fail fast if the blank import above did not actually register the
	// provider. Without this check the binary would start, accept
	// requests, and return 404 "unknown provider" for every one — a
	// confusing failure mode for a mistake whose cause is a missing
	// import in this file.
	if _, ok := providers.Get(cfg.ProviderName); !ok {
		return fmt.Errorf("%s: provider not registered", cfg.ProviderName)
	}

	var out server.EventWriter
	if cfg.Pretty {
		out = output.NewPrettyWriter(cmd.OutOrStdout())
	} else {
		out = output.NewWriter(cmd.OutOrStdout())
	}

	handler := server.NewHandler(
		cfg.ProviderName,
		out,
		cmd.ErrOrStderr(),
		cfg.MaxBody,
	)

	srv := server.New(server.Options{
		Host:    cfg.Host,
		Port:    cfg.Port,
		Path:    cfg.Path,
		Version: buildVersion,
		Timeout: cfg.Timeout,
		Handler: handler,
		ErrOut:  cmd.ErrOrStderr(),
	})

	// Catch SIGINT (Ctrl-C) and SIGTERM. On either signal the context
	// is canceled and the select below takes the shutdown branch.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ListenAndServe runs in a goroutine so the main flow can select on
	// both the signal context and the serve error. The channel is
	// buffered so the goroutine can always send, even if the main flow
	// has already moved on through the shutdown branch.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		// ListenAndServe returned on its own. ErrServerClosed means
		// Shutdown was called and is a clean exit, not an error.
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err

	case <-ctx.Done():
		// A signal arrived. Stop accepting, let in-flight requests
		// finish, and return Shutdown's result.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
