package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/term"

	"github.com/bemeek-io/pando/internal/config"
	"github.com/bemeek-io/pando/internal/core/audit"
	"github.com/bemeek-io/pando/internal/core/bootstrap"
	"github.com/bemeek-io/pando/internal/core/state"
	"github.com/bemeek-io/pando/internal/errs"
	"github.com/bemeek-io/pando/internal/hash"
	"github.com/bemeek-io/pando/internal/log"
	"github.com/bemeek-io/pando/internal/secret"
)

// Server-side administration: the commands that work when nobody can sign in.
//
// These live here and not in internal/cli, and the difference is load-bearing.
// internal/cli is a client of the API and nothing more (R-261) — it imports no
// core package, so a command that needed something the API cannot do would fail
// to compile rather than quietly grow a shortcut. A password reset is exactly
// such a command: it exists for the case where there is no account to
// authenticate as, so there is no request it could make.
//
// Host shell access is the authorization, and it is the right boundary rather
// than an absence of one. Whoever can run this can already read the config file
// that holds the database credentials, and could change the row by hand. The
// command exists so that doing it correctly — a real argon2id digest, sessions
// ended, an audit event written — is easier than doing it by hand.

func adminCmd(configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administer this installation from the host",
		Long: "Commands that work against the database directly, for when nobody can sign in.\n\n" +
			"Running these needs shell access on the host and the configuration that names the\n" +
			"database. That is the whole of the authorization, and it is the same access that\n" +
			"could change the row by hand.",
	}
	cmd.AddCommand(resetPasswordCmd(configPath))
	return cmd
}

func resetPasswordCmd(configPath *string) *cobra.Command {
	var supplied string

	cmd := &cobra.Command{
		Use:   "reset-password [username]",
		Short: "Set a local account's password when nobody can sign in",
		Long: "Sets a new password for a local account and ends every session it has.\n\n" +
			"The account must change the password again at its next sign-in: this hands a way\n" +
			"back in to somebody, which is not the same as choosing their credential.\n\n" +
			"Accounts that sign in through an external identity provider are changed where they\n" +
			"live, not here.",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,

		RunE: func(cmd *cobra.Command, args []string) error {
			username := bootstrap.AdminUsername
			if len(args) == 1 {
				username = args[0]
			}

			password, generated, err := passwordFor(cmd, supplied)
			if err != nil {
				return err
			}
			if password.Len() < hash.MinPasswordLength {
				return errs.Newf(errs.ValidInvalid,
					"A password needs at least %d characters.", hash.MinPasswordLength)
			}

			cfg, logger, err := setup(*configPath)
			if err != nil {
				return err
			}
			defer func() { _ = logger.Sync() }()

			ctx := log.Into(cmd.Context(), logger)
			if err := resetPassword(ctx, cfg, username, password, logger); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Password reset for %s.\n", username)
			if generated {
				// To stdout, not the log. The log is the thing that loses it,
				// which is how this command came to exist.
				fmt.Fprintf(cmd.OutOrStdout(), "New password: %s\n", password.Reveal())
			}
			fmt.Fprintln(cmd.OutOrStdout(),
				"Every session it had has ended, and it must set a new password at the next sign-in.")
			return nil
		},
	}

	cmd.Flags().StringVar(&supplied, "password", "",
		"the new password; prompts on a terminal, and generates one otherwise")
	return cmd
}

// passwordFor resolves the new password: the flag, a prompt, or a generated one.
//
// Prompting is preferred over the flag because a password in argv is in the
// host's process list and in the operator's shell history. The flag stays for
// the unattended case, where there is no terminal to prompt on.
func passwordFor(cmd *cobra.Command, supplied string) (secret.Value, bool, error) {
	if supplied != "" {
		return secret.New(supplied), false, nil
	}

	if f, ok := cmd.InOrStdin().(interface{ Fd() uintptr }); ok && term.IsTerminal(int(f.Fd())) {
		fmt.Fprint(cmd.ErrOrStderr(), "New password (leave empty to generate one): ")
		typed, err := term.ReadPassword(int(f.Fd()))
		fmt.Fprintln(cmd.ErrOrStderr())
		if err != nil {
			return secret.Value{}, false, fmt.Errorf("reading the password: %w", err)
		}
		if len(typed) > 0 {
			return secret.New(string(typed)), false, nil
		}
	}

	password, err := hash.Generate()
	return password, true, err
}

// resetPassword does the work, against the database rather than the API.
func resetPassword(ctx context.Context, cfg *config.Config, username string, password secret.Value, logger *zap.Logger) error {
	db, err := state.Connect(ctx, state.ConnectOptions{
		OwnerURL:       cfg.Database.URL,
		ConnectTimeout: cfg.Database.ConnectTimeout,
	})
	if err != nil {
		return err
	}
	defer db.Close()

	digest, err := hash.New(password)
	if err != nil {
		return errs.Wrap(errs.Internal, "Could not secure the new password.", err)
	}

	users := state.NewUsers(db)
	userID, err := users.ResetPassword(ctx, username, digest)
	if err != nil {
		return err
	}

	// Ending the sessions is not tidying up. A reset that leaves a live session
	// alone has not reset anything: whoever is holding that cookie keeps the
	// access the reset was meant to take back (R-048).
	sessions := state.NewSessions(db)
	if err := sessions.RevokeAllForUser(ctx, userID); err != nil {
		return err
	}

	// Audited, and its failure is fatal to the command.
	//
	// A credential reset that leaves no trace is a backdoor, and this one runs
	// outside every check the API makes — no verb, no session, no principal but
	// whoever holds the host. That is precisely the event the log exists for
	// (R-227), and reporting success without it would be reporting the wrong
	// thing.
	auditor := audit.New(db.Pool)
	if err := auditor.Write(ctx, audit.Event{
		PrincipalKind: audit.KindSystem,
		PrincipalID:   "system",
		Action:        "user.password.reset",
		TargetKind:    "user",
		TargetID:      userID,
		Detail: map[string]any{
			"username": username,
			"via":      "pando admin reset-password",
			"reason":   "run from the host shell, outside any session",
		},
	}); err != nil {
		return errs.Wrap(errs.Internal,
			"The password was reset and Pando could not record it in the audit log.", err)
	}

	logger.Info("password reset from the host shell",
		zap.String("username", username), zap.String("user_id", userID))
	return nil
}
