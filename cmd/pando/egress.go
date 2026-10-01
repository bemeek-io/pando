package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/trypando/pando/internal/egress"
	"github.com/trypando/pando/internal/log"
)

// Environment the egress gateway is started with. The Docker runtime sets
// both on the gateway container it runs in front of a restricted app (R-187).
const (
	envEgressRules  = "PANDO_EGRESS_RULES"
	envEgressListen = "PANDO_EGRESS_LISTEN"
)

// egressGatewayCmd runs Pando's egress gateway (R-187).
//
// Hidden: nobody runs it by hand. A runtime adapter starts it, from Pando's own
// image, in front of each app whose egress is restricted. It needs no database,
// no configuration file and no container runtime socket — only the rules it
// enforces, from the environment.
func egressGatewayCmd() *cobra.Command {
	var listen string
	cmd := &cobra.Command{
		Use:    "egress-gateway",
		Short:  "Run the egress gateway a restricted app's traffic leaves through",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			rules, err := egressRulesFromEnv()
			if err != nil {
				return err
			}
			compiled, err := rules.Compile()
			if err != nil {
				return fmt.Errorf("the egress gateway was given a rule it cannot use, so it did not start: %w", err)
			}

			if listen == "" {
				listen = os.Getenv(envEgressListen)
			}
			if listen == "" {
				listen = ":" + strconv.Itoa(egress.DefaultPort)
			}
			if _, _, err := net.SplitHostPort(listen); err != nil {
				return fmt.Errorf("%q is not an address the egress gateway can listen on. Write it as host:port or :port, such as :3128", listen)
			}

			logger, err := log.New("info", false)
			if err != nil {
				return err
			}
			defer func() { _ = logger.Sync() }()

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			logger.Info("egress gateway listening",
				zap.String("addr", listen), zap.Int("layers", len(rules.Layers)), zap.Bool("block_private", rules.BlockPrivate))
			gw := &egress.Gateway{Rules: compiled, Logger: logger}
			return gw.ListenAndServe(ctx, listen)
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "", "address to listen on (default $"+envEgressListen+", else :3128)")
	return cmd
}

// egressRulesFromEnv reads the rules the gateway enforces.
//
// Strictly: an unknown field is a refusal to start rather than a field
// ignored. A gateway that dropped part of its rules would let out exactly
// what that part was written to stop.
func egressRulesFromEnv() (egress.Rules, error) {
	raw := os.Getenv(envEgressRules)
	if raw == "" {
		return egress.Rules{}, fmt.Errorf("the egress gateway was started without its rules, so it did not start. Pando sets %s when it starts the gateway", envEgressRules)
	}
	var rules egress.Rules
	dec := json.NewDecoder(bytes.NewReader([]byte(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rules); err != nil {
		return egress.Rules{}, fmt.Errorf("the egress gateway could not read its rules from %s, so it did not start: %w", envEgressRules, err)
	}
	return rules, nil
}
