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
	Server   Server   `mapstructure:"server"`
	Database Database `mapstructure:"database"`
	Log      Log      `mapstructure:"log"`
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
	v.SetDefault("log.level", "info")
	v.SetDefault("log.development", false)

	v.SetEnvPrefix("PANDO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// PANDO_DATABASE_URL is the documented spelling, so bind it explicitly
	// rather than relying on the replacer to find it.
	_ = v.BindEnv("database.url", "PANDO_DATABASE_URL")

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
