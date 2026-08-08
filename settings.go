package axon

import (
	"fmt"
	"sort"
	"time"
)

// settings.go is the whole of axon's configuration: one struct tree that maps
// field-for-field onto axon.yaml.
//
// Before this existed, these values lived in three places — thirteen AXON_*
// environment variables, a dozen constants scattered across the files that
// used them, and a handful of literals inline. None of them were visible from
// the outside, and several could not be reached at all. A library whose
// behaviour depends on state you cannot see or set is not simple, it is just
// quiet.
//
// The rule that decides what belongs here: a value is configuration if
// changing it is an *operational* decision — what it costs, how long it may
// take, how much context it may consume, where state is written. A value stays
// in the code if changing it is a *correctness* decision. That is why file
// permissions, buffer sizes, the binary-content heuristic and every prompt
// axon sends are not in this file and must not be added to it.
//
// Nothing in this package reads this struct from disk. Loading is in load.go,
// and it happens because an embedder asked for it.

// Settings is the parsed contents of axon.yaml.
//
// The zero value is usable: every field left unset falls back to the value in
// DefaultSettings, applied by WithDefaults. That is what lets a config name
// only the two or three things it cares about.
type Settings struct {
	// Providers are the endpoints axon may talk to, keyed by the name an
	// embedder selects them with.
	Providers map[string]Endpoint `yaml:"providers"`

	// Session controls where conversation state is written.
	Session SessionConfig `yaml:"session"`

	// Model shapes each request to the provider.
	Model ModelConfig `yaml:"model"`

	// Retry bounds how hard axon tries when a request fails.
	Retry RetryConfig `yaml:"retry"`

	// Tools caps what a single tool call may cost in time and context.
	Tools ToolsConfig `yaml:"tools"`

	// Pruner tunes the secondary model that parks stale context.
	Pruner PrunerSettings `yaml:"pruner"`
}

// ---------------------------------------------------------------------------
// Providers
// ---------------------------------------------------------------------------

// Endpoint is one place to send requests.
//
// axon speaks exactly one protocol: OpenAI-style streaming chat completions at
// {base_url}/v1/chat/completions with a bearer token. Any endpoint that speaks
// it works. There is no per-provider code, and adding a provider that speaks a
// different protocol means implementing the Model interface, not editing this.
type Endpoint struct {
	// BaseURL is the API root. "/v1" is appended when absent. Required.
	BaseURL string `yaml:"base_url"`

	// APIKey is sent as a bearer token. Write it as ${VAR} and it is resolved
	// from the credentials file, so this config carries no secret and can be
	// committed.
	APIKey string `yaml:"api_key"`

	// Models this endpoint may be asked for, keyed by the identifier the
	// provider expects. The value may be empty — it carries per-model options
	// when there are any.
	//
	// axon does not choose between them. Listing is all this does; the
	// embedder pins one or offers the user a choice.
	Models map[string]ModelOptions `yaml:"models"`
}

// ModelOptions are per-model settings for one endpoint.
type ModelOptions struct {
	// Route is forwarded verbatim as the request's "provider" field. This is
	// an OpenRouter routing hint and is ignored by endpoints that do not
	// understand it — set it only where it means something.
	Route string `yaml:"route"`
}

// ModelNames returns this endpoint's models in a stable order, so a picker
// built from them does not reshuffle itself between runs.
func (e Endpoint) ModelNames() []string {
	names := make([]string, 0, len(e.Models))
	for name := range e.Models {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// ---------------------------------------------------------------------------
// Sections
// ---------------------------------------------------------------------------

// SessionConfig decides where conversation state lives.
type SessionConfig struct {
	// DataDir is the root for session files and background-shell logs.
	DataDir string `yaml:"data_dir"`

	// Path pins one session file instead of deriving it from the working
	// directory. Empty means derive, which is what gives each project its own
	// session.
	Path string `yaml:"path"`
}

// ModelConfig shapes the request sent to the provider.
type ModelConfig struct {
	// MaxTokens caps one reply.
	MaxTokens int `yaml:"max_tokens"`

	// RequestTimeout bounds a whole streamed response. It is generous
	// because a long agent turn legitimately takes minutes.
	RequestTimeout Duration `yaml:"request_timeout"`

	// IdleTimeout bounds the gap between two chunks, so a provider that goes
	// silent mid-stream fails fast instead of holding the turn until
	// RequestTimeout.
	IdleTimeout Duration `yaml:"idle_timeout"`

	// ReasoningEffort is forwarded as reasoning.effort. Empty omits it.
	ReasoningEffort string `yaml:"reasoning_effort"`

	// ExcludeReasoning asks the provider not to return reasoning tokens.
	ExcludeReasoning bool `yaml:"exclude_reasoning"`
}

// RetryConfig bounds how hard axon tries.
type RetryConfig struct {
	// MaxAttempts includes the first try. One means never retry.
	MaxAttempts int `yaml:"max_attempts"`

	// BackoffCap ceilings the exponential wait between attempts.
	BackoffCap Duration `yaml:"backoff_cap"`

	// OnStatus lists the HTTP status codes worth retrying. Network errors
	// and truncated streams are always retried and are not listed here.
	OnStatus []int `yaml:"on_status"`
}

// Retryable reports whether a status code is one this policy retries.
func (r RetryConfig) Retryable(status int) bool {
	for _, code := range r.OnStatus {
		if code == status {
			return true
		}
	}

	return false
}

// ToolsConfig caps what one tool call may cost.
//
// Every value here bounds how many tokens a single call can push into the
// model's context, or how long it can hold a turn. They are the only defence
// against one bad tool call costing a fortune.
type ToolsConfig struct {
	Read       ReadConfig       `yaml:"read"`
	Exec       ExecConfig       `yaml:"exec"`
	BashOutput BashOutputConfig `yaml:"bash_output"`
	Search     SearchConfig     `yaml:"search"`
}

type ReadConfig struct {
	// Lines is how many lines a slice read returns by default.
	Lines int `yaml:"lines"`

	// MaxBytes caps a full read, so a huge log is refused rather than loaded
	// into memory and split line-by-line straight into context.
	MaxBytes Bytes `yaml:"max_bytes"`
}

type ExecConfig struct {
	// Timeout is the default for a foreground command.
	Timeout Duration `yaml:"timeout"`

	// MaxTimeout ceilings whatever timeout the model asks for. Without it one
	// tool call could hold the turn for hours.
	MaxTimeout Duration `yaml:"max_timeout"`

	// OutputBytes caps one command's captured output.
	OutputBytes Bytes `yaml:"output_bytes"`

	// TailLines is the default number of trailing lines kept when output
	// exceeds the cap.
	TailLines int `yaml:"tail_lines"`

	// MaxTailLines ceilings the tail the model may request.
	MaxTailLines int `yaml:"max_tail_lines"`

	// KillGrace is how long a killed command gets to release its output pipe
	// before axon stops waiting for it.
	KillGrace Duration `yaml:"kill_grace"`
}

type BashOutputConfig struct {
	// MaxBytes caps one poll of a background shell, so a noisy server cannot
	// dump megabytes into context per read.
	MaxBytes Bytes `yaml:"max_bytes"`
}

type SearchConfig struct {
	// Timeout bounds a single ripgrep invocation.
	Timeout Duration `yaml:"timeout"`

	// MaxMatches caps how many matches one search returns.
	MaxMatches int `yaml:"max_matches"`

	// OutputBytes caps the total size of a search result.
	OutputBytes Bytes `yaml:"output_bytes"`
}

// PruneMode is how hard the curator is asked to look for blocks to park.
//
// It changes the curator's judgment threshold — how confident it must be
// that a block is dead before parking it — and nothing else. Every mode gets
// the same never-park guarantees and the same one-way parking semantics; a
// stricter mode is not permitted to park something a looser one protects.
// The knob is the bar for "is this still needed", not the safety rules.
type PruneMode string

const (
	// PruneLow parks only what is unambiguously dead. Use when context is
	// cheap relative to the cost of the agent losing a thread.
	PruneLow PruneMode = "low"

	// PruneModerate parks what is clearly no longer in play. The default.
	PruneModerate PruneMode = "moderate"

	// PruneExtreme parks anything not carrying the current direction
	// forward. Use when context is the binding constraint and the agent
	// re-reading a file occasionally is cheaper than a bloated window.
	PruneExtreme PruneMode = "extreme"
)

// PruneModes lists the valid modes, for validation and error messages.
var PruneModes = []PruneMode{PruneLow, PruneModerate, PruneExtreme}

// Valid reports whether m is a mode the curator knows how to run.
func (m PruneMode) Valid() bool {
	for _, known := range PruneModes {
		if m == known {
			return true
		}
	}
	return false
}

// PrunerSettings tunes the secondary model that parks stale context.
type PrunerSettings struct {
	// Mode is how aggressively the curator parks. See PruneMode. The zero
	// value takes PruneModerate.
	Mode PruneMode `yaml:"mode"`

	// WindowBlocks is how many of the most recent blocks are kept active for
	// free, no model call. Only blocks older than the window are ever
	// candidates for parking — by the window itself (recency, free) or by
	// the curator (judgment, costs a call). This is the first and cheapest
	// line of defense; the curator only ever sees what falls outside it.
	WindowBlocks int `yaml:"window_blocks"`

	// FloorTokens is the context size below which pruning is not worth a
	// round trip.
	FloorTokens int `yaml:"floor_tokens"`

	// GrowthTokens is how much context must accumulate since the last prune
	// before another one fires.
	GrowthTokens int `yaml:"growth_tokens"`

	// MaxTokens caps the curator's reply. Its answer is one line of JSON; a
	// chatty model hits this wall instead of burning tokens on prose.
	MaxTokens int `yaml:"max_tokens"`

	// Timeout bounds one curator call.
	Timeout Duration `yaml:"timeout"`
}

// ---------------------------------------------------------------------------
// Defaults
// ---------------------------------------------------------------------------

// DefaultSettings returns the settings axon uses when a config names nothing.
//
// These are the values that were previously compiled in or read from the
// environment, unchanged, so adopting a config file does not change how an
// existing agent behaves. Every one is deliberately conservative: they bound
// cost, and the failure mode of a cap that is too low is an inconvenience
// while the failure mode of one that is too high is a bill.
func DefaultSettings() Settings {
	return Settings{
		Session: SessionConfig{
			DataDir: defaultDataDir(),
		},

		Model: ModelConfig{
			MaxTokens:      20000,
			RequestTimeout: Duration(30 * time.Minute),
			IdleTimeout:    Duration(20 * time.Second),
		},

		Retry: RetryConfig{
			MaxAttempts: 10,
			BackoffCap:  Duration(60 * time.Second),
			OnStatus:    []int{429, 500, 502, 503, 504},
		},

		Tools: ToolsConfig{
			Read: ReadConfig{
				Lines:    200,
				MaxBytes: 2 << 20,
			},
			Exec: ExecConfig{
				Timeout:      Duration(30 * time.Second),
				MaxTimeout:   Duration(10 * time.Minute),
				OutputBytes:  12000,
				TailLines:    50,
				MaxTailLines: 500,
				KillGrace:    Duration(2 * time.Second),
			},
			BashOutput: BashOutputConfig{
				MaxBytes: 32 << 10,
			},
			Search: SearchConfig{
				Timeout:     Duration(30 * time.Second),
				MaxMatches:  100,
				OutputBytes: 12000,
			},
		},

		Pruner: PrunerSettings{
			Mode:         PruneModerate,
			WindowBlocks: 30,
			FloorTokens:  10000,
			GrowthTokens: 5000,
			MaxTokens:    4096,
			Timeout:      Duration(60 * time.Second),
		},
	}
}

// WithDefaults fills every unset field from DefaultSettings and returns the
// result.
//
// This is the load-bearing half of "the zero value is usable". A YAML file
// that sets `tools.read.lines` and nothing else decodes into a struct where
// every other number is zero — and a zero read limit is not "unlimited", it is
// an agent that returns nothing. Every scalar therefore means "unset" at zero
// and is replaced here.
//
// The consequence, which is worth stating because it is the one real
// limitation of this design: a value cannot be deliberately set to zero. No
// field in this config has a meaningful zero, so nothing is lost, but a future
// field that does would need a pointer instead.
func (r Settings) WithDefaults() Settings {
	d := DefaultSettings()

	fillString(&r.Session.DataDir, d.Session.DataDir)

	fillInt(&r.Model.MaxTokens, d.Model.MaxTokens)
	fillDuration(&r.Model.RequestTimeout, d.Model.RequestTimeout)
	fillDuration(&r.Model.IdleTimeout, d.Model.IdleTimeout)

	fillInt(&r.Retry.MaxAttempts, d.Retry.MaxAttempts)
	fillDuration(&r.Retry.BackoffCap, d.Retry.BackoffCap)
	if len(r.Retry.OnStatus) == 0 {
		r.Retry.OnStatus = d.Retry.OnStatus
	}

	fillInt(&r.Tools.Read.Lines, d.Tools.Read.Lines)
	fillBytes(&r.Tools.Read.MaxBytes, d.Tools.Read.MaxBytes)

	fillDuration(&r.Tools.Exec.Timeout, d.Tools.Exec.Timeout)
	fillDuration(&r.Tools.Exec.MaxTimeout, d.Tools.Exec.MaxTimeout)
	fillBytes(&r.Tools.Exec.OutputBytes, d.Tools.Exec.OutputBytes)
	fillInt(&r.Tools.Exec.TailLines, d.Tools.Exec.TailLines)
	fillInt(&r.Tools.Exec.MaxTailLines, d.Tools.Exec.MaxTailLines)
	fillDuration(&r.Tools.Exec.KillGrace, d.Tools.Exec.KillGrace)

	fillBytes(&r.Tools.BashOutput.MaxBytes, d.Tools.BashOutput.MaxBytes)

	fillDuration(&r.Tools.Search.Timeout, d.Tools.Search.Timeout)
	fillInt(&r.Tools.Search.MaxMatches, d.Tools.Search.MaxMatches)
	fillBytes(&r.Tools.Search.OutputBytes, d.Tools.Search.OutputBytes)

	if r.Pruner.Mode == "" {
		r.Pruner.Mode = d.Pruner.Mode
	}
	fillInt(&r.Pruner.WindowBlocks, d.Pruner.WindowBlocks)
	fillInt(&r.Pruner.FloorTokens, d.Pruner.FloorTokens)
	fillInt(&r.Pruner.GrowthTokens, d.Pruner.GrowthTokens)
	fillInt(&r.Pruner.MaxTokens, d.Pruner.MaxTokens)
	fillDuration(&r.Pruner.Timeout, d.Pruner.Timeout)

	return r
}

func fillString(dst *string, fallback string) {
	if *dst == "" {
		*dst = fallback
	}
}

func fillInt(dst *int, fallback int) {
	if *dst <= 0 {
		*dst = fallback
	}
}

func fillDuration(dst *Duration, fallback Duration) {
	if *dst <= 0 {
		*dst = fallback
	}
}

func fillBytes(dst *Bytes, fallback Bytes) {
	if *dst <= 0 {
		*dst = fallback
	}
}

// ---------------------------------------------------------------------------
// Using a Settings
// ---------------------------------------------------------------------------

// Provider resolves one endpoint and model into the value the client takes.
//
// It is the only place that turns configuration into a request target, so an
// unknown endpoint or model is caught here with a message naming what was
// available, rather than surfacing later as a 404 from a provider.
func (r Settings) Provider(endpoint, model string) (Provider, error) {
	e, ok := r.Providers[endpoint]
	if !ok {
		return Provider{}, fmt.Errorf("%w: no provider %q in the config (have: %s)",
			ErrUnknownProvider, endpoint, join(r.ProviderNames()))
	}

	if e.BaseURL == "" {
		return Provider{}, fmt.Errorf("%w: provider %q has no base_url", ErrInvalidConfig, endpoint)
	}

	if model == "" {
		return Provider{}, fmt.Errorf("%w: no model named for provider %q (have: %s)",
			ErrUnknownModel, endpoint, join(e.ModelNames()))
	}

	opts, ok := e.Models[model]
	if !ok {
		return Provider{}, fmt.Errorf("%w: provider %q has no model %q (have: %s)",
			ErrUnknownModel, endpoint, model, join(e.ModelNames()))
	}

	p := Provider{
		Name:    endpoint,
		BaseURL: e.BaseURL,
		Model:   model,
		APIKey:  e.APIKey,
	}

	if opts.Route != "" {
		p.Extra = routeJSON(opts.Route)
	}

	return p, nil
}

// ProviderNames returns the configured endpoints in a stable order.
func (r Settings) ProviderNames() []string {
	names := make([]string, 0, len(r.Providers))
	for name := range r.Providers {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// NewClient builds the model client for one endpoint and model, with this
// these settings' request options already applied.
//
// This is the one call an embedder needs: config in, working Model out.
func (r Settings) NewClient(endpoint, model string) (Model, error) {
	p, err := r.Provider(endpoint, model)
	if err != nil {
		return nil, err
	}

	settings := r.WithDefaults()

	return NewClient(ClientConfig{
		Provider:         p,
		MaxTokens:        settings.Model.MaxTokens,
		ReasoningEffort:  settings.Model.ReasoningEffort,
		ExcludeReasoning: settings.Model.ExcludeReasoning,
		RequestTimeout:   settings.Model.RequestTimeout.Std(),
		IdleTimeout:      settings.Model.IdleTimeout.Std(),
	})
}

// limits projects the tool section into the flat caps the tools take. Tools
// receive this rather than the whole Settings so none of them can reach the
// provider credentials.
func (t ToolsConfig) limits() Limits {
	return Limits{
		ReadLines:    t.Read.Lines,
		ReadMaxBytes: t.Read.MaxBytes.Int(),

		ExecTimeout:      t.Exec.Timeout.Std(),
		ExecMaxTimeout:   t.Exec.MaxTimeout.Std(),
		ExecOutputBytes:  t.Exec.OutputBytes.Int(),
		ExecTailLines:    t.Exec.TailLines,
		ExecMaxTailLines: t.Exec.MaxTailLines,
		ExecKillGrace:    t.Exec.KillGrace.Std(),

		BashOutputMaxBytes: t.BashOutput.MaxBytes.Int(),

		SearchTimeout:     t.Search.Timeout.Std(),
		SearchMaxMatches:  t.Search.MaxMatches,
		SearchOutputBytes: t.Search.OutputBytes.Int(),
	}
}

// routeJSON encodes an OpenRouter routing hint as the request's "provider"
// field. Hand-built rather than marshalled because the shape is one key and
// the escape rules for a provider name are the ordinary JSON string ones.
func routeJSON(route string) []byte {
	return []byte(`{"order":[` + quoteJSON(route) + `],"allow_fallbacks":true}`)
}

func join(names []string) string {
	if len(names) == 0 {
		return "none configured"
	}

	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}

	return out
}
