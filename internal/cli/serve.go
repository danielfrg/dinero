package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
	_ "time/tzdata"

	"github.com/danielfrg/dinero/internal/config"
	"github.com/danielfrg/dinero/internal/httpapi"
	"github.com/danielfrg/dinero/internal/ledger"
	"github.com/danielfrg/dinero/internal/mcp"
	"github.com/danielfrg/dinero/internal/server"
	"github.com/danielfrg/dinero/internal/store"
	"github.com/spf13/cobra"
)

func newServeCommand(deps Dependencies, configPath *string) *cobra.Command {
	var host string
	var port int
	var allowedHosts, allowedOrigins []string
	cmd := &cobra.Command{Use: "serve", Short: "Serve the read-only HTTP API", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
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
		if port == 0 && !cmd.Flags().Changed("port") {
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
		policy := httpapi.Options{
			AllowedHosts:   append(cfg.Server.AllowedHosts, allowedHosts...),
			AllowedOrigins: append(cfg.Server.AllowedOrigins, allowedOrigins...),
		}
		handler := server.NewHandler(svc, policy, mcp.NewTransportHandler(svc))
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		listener, err := deps.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("could not listen on %s", addr)
		}
		defer listener.Close()
		boundAddress := listener.Addr().String()
		linkAddress := boundAddress
		if boundHost, boundPort, err := net.SplitHostPort(boundAddress); err == nil {
			if boundHost == "" || boundHost == "0.0.0.0" || boundHost == "::" {
				linkAddress = net.JoinHostPort("localhost", boundPort)
			}
		}
		message := "Dinero API listening at http://" + linkAddress
		if linkAddress != boundAddress {
			message += " (bound to " + boundAddress + ")"
		}
		if _, err := fmt.Fprintln(cmd.ErrOrStderr(), message); err != nil {
			return err
		}
		return server.Serve(cmd.Context(), listener, handler, 10*time.Second)
	}}
	cmd.Flags().StringVar(&host, "host", "", "listener host")
	cmd.Flags().IntVar(&port, "port", 0, "listener port")
	cmd.Flags().StringSliceVar(&allowedHosts, "allowed-host", nil, "additional allowed HTTP hostnames (repeat or comma-separate)")
	cmd.Flags().StringSliceVar(&allowedOrigins, "allowed-origin", nil, "additional allowed browser origins (repeat or comma-separate)")
	return cmd
}
