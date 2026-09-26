package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// aiCmd is the AI functions: which adapter handles each (R-259), and the
// administrative ones themselves (R-343 … R-346). Every subcommand is one
// endpoint (R-261).
func aiCmd(client func() (*Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ai",
		Short: "Assign AI functions to adapters, and ask the AI functions",
		Long: "Each AI function is handled by at most one AI adapter, optionally on a model of its own.\n" +
			"A function with no adapter is off. The draft commands propose and never apply: create\n" +
			"what they draft with pando group and pando policy, or in the console.",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "functions",
		Short: "List each AI function, the adapter that handles it, and on which model",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Functions []struct {
					Function       string `json:"function"`
					AdapterID      string `json:"adapter_id"`
					EffectiveModel string `json:"effective_model"`
					On             bool   `json:"on"`
					Off            string `json:"off"`
					Source         *struct {
						Kind string `json:"kind"`
						Key  string `json:"key"`
					} `json:"source"`
				} `json:"functions"`
			}
			if err := c.Do("GET", "/ai/functions", nil, &out); err != nil {
				return err
			}
			t := table(cmd.OutOrStdout(), "FUNCTION", "ADAPTER", "MODEL", "STATE", "SET IN")
			for _, f := range out.Functions {
				state, where := "on", "console"
				if !f.On {
					state = "off"
				}
				if f.Source == nil {
					where = "-"
				} else if f.Source.Kind == "file" {
					where = f.Source.Key
				}
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n", f.Function, dash(f.AdapterID), dash(f.EffectiveModel), state, where)
			}
			return t.Flush()
		},
	})

	var model string
	assign := &cobra.Command{
		Use:   "assign <function> <adapter>",
		Short: "Have an AI adapter handle a function, optionally on its own model",
		Long: "Refused while another adapter handles the function: unassign it there first.\n" +
			"--model needs an adapter that can choose its model.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out map[string]any
			body := map[string]string{"adapter_id": args[1], "model": model}
			if err := c.Do("PUT", "/ai/functions/"+url.PathEscape(args[0]), body, &out); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is now handled by %s.\n", args[0], args[1])
			return nil
		},
	}
	assign.Flags().StringVar(&model, "model", "", "a model for this function only; empty uses the adapter's own")
	cmd.AddCommand(assign)

	cmd.AddCommand(&cobra.Command{
		Use:   "unassign <function>",
		Short: "Turn an AI function off",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			if err := c.Do("DELETE", "/ai/functions/"+url.PathEscape(args[0]), nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is off.\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "ask <question>",
		Short: "Ask how to do something, answered from the API, CLI and MCP reference",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Answer string   `json:"answer"`
				Cites  []string `json:"cites"`
			}
			if err := c.Do("POST", "/ai/reference/answer", map[string]string{"question": strings.Join(args, " ")}, &out); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out.Answer)
			for _, cite := range out.Cites {
				fmt.Fprintln(cmd.OutOrStdout(), "  see:", cite)
			}
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "audit <question>",
		Short: "Search the audit log with a question, and summarize what matched",
		Long: "Prints the summary and the filters it used. Run pando audit with those filters to\n" +
			"see the records.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Summary string         `json:"summary"`
				Note    string         `json:"note"`
				Matched int            `json:"matched"`
				Filter  map[string]any `json:"filter"`
			}
			if err := c.Do("POST", "/ai/audit/search", map[string]string{"question": strings.Join(args, " ")}, &out); err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			fmt.Fprintln(w, out.Summary)
			if out.Note != "" {
				fmt.Fprintln(w, out.Note)
			}
			fmt.Fprintf(w, "\n%d matched. Filters:\n", out.Matched)
			return printJSON(w, out.Filter)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "draft-access <description>",
		Short: "Draft a custom role and a group from a description",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out map[string]any
			if err := c.Do("POST", "/ai/access/draft", map[string]string{"description": strings.Join(args, " ")}, &out); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), out)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "draft-policy <description>",
		Short: "Propose host policy from a description",
		Long:  "Prints the proposed document; save it with pando policy set.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out map[string]any
			if err := c.Do("POST", "/ai/policy/draft", map[string]string{"description": strings.Join(args, " ")}, &out); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), out)
		},
	})

	return cmd
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
