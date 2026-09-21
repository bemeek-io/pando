// Package anthropic screens deployment plans with Claude (design 09 §6).
//
// The first AI adapter, and the one the interface in adapter/api was shaped
// against. It talks to the Messages API through the official Go SDK, reads the
// repository through the SourceView it is handed, and returns amendments drawn
// from the closed set — which core then validates again, because an adapter
// proposes and core decides (R-027).
//
// Nothing here writes. SourceView has no write method, and the tools this
// adapter exposes to the model are a listing and a read.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/bemeek-io/pando/internal/adapter/api"
	"github.com/bemeek-io/pando/internal/secret"
)

// Kind is what adapter_configs stores.
const Kind = "anthropic"

// DefaultModel is what an install gets without choosing.
//
// [P] Screening runs once per detection, on a repository somebody is about to
// deploy, and what is being bought is whether the app comes up on the first
// try. This is not a high-volume path where a cheaper model pays for itself,
// and the cost of a wrong amendment is a person's afternoon.
const DefaultModel = "claude-opus-5"

// Budget ceilings this adapter will not exceed regardless of what it is asked
// for. Core lowers them further; neither side raises the other's.
const (
	DefaultMaxFiles = 40
	DefaultMaxBytes = 256 << 10
	maxFileBytes    = 64 << 10
	maxIterations   = 24

	// maxTokens is generous because the answer is a tool call carrying
	// amendments and their reasons, and a truncated one is an amendment with
	// half a reason. Well under the model's ceiling; this is not a long answer.
	maxTokens = 8192
)

// Config is the adapter's own configuration (design 09 §7).
type Config struct {
	// APIKey is a secret.Value, so it renders [redacted] in every marshaler and
	// cannot reach a log line (R-194).
	//
	// Stored in adapter_configs in the clear, which is the reason APIKeyEnv
	// exists and is preferred. No adapter before this one held a credential,
	// so where an install-scoped credential should live is an open question
	// (O-20) rather than something this adapter gets to settle.
	APIKey secret.Value `json:"api_key,omitzero"`

	// APIKeyEnv names an environment variable holding the key, so the database
	// holds a name rather than a value. When neither this nor APIKey is set,
	// ANTHROPIC_API_KEY is read — the SDK's own convention.
	APIKeyEnv string `json:"api_key_env,omitempty"`

	Model string `json:"model,omitempty"`

	// BaseURL points at a gateway or a proxy. Empty is the Anthropic API.
	BaseURL string `json:"base_url,omitempty"`

	// ScreenPlans defaults to true (R-316): configuring this adapter meant
	// supplying a credential, and that was the decision. An install that wants
	// the adapter for something else turns it off here.
	ScreenPlans *bool `json:"screen_plans,omitempty"`

	MaxFiles int   `json:"max_files,omitempty"`
	MaxBytes int64 `json:"max_bytes,omitempty"`

	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// Adapter is the AI adapter.
type Adapter struct {
	cfg    Config
	client anthropic.Client
	ready  bool
}

// New returns an unconfigured adapter.
func New() *Adapter { return &Adapter{} }

func (a *Adapter) Kind() string           { return Kind }
func (a *Adapter) Category() api.Category { return api.CategoryAI }

// Configure reads the adapter's row from adapter_configs.
func (a *Adapter) Configure(_ context.Context, raw json.RawMessage) error {
	var cfg Config
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("anthropic: reading configuration: %w", err)
		}
	}

	key := resolveKey(cfg, os.Getenv)
	if key.IsZero() {
		// Not a warning. An adapter with no credential can do nothing, and
		// registering it would put a permanently unhealthy adapter in the
		// console with no way to tell it from a provider outage.
		return errors.New("anthropic: no API key was found — set api_key_env to the name of an " +
			"environment variable holding it, or set ANTHROPIC_API_KEY where Pando runs")
	}
	cfg.APIKey = key
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	if cfg.MaxFiles <= 0 {
		cfg.MaxFiles = DefaultMaxFiles
	}
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = DefaultMaxBytes
	}

	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey.Reveal())}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.TimeoutSeconds > 0 {
		opts = append(opts, option.WithRequestTimeout(time.Duration(cfg.TimeoutSeconds)*time.Second))
	}

	a.cfg = cfg
	a.client = anthropic.NewClient(opts...)
	a.ready = true
	return nil
}

// HealthCheck asks the API whether the configured model exists.
//
// A real call rather than a ping, because the failure this has to catch is a
// credential that does not work or a model name nobody can serve — and both
// answer a models lookup the same way they would answer a screening. It costs
// no tokens.
func (a *Adapter) HealthCheck(ctx context.Context) error {
	if !a.ready {
		return errors.New("anthropic: not configured")
	}
	if _, err := a.client.Models.Get(ctx, a.cfg.Model, anthropic.ModelGetParams{}); err != nil {
		return fmt.Errorf("anthropic: %w", err)
	}
	return nil
}

// Capabilities reports what this adapter does, as data (R-254, R-259).
func (a *Adapter) Capabilities(_ context.Context) (api.AICapabilities, error) {
	caps := api.AICapabilities{
		Model:    a.cfg.Model,
		MaxFiles: a.cfg.MaxFiles,
		MaxBytes: a.cfg.MaxBytes,
	}
	if a.screensPlans() {
		caps.Functions = append(caps.Functions, api.AIFunctionScreenPlan)
	}
	return caps, nil
}

// resolveKey finds the credential: inline, then the named variable, then the
// SDK's default variable. getenv is injected so the order is testable without
// touching the process environment.
func resolveKey(cfg Config, getenv func(string) string) secret.Value {
	if !cfg.APIKey.IsZero() {
		return cfg.APIKey
	}
	if cfg.APIKeyEnv != "" {
		return secret.New(getenv(cfg.APIKeyEnv))
	}
	return secret.New(getenv("ANTHROPIC_API_KEY"))
}

func (a *Adapter) screensPlans() bool {
	if a.cfg.ScreenPlans == nil {
		return true // R-316
	}
	return *a.cfg.ScreenPlans
}

var _ api.AIAdapter = (*Adapter)(nil)
