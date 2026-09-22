package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// configCmd shows the configuration the server started with (R-271): each
// non-secret setting, its value and where it came from, and the host policy
// fields fixed there — the same answer GET /config gives the console.
func configCmd(client func() (*Client, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show the server's startup configuration and where each setting came from",
		Long: "Lists every setting the server started with, its value, and where it was set: an\n" +
			"environment variable, the config file, or the default. Host policy fields set there are\n" +
			"listed separately; they cannot be changed with `pando policy set` while they are set.\n" +
			"Secrets are never shown.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			type source struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
				Key  string `json:"key"`
			}
			var out struct {
				File     string `json:"file"`
				Settings []struct {
					Key    string `json:"key"`
					Value  any    `json:"value"`
					Source source `json:"source"`
					Env    string `json:"env"`
				} `json:"settings"`
				Policy []struct {
					Key    string          `json:"key"`
					Value  json.RawMessage `json:"value"`
					Source source          `json:"source"`
				} `json:"policy"`
			}
			if err := c.Do("GET", "/config", nil, &out); err != nil {
				return err
			}

			from := func(s source) string {
				switch s.Kind {
				case "env":
					return "env " + s.Name
				case "file":
					return "file " + s.Name + " (" + s.Key + ")"
				}
				return "default"
			}

			w := cmd.OutOrStdout()
			if out.File != "" {
				fmt.Fprintf(w, "Config file: %s\n\n", out.File)
			} else {
				fmt.Fprintln(w, "Config file: none")
				fmt.Fprintln(w)
			}
			t := table(w, "SETTING", "VALUE", "FROM")
			for _, s := range out.Settings {
				f := from(s.Source)
				if s.Source.Kind == "default" {
					f = "default (set with " + s.Env + ")"
				}
				fmt.Fprintf(t, "%s\t%v\t%s\n", s.Key, s.Value, f)
			}
			if err := t.Flush(); err != nil {
				return err
			}
			fmt.Fprintln(w)
			fmt.Fprintln(w, "To change one: an env setting is in the environment Pando starts with — with Docker")
			fmt.Fprintln(w, "Compose, under environment: on the pando service in docker-compose.yml, applied with")
			fmt.Fprintln(w, "`docker compose up -d pando` (a plain restart keeps the old environment). A file setting")
			fmt.Fprintln(w, "is in the config file shown; edit it and restart Pando.")
			if len(out.Policy) > 0 {
				fmt.Fprintln(w)
				t = table(w, "FIXED POLICY", "VALUE", "FROM")
				for _, p := range out.Policy {
					fmt.Fprintf(t, "%s\t%s\t%s\n", p.Key, p.Value, from(p.Source))
				}
				return t.Flush()
			}
			return nil
		},
	}
}
