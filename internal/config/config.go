package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
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
