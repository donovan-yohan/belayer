package cli

import (
	"fmt"
	"net/http"

	"github.com/donovan-yohan/belayer/internal/dashboard"
	"github.com/spf13/cobra"
)

func newDashboardCmd() *cobra.Command {
	var port, configPath string

	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Start the unified Belayer dashboard",
		Long: `Starts a web dashboard that aggregates multiple Belayer daemons into a single UI.

The dashboard serves the embedded web assets at /ui/ and reverse-proxies API calls
to configured daemon backends via /api/daemons/{name}/...

Configuration is loaded from a YAML file:

  daemons:
    - name: extend-api
      url: http://localhost:7523
      token: ***
    - name: relay-ide
      url: http://localhost:7524
      token: ***
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				return fmt.Errorf("--config is required (path to dashboard YAML config)")
			}
			daemons, err := dashboard.LoadConfig(configPath)
			if err != nil {
				return err
			}
			if len(daemons) == 0 {
				return fmt.Errorf("config file %q contains no daemons", configPath)
			}

			srv, err := dashboard.NewServer(daemons)
			if err != nil {
				return err
			}

			addr := ":" + port
			if port == "" {
				addr = ":7525"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "belayer dashboard listening on %s\n", addr)
			fmt.Fprintf(cmd.OutOrStdout(), "configured daemons:\n")
			for _, d := range daemons {
				fmt.Fprintf(cmd.OutOrStdout(), "  - %s -> %s\n", d.Name, d.URL)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "open http://localhost%s/ui/ in your browser\n", addr)

			return http.ListenAndServe(addr, srv.Handler())
		},
	}

	cmd.Flags().StringVarP(&port, "port", "p", "7525", "TCP port to listen on")
	cmd.Flags().StringVar(&configPath, "config", "", "Path to dashboard YAML config file (required)")
	return cmd
}
