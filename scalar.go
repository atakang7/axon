package axon

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// scalar.go defines the two value types the config file uses that YAML has no
// native notion of: a duration and a byte size.
//
// They exist so the file can say `30s` and `2MB` instead of `30` and
// `2097152`. That is not cosmetic. A config file is read far more often than
// it is written, and a bare number forces every reader to remember which unit
// each field is in — which is exactly the kind of thing nobody remembers, and
// exactly how a 30-second timeout becomes a 30-millisecond one.
//
// Both accept a bare number too, in the field's natural unit, so a file
// written without units still loads.

// ---------------------------------------------------------------------------
// Duration
// ---------------------------------------------------------------------------

// Duration is a time.Duration that reads `30s`, `10m`, `1h30m` from YAML. A
// bare number is interpreted as seconds.
type Duration time.Duration

// Std returns the standard library value.
func (d Duration) Std() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

// UnmarshalYAML parses either a duration string or a bare number of seconds.
func (d *Duration) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("line %d: duration must be a string like \"30s\" or a number of seconds", node.Line)
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}

	// A bare number is seconds. This is the forgiving path for a file written
	// before the units existed.
	if n, err := strconv.ParseFloat(raw, 64); err == nil {
		*d = Duration(time.Duration(n * float64(time.Second)))

		return nil
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("line %d: %q is not a duration (want e.g. \"30s\", \"10m\", \"1h30m\")", node.Line, raw)
	}

	if parsed < 0 {
		return fmt.Errorf("line %d: duration %q must not be negative", node.Line, raw)
	}

	*d = Duration(parsed)

	return nil
}

// MarshalYAML writes the human form, so a config round-trips as `30s` rather
// than as a nanosecond count.
func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }

// ---------------------------------------------------------------------------
// Bytes
// ---------------------------------------------------------------------------

// Bytes is a byte count that reads `12KB`, `2MB`, `8MiB` from YAML. A bare
// number is interpreted as bytes.
//
// Both decimal (KB = 1000) and binary (KiB = 1024) suffixes are accepted,
// because people write both and silently meaning the other one is worse than
// accepting both.
type Bytes int64

// Int returns the count as an int, which is what the tool limits take.
func (b Bytes) Int() int { return int(b) }

func (b Bytes) String() string {
	switch {
	case b >= 1<<20 && b%(1<<20) == 0:
		return strconv.FormatInt(int64(b)/(1<<20), 10) + "MiB"

	case b >= 1<<10 && b%(1<<10) == 0:
		return strconv.FormatInt(int64(b)/(1<<10), 10) + "KiB"
	}

	return strconv.FormatInt(int64(b), 10)
}

// byteUnits maps a suffix to its multiplier, longest first so that "KiB" is
// matched before "B" would swallow it.
var byteUnits = []struct {
	suffix string
	scale  int64
}{
	{"KIB", 1 << 10}, {"MIB", 1 << 20}, {"GIB", 1 << 30},
	{"KB", 1000}, {"MB", 1000 * 1000}, {"GB", 1000 * 1000 * 1000},
	{"K", 1 << 10}, {"M", 1 << 20}, {"G", 1 << 30},
	{"B", 1},
}

// UnmarshalYAML parses either a sized string or a bare number of bytes.
func (b *Bytes) UnmarshalYAML(node *yaml.Node) error {
	var raw string
	if err := node.Decode(&raw); err != nil {
		return fmt.Errorf("line %d: size must be a string like \"2MB\" or a number of bytes", node.Line)
	}

	raw = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(raw), " ", ""))
	if raw == "" {
		return nil
	}

	for _, unit := range byteUnits {
		if !strings.HasSuffix(raw, unit.suffix) {
			continue
		}

		digits := strings.TrimSuffix(raw, unit.suffix)
		n, err := strconv.ParseFloat(digits, 64)
		if err != nil {
			return fmt.Errorf("line %d: %q is not a size (want e.g. \"12KB\", \"2MB\")", node.Line, raw)
		}
		if n < 0 {
			return fmt.Errorf("line %d: size %q must not be negative", node.Line, raw)
		}

		*b = Bytes(int64(n * float64(unit.scale)))

		return nil
	}

	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("line %d: %q is not a size (want e.g. \"12KB\", \"2MB\", or a plain byte count)", node.Line, raw)
	}
	if n < 0 {
		return fmt.Errorf("line %d: size %d must not be negative", node.Line, n)
	}

	*b = Bytes(n)

	return nil
}

// MarshalYAML writes the human form.
func (b Bytes) MarshalYAML() (any, error) { return b.String(), nil }
