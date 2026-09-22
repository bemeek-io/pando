package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// sectionCmd manages the sections in your own launcher (R-342): named
// groupings of the apps you can open. The console makes them from an app's
// menu; this is the same thing for a terminal, as R-261 requires.
func sectionCmd(client func() (*Client, error)) *cobra.Command {
	cmd := &cobra.Command{Use: "section", Short: "Group the apps in your launcher into sections"}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "The apps you can open, by section",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var out struct {
				Apps []struct {
					ID        string `json:"id"`
					Name      string `json:"name"`
					SectionID string `json:"section_id"`
					Favorite  bool   `json:"favorite"`
				} `json:"apps"`
				Sections []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"sections"`
			}
			if err := c.Do("GET", "/me/apps", nil, &out); err != nil {
				return err
			}

			names := map[string]string{"": "Your apps"}
			for _, s := range out.Sections {
				names[s.ID] = s.Name
			}
			t := table(cmd.OutOrStdout(), "SECTION", "APP", "FAVORITE", "APP ID", "SECTION ID")
			for _, a := range out.Apps {
				fav := ""
				if a.Favorite {
					fav = "yes"
				}
				fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n", names[a.SectionID], a.Name, fav, a.ID, a.SectionID)
			}
			// Sections with nothing in them are still sections; list them so
			// their IDs can be found to use or delete.
			used := map[string]bool{}
			for _, a := range out.Apps {
				used[a.SectionID] = true
			}
			for _, s := range out.Sections {
				if !used[s.ID] {
					fmt.Fprintf(t, "%s\t\t\t\t%s\n", s.Name, s.ID)
				}
			}
			return t.Flush()
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "create <name>",
		Short: "Make a section",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			var section struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}
			if err := c.Do("POST", "/me/sections", map[string]string{"name": args[0]}, &section); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Made the section %q (%s).\n", section.Name, section.ID)
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "rename <section-id> <name>",
		Short: "Rename a section",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			if err := c.Do("PATCH", "/me/sections/"+args[0], map[string]string{"name": args[1]}, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed the section to %q.\n", args[1])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete <section-id>",
		Short: "Delete a section. Its apps go back to Your apps",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			if err := c.Do("DELETE", "/me/sections/"+args[0], nil, nil); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Deleted the section. Its apps are back under Your apps.")
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "add <section-id> <app>",
		Short: "Move an app into a section",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			if err := c.Do("PUT", "/me/sections/"+args[0]+"/apps/"+args[1], nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Moved %s into the section.\n", args[1])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "remove <section-id> <app>",
		Short: "Move an app out of a section, back to Your apps",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client()
			if err != nil {
				return err
			}
			if err := c.Do("DELETE", "/me/sections/"+args[0]+"/apps/"+args[1], nil, nil); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Moved %s back to Your apps.\n", args[1])
			return nil
		},
	})

	return cmd
}
