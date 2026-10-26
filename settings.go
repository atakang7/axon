package axon

import (
	"fmt"
	"net/url"
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

	// Pruner tunes checkpoints, when the deciding model chooses what stays
	// in context.
	Pruner PrunerSettings `yaml:"pruner"`

	// Explorer bounds the read-only explore tool.
	Explorer ExplorerSettings `yaml:"explorer"`

	// Goals tunes how the conversation's contract is enforced. See GOALS.md.
	Goals GoalSettings `yaml:"goals"`
}

// GoalSettings controls the goal contract. Status and the goal block are
// always on; only the turn-end note can be turned off.
type GoalSettings struct {
	// DisableNudges stops the one-time note sent when the agent is about to
	// end a turn with the goal's criteria unproven.
	DisableNudges bool `yaml:"disable_nudges"`
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

// PruneMode is how early checkpoints fire: the context size at which the
// deciding model is asked to choose what stays. It sets a size, not a
// judgement bar; what the model keeps is always its own call.
type PruneMode string

const (
	// PruneLow checkpoints at 48K tokens.
	PruneLow PruneMode = "low"

	// PruneModerate checkpoints at 32K tokens. The default.
	PruneModerate PruneMode = "moderate"

	// PruneExtreme checkpoints at 20K tokens.
	PruneExtreme PruneMode = "extreme"
)

// PruneModes lists the valid modes, for validation and error messages.
var PruneModes = []PruneMode{PruneLow, PruneModerate, PruneExtreme}

// Valid reports whether m is a known mode.
func (m PruneMode) Valid() bool {
	for _, known := range PruneModes {
		if m == known {
			return true
		}
	}
	return false
}

// PrunerSettings tunes checkpoints: when the deciding model is asked what
// stays in context. See WORKING_CONTEXT.md.
type PrunerSettings struct {
	// Mode sets the checkpoint trigger. See PruneMode. The zero value takes
	// PruneModerate.
	Mode PruneMode `yaml:"mode"`

	// TriggerTokens overrides Mode's trigger: the estimated context size at
	// which a checkpoint is forced before the next request.
	TriggerTokens int `yaml:"trigger_tokens"`

	// GrowthTokens is how far the context must grow beyond what the last
	// checkpoint kept before another one is forced in the same run. It is
	// what lets a large working set stay without a checkpoint every request.
	GrowthTokens int `yaml:"growth_tokens"`

	// MaxTokens caps the checkpoint reply. It covers reasoning as well as the
	// tool call, since providers bill both against it.
	MaxTokens int `yaml:"max_tokens"`

	// Timeout bounds one checkpoint request.
	Timeout Duration `yaml:"timeout"`

	// KeepRuns is how many finished runs stay unfolded; older ones fold to
	// run cards. Zero follows Mode: low 6, moderate 3, extreme 1.
	KeepRuns int `yaml:"keep_runs"`

	// Semantic, Categories, WindowBlocks and FloorTokens configured the free
	// model reviewers removed in v2.7. They are still accepted so existing
	// config files load, and ignored.
	Semantic     bool `yaml:"semantic"`
	Categories   bool `yaml:"categories"`
	WindowBlocks int  `yaml:"window_blocks"`
	FloorTokens  int  `yaml:"floor_tokens"`
}

// ContextWindow is the windowBlocks argument the agent passes to
// ContextMessages: -1, the full working view. Age-based masking was a cheap
// rule deciding relevance; checkpoints decide instead.
func (s PrunerSettings) ContextWindow() int {
	return -1
}

// Trigger is the context size, in estimated tokens, that forces a checkpoint.
func (s PrunerSettings) Trigger() int {
	if s.TriggerTokens > 0 {
		return s.TriggerTokens
	}
	switch s.Mode {
	case PruneLow:
		return 48000
	case PruneExtreme:
		return 20000
	}
	return 32000
}

// ExplorerSettings bounds one explore call. See WORKING_CONTEXT.md.
type ExplorerSettings struct {
	// MaxRounds caps the explorer's model requests before it must report.
	MaxRounds int `yaml:"max_rounds"`

	// MaxTokens caps one explorer reply, reasoning included.
	MaxTokens int `yaml:"max_tokens"`

	// Timeout bounds the whole exploration.
	Timeout Duration `yaml:"timeout"`

	// NoScout turns off the scout: the explorer looking into each new
	// request before the main model starts. See ROLES.md.
	NoScout bool `yaml:"no_scout"`
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
			// One try and ten retries, waiting 5s, 10s, 20s, 40s, then 60s.
			MaxAttempts: 11,
			BackoffCap:  Duration(time.Minute),
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
			GrowthTokens: 12000,
			MaxTokens:    16000,
			Timeout:      Duration(5 * time.Minute),
		},

		Explorer: ExplorerSettings{
			MaxRounds: 16,
			MaxTokens: 8000,
			Timeout:   Duration(10 * time.Minute),
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
	fillInt(&r.Pruner.GrowthTokens, d.Pruner.GrowthTokens)
	fillInt(&r.Pruner.MaxTokens, d.Pruner.MaxTokens)
	fillDuration(&r.Pruner.Timeout, d.Pruner.Timeout)

	fillInt(&r.Explorer.MaxRounds, d.Explorer.MaxRounds)
	fillInt(&r.Explorer.MaxTokens, d.Explorer.MaxTokens)
	fillDuration(&r.Explorer.Timeout, d.Explorer.Timeout)

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
	// OpenRouter's own catalog is the list of valid models there; the config
	// does not have to repeat it for a model to be selectable.
	if !ok && !isOpenRouter(e.BaseURL) {
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

func isOpenRouter(baseURL string) bool {
	u, err := url.Parse(baseURL)
	return err == nil && u.Host == "openrouter.ai"
}

// WithRoute returns settings in which model on endpoint is routed to an
// OpenRouter provider first (route is a provider slug or tag such as
// "wafer/us"), falling back to others when it is unavailable. An empty route
// clears the preference. The receiver is not modified.
func (r Settings) WithRoute(endpoint, model, route string) Settings {
	e, ok := r.Providers[endpoint]
	if !ok {
		return r
	}
	providers := make(map[string]Endpoint, len(r.Providers))
	for k, v := range r.Providers {
		providers[k] = v
	}
	models := make(map[string]ModelOptions, len(e.Models)+1)
	for k, v := range e.Models {
		models[k] = v
	}
	models[model] = ModelOptions{Route: route}
	e.Models = models
	providers[endpoint] = e
	r.Providers = providers
	return r
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

// keptRuns resolves KeepRuns against the prune mode.
func (s PrunerSettings) keptRuns() int {
	if s.KeepRuns > 0 {
		return s.KeepRuns
	}
	switch s.Mode {
	case PruneLow:
		return 6
	case PruneExtreme:
		return 1
	}
	return 3
}
