package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"time"
	_ "time/tzdata"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/httpapi"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielfrg/dinero/internal/server"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/spf13/cobra"
)

func newServeCommand(deps Dependencies, configPath *string) *cobra.Command {
	var host string
	var port int
	cmd := &cobra.Command{Use: "serve", Short: "Serve the read-only HTTP API", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		token, err := httpapi.ReadTokenFile(os.Getenv("DINERO_API_TOKEN_FILE"))
		if err != nil {
			return err
		}
		cfg, err := deps.LoadConfig(config.ResolvePath(*configPath))
		if err != nil {
			return errors.New("could not load configuration")
		}
		if !cmd.Flags().Changed("host") {
			host = cfg.Server.Host
		}
		if host == "" {
			host = "127.0.0.1"
		}
		if !cmd.Flags().Changed("port") {
			port = cfg.Server.Port
		}
		if port == 0 {
			port = 8080
		}
		if port < 1 || port > 65535 {
			return errors.New("port must be 1 through 65535")
		}
		dsn, err := cfg.DSN()
		if err != nil {
			return errors.New("invalid database configuration")
		}
		openCtx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
		defer cancel()
		db, err := store.Open(openCtx, dsn, true, cfg.Timezone)
		if err != nil {
			return errors.New("data source unavailable")
		}
		defer db.Close()
		svc := ledger.NewService(db, cfg)
		handler := server.NewHandler(svc, httpapi.Options{Token: token, AllowedHosts: cfg.Server.AllowedHosts, AllowedOrigins: cfg.Server.AllowedOrigins}, nil)
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		listener, err := deps.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("could not listen on %s", addr)
		}
		return server.Serve(cmd.Context(), listener, handler, 10*time.Second)
	}}
	cmd.Flags().StringVar(&host, "host", "", "listener host")
	cmd.Flags().IntVar(&port, "port", 0, "listener port")
	return cmd
}
