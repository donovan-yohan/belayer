package daemon

import "fmt"

// LogLevelStandard is the default log level for new sessions.
const LogLevelStandard = "standard"

// LogLevelVerbose enables verbose logging for a session.
const LogLevelVerbose = "verbose"

// LogLevelTrace is the highest log tier: superset of verbose, includes
// untruncated tool payloads and trace:* event types.
const LogLevelTrace = "trace"

// DefaultLogLevel is the fallback when no explicit level is provided.
const DefaultLogLevel = LogLevelStandard

// ValidateLogLevel checks that s is a recognised log level. Empty string is
// treated as an alias for DefaultLogLevel and returns (DefaultLogLevel, nil).
// Unknown values return an error naming the allowed set.
func ValidateLogLevel(s string) (string, error) {
	switch s {
	case "":
		return DefaultLogLevel, nil
	case LogLevelStandard, LogLevelVerbose, LogLevelTrace:
		return s, nil
	default:
		return "", fmt.Errorf("invalid log_level %q: must be one of [standard, verbose, trace]", s)
	}
}

// ResolveLogLevel applies the three-tier resolution order:
//  1. explicit (from POST /sessions body) — validated and returned if non-empty.
//  2. configDefault (from Config.DefaultLogLevel) — validated and returned if non-empty.
//  3. DefaultLogLevel as the final fallback.
func ResolveLogLevel(explicit, configDefault string) (string, error) {
	if explicit != "" {
		return ValidateLogLevel(explicit)
	}
	if configDefault != "" {
		return ValidateLogLevel(configDefault)
	}
	return DefaultLogLevel, nil
}
