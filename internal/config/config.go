package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"

	"github.com/bemeek-io/pando/internal/core/spec"
)

// Config is Pando's configuration, from YAML, environment, and flags (R-271).
//
// Adapter configuration is deliberately absent: configured adapter instances
// live in the database (adapter_configs), not here, so they can be changed
// through the API without a restart or a file edit.
type Config struct {
	Server     Server     `mapstructure:"server"`
	Database   Database   `mapstructure:"database"`
	Log        Log        `mapstructure:"log"`
	Reconciler Reconciler `mapstructure:"reconciler"`
}

// Reconciler tunes R-149's retry backoff and R-150's give-up rule.
//
// Present because the acceptance test for R-151 has to wait out the real
// schedule, and at the shipped numbers that is forty minutes. Leave these unset
// in production: the defaults are the requirement, and Pando says so at startup
// if they are set faster.
type Reconciler struct {
	// Backoff is the retry schedule, indexed by consecutive failures, as a
	// comma-separated list of durations: "0s,5s,15s,60s,5m".
	Backoff string `mapstructure:"backoff"`

	// FailureThreshold is how many failures inside FailureWindow before an app
	// is given up on and left failed (R-150).
	FailureThreshold int `mapstructure:"failure_threshold"`

	// FailureWindow is measured from the last failure, so a slow crash loop
	// still reaches the threshold rather than resetting forever.
	FailureWindow time.Duration `mapstructure:"failure_window"`

	// GCInterval is how often garbage collection runs. Zero means the shipped
	// hour. Short intervals are wasteful rather than dangerous — nothing the
	// collector does is urgent — so unlike the backoff this gets no warning.
	GCInterval time.Duration `mapstructure:"gc_interval"`
}

// BackoffSchedule parses Backoff, returning nil when it is unset.
func (r Reconciler) BackoffSchedule() ([]time.Duration, error) {
	if strings.TrimSpace(r.Backoff) == "" {
		return nil, nil
	}

	var out []time.Duration
	for _, part := range strings.Split(r.Backoff, ",") {
		d, err := time.ParseDuration(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("reconciler.backoff: %q is not a duration like 5s or 2m: %w", part, err)
		}
		if d < 0 {
			return nil, fmt.Errorf("reconciler.backoff: %q is negative", part)
		}
		out = append(out, d)
	}
	return out, nil
}

type Server struct {
	Addr            string        `mapstructure:"addr"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`

	// ProxyUpstream is the address routing adapters are told to send traffic
	// to (R-023). It must be reachable from wherever an adapter's data plane
	// runs, which is not necessarily where Pando runs.
	ProxyUpstream string `mapstructure:"proxy_upstream"`

	// Issuer is the `iss` claim in every assertion, and identifies this
	// install to the apps it fronts.
	Issuer string `mapstructure:"issuer"`

	// RoutingMode is how apps are addressed — "subdomain" or "path" (design 03
	// §4.1). Neither is a global setting in the spec sense; this is the
	// install's default shape, and each app's spec still names its own mode.
	RoutingMode spec.RoutingMode `mapstructure:"routing_mode"`

	// BaseDomain is what a per-app subdomain is carved out of, so an app
	// called "notes" is reached at notes.example.com. Only used in subdomain
	// mode; empty is correct for an install addressing apps by path, which is
	// the default and needs no DNS at all (R-002 — setup cost is paid once).
	BaseDomain string `mapstructure:"base_domain"`

	// PortRangeStart and PortRangeEnd bound the ports handed to apps in
	// port-mode routing — the loopback adapter's only mode, and so the laptop
	// default. [P]: the requirements do not specify a range (O-15).
	PortRangeStart int `mapstructure:"port_range_start"`
	PortRangeEnd   int `mapstructure:"port_range_end"`

	// WorkDir is where a DR bundle is assembled before it is encrypted and
	// streamed out (R-212). It needs room for a database dump plus every app
	// volume, which is why it is configurable and does not default to the
	// system temporary directory — that is often a small tmpfs, and running out
	// of space partway through a backup is how an install discovers it has no
	// backups.
	WorkDir string `mapstructure:"work_dir"`
}

type Database struct {
	// URL is the connection string. The bundled Compose file supplies it; an
	// operator pointing Pando at an existing Postgres sets PANDO_DATABASE_URL.
	//
	// Whatever it names, the account must be able to manage Pando's restricted
	// application role — see design 00 §1.1. Pando verifies this at startup and
	// refuses to run if the audit log would be rewritable.
	URL            string        `mapstructure:"url"`
	ConnectTimeout time.Duration `mapstructure:"connect_timeout"`
}

type Log struct {
	Level       string `mapstructure:"level"`
	Development bool   `mapstructure:"development"`
}

// Load reads configuration. Precedence, lowest to highest: defaults, config
// file, environment, flags.
func Load(path string) (*Config, error) {
	v := viper.New()

	v.SetDefault("server.addr", ":8080")
	v.SetDefault("server.shutdown_timeout", 15*time.Second)
	v.SetDefault("database.connect_timeout", 60*time.Second)
	v.SetDefault("server.issuer", "https://pando.local")
	v.SetDefault("server.routing_mode", string(spec.RoutingPath))
	v.SetDefault("server.port_range_start", 9000)
	v.SetDefault("server.port_range_end", 9999)
	v.SetDefault("server.work_dir", "/var/lib/pando/work")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.development", false)

	v.SetEnvPrefix("PANDO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Registering every key with a zero default, so Unmarshal can see it.
	//
	// This is the other half of the same viper trap as the binds below. BindEnv
	// makes a key readable through Get; it does not necessarily put the key in
	// the settings map that Unmarshal walks. A key with neither a default nor a
	// config-file entry can therefore be bound, be present in the environment,
	// and still arrive as the zero value — silently, which is the part that
	// costs an afternoon.
	//
	// A zero default is not a value: it is how the key gets registered.
	v.SetDefault("server.base_domain", "")
	v.SetDefault("reconciler.backoff", "")
	v.SetDefault("reconciler.failure_threshold", 0)
	v.SetDefault("reconciler.failure_window", time.Duration(0))
	v.SetDefault("reconciler.gc_interval", time.Duration(0))

	// Every key is bound explicitly, and that is not belt-and-braces.
	//
	// AutomaticEnv looks up an environment variable only for keys viper already
	// knows — from a default, a config file, or a bind. A key with none of
	// those is invisible to Unmarshal, so `PANDO_SERVER_BASE_DOMAIN=…` sets
	// nothing and says nothing: the process starts, the setting is empty, and
	// the failure surfaces much later as an app with no hostname. That happened
	// to base_domain.
	//
	// A key that is only ever set from the environment therefore has to be
	// listed here. Defaults elsewhere in this function register their keys as a
	// side effect; these have no sensible default to register them.
	for key, env := range map[string]string{
		"database.url":          "PANDO_DATABASE_URL",
		"server.base_domain":    "PANDO_SERVER_BASE_DOMAIN",
		"server.proxy_upstream": "PANDO_SERVER_PROXY_UPSTREAM",
		"server.issuer":         "PANDO_SERVER_ISSUER",
		"server.addr":           "PANDO_SERVER_ADDR",
		"server.routing_mode":   "PANDO_SERVER_ROUTING_MODE",
		"server.work_dir":       "PANDO_SERVER_WORK_DIR",
		"log.level":             "PANDO_LOG_LEVEL",
	} {
		_ = v.BindEnv(key, env)
	}

	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("reading config file %s: %w", path, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("parsing configuration: %w", err)
	}
	return &cfg, cfg.validate()
}

func (c *Config) validate() error {
	if c.Database.URL == "" {
		return fmt.Errorf("no database URL configured: set PANDO_DATABASE_URL, or database.url in the config file")
	}
	return nil
}
