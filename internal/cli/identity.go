package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// identityCmd manages identity providers from the command line, as the
// console's Sign-in screen does (issue #51, R-261). A secret is asked for
// without echoing, never taken as a flag.
func identityCmd(client func() (*Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "identity-provider",
		Aliases: []string{"idp"},
		Short:   "Connect identity providers: OpenID Connect, SAML and SCIM",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Show the identity providers, what to register with each, and how soon each can revoke",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Providers []struct {
					ID          string `json:"id"`
					Kind        string `json:"kind"`
					Name        string `json:"name"`
					Enabled     bool   `json:"enabled"`
					SCIM        bool   `json:"scim_enabled"`
					CallbackURL string `json:"callback_url"`
					Problem     string `json:"problem"`
					Revocation  *struct {
						Mode          string `json:"mode"`
						WindowSeconds int64  `json:"window_seconds"`
					} `json:"revocation"`
				} `json:"providers"`
			}
			if err := c.Do("GET", "/identity-providers", nil, &out); err != nil {
				return err
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tKIND\tON\tSCIM\tREVOKES WITHIN\tCALLBACK")
			for _, p := range out.Providers {
				window := "-"
				if p.Revocation != nil {
					window = fmt.Sprintf("%ds (%s)", p.Revocation.WindowSeconds, p.Revocation.Mode)
				}
				on := strconv.FormatBool(p.Enabled)
				if p.Problem != "" {
					on = "not usable: " + p.Problem
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%t\t%s\t%s\n", p.ID, p.Name, p.Kind, on, p.SCIM, window, p.CallbackURL)
			}
			return w.Flush()
		},
	})

	var name, preset string
	var set []string
	var jit, linkByEmail, enable bool
	add := &cobra.Command{
		Use:   "add <oidc|saml>",
		Short: "Add an identity provider",
		Long: "Adds an identity provider, turned off until you test it and turn it on. --preset fills in what a\n" +
			"known provider needs (okta, entra, google, keycloak, authentik); --set KEY=VALUE sets the rest.\n" +
			"A client secret is asked for without echoing it.\n\n" +
			"  pando idp add oidc --preset okta --name Okta --set issuer=https://example.okta.com --set client_id=0oa…\n" +
			"  pando idp add saml --preset entra --name Entra --set idp_metadata_url=https://login.microsoftonline.com/…",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			info, err := identityKind(c, args[0])
			if err != nil {
				return err
			}
			values := map[string]string{}
			if preset != "" {
				found := false
				for _, p := range info.Presets {
					if p.ID == preset {
						found = true
						for k, v := range p.Values {
							values[k] = v
						}
						if name == "" {
							name = p.Label
						}
					}
				}
				if !found {
					return fmt.Errorf("%s has no preset %q", args[0], preset)
				}
			}
			if err := mergeSet(values, set); err != nil {
				return err
			}
			config, credentials, err := identityConfig(cmd, info, values, true)
			if err != nil {
				return err
			}
			if name == "" {
				return fmt.Errorf("a provider needs --name: the sign-in page shows it")
			}
			body := map[string]any{"kind": info.Kind, "name": name, "config": config,
				"jit_provisioning": jit, "link_by_email": linkByEmail, "enabled": enable}
			if len(credentials) > 0 {
				body["credentials"] = credentials
			}
			var created struct {
				ID          string `json:"id"`
				CallbackURL string `json:"callback_url"`
				EntityID    string `json:"entity_id"`
			}
			if err := c.Do("POST", "/identity-providers", body, &created); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Added %s (%s).\n", name, created.ID)
			if info.Kind == "saml" {
				fmt.Fprintf(out, "Register with the provider:\n  ACS URL:   %s\n  Entity ID: %s\n", created.CallbackURL, created.EntityID)
			} else {
				fmt.Fprintf(out, "Register this redirect URI with the provider:\n  %s\n", created.CallbackURL)
			}
			if !enable {
				fmt.Fprintf(out, "Test it with: pando idp test %s — then turn it on with: pando idp set %s --enable\n", created.ID, created.ID)
			}
			return nil
		},
	}
	add.Flags().StringVar(&name, "name", "", "what the sign-in page calls it, such as Okta")
	add.Flags().StringVar(&preset, "preset", "", "a known provider's settings: okta, entra, google, keycloak, authentik")
	add.Flags().StringArrayVar(&set, "set", nil, "a setting, KEY=VALUE; repeat for more")
	add.Flags().BoolVar(&jit, "jit", false, "create an account at someone's first sign-in")
	add.Flags().BoolVar(&linkByEmail, "link-by-email", false, "link a first sign-in to an existing account by verified email")
	add.Flags().BoolVar(&enable, "enable", false, "turn it on now, without testing it first")
	cmd.AddCommand(add)

	var setName string
	var setValues []string
	var on, off, secret bool
	var setJIT, setLink string
	change := &cobra.Command{
		Use:   "set <provider-id>",
		Short: "Change an identity provider",
		Long: "Changes a provider. --set KEY=VALUE changes one setting and keeps the rest; --secret asks for a\n" +
			"new client secret; --enable and --disable turn it on and off.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var cur struct {
				Kind   string         `json:"kind"`
				Config map[string]any `json:"config"`
			}
			if err := c.Do("GET", "/identity-providers/"+url.PathEscape(args[0]), nil, &cur); err != nil {
				return err
			}
			body := map[string]any{}
			if setName != "" {
				body["name"] = setName
			}
			if on && off {
				return fmt.Errorf("--enable and --disable together say nothing")
			}
			if on || off {
				body["enabled"] = on
			}
			for flag, key := range map[string]string{setJIT: "jit_provisioning", setLink: "link_by_email"} {
				if flag == "" {
					continue
				}
				b, err := strconv.ParseBool(flag)
				if err != nil {
					return fmt.Errorf("%s is true or false, and %q is neither", key, flag)
				}
				body[key] = b
			}
			if len(setValues) > 0 || secret {
				info, err := identityKind(c, cur.Kind)
				if err != nil {
					return err
				}
				values := map[string]string{}
				for k, v := range cur.Config {
					values[k] = fmt.Sprint(v)
				}
				if err := mergeSet(values, setValues); err != nil {
					return err
				}
				config, credentials, err := identityConfig(cmd, info, values, secret)
				if err != nil {
					return err
				}
				if len(setValues) > 0 {
					body["config"] = config
				}
				if len(credentials) > 0 {
					body["credentials"] = credentials
				}
			}
			if len(body) == 0 {
				return fmt.Errorf("nothing to change: give --name, --set, --secret, --enable, --disable, --jit or --link-by-email")
			}
			if err := c.Do("PATCH", "/identity-providers/"+url.PathEscape(args[0]), body, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Saved %s. It applies at the next sign-in.\n", args[0])
			return nil
		},
	}
	change.Flags().StringVar(&setName, "name", "", "a new name")
	change.Flags().StringArrayVar(&setValues, "set", nil, "a setting, KEY=VALUE; repeat for more")
	change.Flags().BoolVar(&secret, "secret", false, "ask for a new client secret")
	change.Flags().BoolVar(&on, "enable", false, "turn it on")
	change.Flags().BoolVar(&off, "disable", false, "turn it off")
	change.Flags().StringVar(&setJIT, "jit", "", "true or false: create an account at first sign-in")
	change.Flags().StringVar(&setLink, "link-by-email", "", "true or false: link by verified email")
	cmd.AddCommand(change)

	cmd.AddCommand(simple(client, "remove <provider-id>", "Remove an identity provider nobody has signed in through",
		func(c *Client, cmd *cobra.Command, args []string) error {
			if err := c.Do("DELETE", "/identity-providers/"+url.PathEscape(args[0]), nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed %s.\n", args[0])
			return nil
		}))

	cmd.AddCommand(simple(client, "check <provider-id>", "Check that Pando can reach the provider",
		func(c *Client, cmd *cobra.Command, args []string) error {
			var out struct {
				OK      bool   `json:"ok"`
				Message string `json:"message"`
				Remedy  string `json:"remedy"`
			}
			if err := c.Do("POST", "/identity-providers/"+url.PathEscape(args[0])+"/check", nil, &out); err != nil {
				return err
			}
			if !out.OK {
				return fmt.Errorf("%s %s", out.Message, out.Remedy)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "The provider answers.")
			return nil
		}))

	cmd.AddCommand(simple(client, "test <provider-id>", "Print the address that starts a test sign-in in your browser",
		func(c *Client, cmd *cobra.Command, args []string) error {
			fmt.Fprintf(cmd.OutOrStdout(),
				"A test sign-in happens in a browser signed in to Pando as an administrator. Open:\n  %s/admin/sign-in\nand choose Test sign-in, or go straight to:\n  %s/api/v1/identity-providers/%s/test\n",
				strings.TrimRight(c.BaseURL, "/"), strings.TrimRight(c.BaseURL, "/"), url.PathEscape(args[0]))
			return nil
		}))

	var scimOff bool
	scim := &cobra.Command{
		Use:   "scim-token <provider-id>",
		Short: "Turn SCIM on for a provider and print its token, or replace it",
		Long:  "Prints the SCIM base URL and a new token. The token is shown once; running this again replaces it.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			path := "/identity-providers/" + url.PathEscape(args[0]) + "/scim-token"
			if scimOff {
				if err := c.Do("DELETE", path, nil, nil); err != nil {
					return err
				}
				fmt.Fprintln(cmd.OutOrStdout(), "SCIM is off for this provider.")
				return nil
			}
			var out struct {
				Token   string `json:"token"`
				BaseURL string `json:"scim_base_url"`
			}
			if err := c.Do("POST", path, nil, &out); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "SCIM base URL: %s\nToken:         %s\nPando will not show this token again.\n", out.BaseURL, out.Token)
			return nil
		},
	}
	scim.Flags().BoolVar(&scimOff, "off", false, "turn SCIM off instead")
	cmd.AddCommand(scim)

	var replace bool
	link := &cobra.Command{
		Use:   "link <user-id> <provider-id> <external-id>",
		Short: "Link a provider's identity to an account",
		Long: "Whoever signs in with that identity signs in to that account. Nothing is merged: with --replace,\n" +
			"an account the identity already reaches is kept, suspended, as an alias.",
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Aliased string `json:"aliased_user_id"`
			}
			if err := c.Do("POST", "/users/"+url.PathEscape(args[0])+"/identities",
				map[string]any{"adapter_id": args[1], "external_id": args[2], "replace_account": replace}, &out); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Linked %s at %s to %s.\n", args[2], args[1], args[0])
			if out.Aliased != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is now a suspended alias of %s.\n", out.Aliased, args[0])
			}
			return nil
		},
	}
	link.Flags().BoolVar(&replace, "replace", false, "move the identity here if it already signs in to another account")
	cmd.AddCommand(link)

	cmd.AddCommand(&cobra.Command{
		Use:   "unlink <user-id> <provider-id> <external-id>",
		Short: "Unlink a provider's identity from an account",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			q := url.Values{"adapter_id": {args[1]}, "external_id": {args[2]}}
			if err := c.Do("DELETE", "/users/"+url.PathEscape(args[0])+"/identities?"+q.Encode(), nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Unlinked %s from %s.\n", args[2], args[0])
			return nil
		},
	})

	var unlinkGroup bool
	groupLink := &cobra.Command{
		Use:   "link-group <group-id> <provider-group-id>",
		Short: "Make a provider's group count as members of a Pando group",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			method, verb := "PUT", "Linked"
			if unlinkGroup {
				method, verb = "DELETE", "Unlinked"
			}
			if err := c.Do(method, "/groups/"+url.PathEscape(args[0])+"/links/"+url.PathEscape(args[1]), nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s and %s.\n", verb, args[1], args[0])
			return nil
		},
	}
	groupLink.Flags().BoolVar(&unlinkGroup, "remove", false, "remove the link instead")
	cmd.AddCommand(groupLink)
	return cmd
}

// simple is a command of fixed arguments that needs only a client.
func simple(client func() (*Client, error), use, short string, run func(*Client, *cobra.Command, []string) error) *cobra.Command {
	return &cobra.Command{
		Use: use, Short: short, Args: cobra.ExactArgs(strings.Count(use, "<")),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			return run(c, cmd, args)
		},
	}
}

type identityKindInfo struct {
	kindInfo
	Presets []struct {
		ID     string            `json:"id"`
		Label  string            `json:"label"`
		Values map[string]string `json:"values"`
	} `json:"presets"`
}

func identityKind(c *Client, kind string) (*identityKindInfo, error) {
	var out struct {
		Kinds []identityKindInfo `json:"kinds"`
	}
	if err := c.Do("GET", "/identity-providers", nil, &out); err != nil {
		return nil, err
	}
	for i := range out.Kinds {
		if out.Kinds[i].Kind == kind {
			return &out.Kinds[i], nil
		}
	}
	return nil, fmt.Errorf("this Pando has no %q identity provider: use oidc or saml", kind)
}

func mergeSet(values map[string]string, set []string) error {
	for _, kv := range set {
		key, value, found := strings.Cut(kv, "=")
		if !found || key == "" {
			return fmt.Errorf("--set takes KEY=VALUE, and %q has no =", kv)
		}
		values[key] = value
	}
	return nil
}

// identityConfig splits values into settings and secrets, asking for secrets.
func identityConfig(cmd *cobra.Command, info *identityKindInfo, values map[string]string, askSecrets bool) (map[string]any, map[string]string, error) {
	config := map[string]any{}
	credentials := map[string]string{}
	for _, f := range info.Fields {
		raw, given := values[f.Key]
		delete(values, f.Key)
		if f.Credential {
			if given {
				return nil, nil, fmt.Errorf("%s is a secret: leave it off --set and Pando will ask for it", f.Key)
			}
			if askSecrets {
				v, err := promptSecret(cmd, f.Label+" (empty to skip): ")
				if err != nil {
					return nil, nil, err
				}
				if v != "" {
					credentials[f.Key] = v
				}
			}
			continue
		}
		if !given || raw == "" {
			continue
		}
		if strings.Contains(raw, "{") {
			return nil, nil, fmt.Errorf("%s still has a placeholder in it (%s): replace the part in braces", f.Key, raw)
		}
		if f.Type == "bool" {
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return nil, nil, fmt.Errorf("%s is true or false, and %q is neither", f.Key, raw)
			}
			config[f.Key] = b
			continue
		}
		config[f.Key] = raw
	}
	for key := range values {
		return nil, nil, fmt.Errorf("%s providers have no setting %q", info.Kind, key)
	}
	return config, credentials, nil
}
