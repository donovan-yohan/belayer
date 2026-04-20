package daemon

import (
	"regexp"
	"strings"
)

var (
	// keyRegex matches JSON fields whose key is a common secret name.
	keyRegex = regexp.MustCompile(`(?i)"(api[_-]?key|authorization|bearer|password|secret|token|credential)"\s*:\s*"[^"]*"`)
	// openaiRegex matches OpenAI-style sk-... tokens (including sk-ant-* and sk-proj-*) outside JSON.
	openaiRegex = regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}`)
	// bearerRegex matches free-form Bearer tokens case-insensitively, allowing whitespace variants.
	bearerRegex = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`)

	// githubTokenRegex matches GitHub classic personal access tokens (ghp_, gho_, ghu_, ghs_, ghr_).
	githubTokenRegex = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{36,}`)
	// githubFineGrainedPATRegex matches GitHub fine-grained personal access tokens.
	githubFineGrainedPATRegex = regexp.MustCompile(`github_pat_[A-Za-z0-9_]{82,}`)
	// awsKeyRegex matches AWS IAM access keys (AKIA) and STS session keys (ASIA).
	awsKeyRegex = regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`)
	// jwtRegex matches JWT tokens by their base64-encoded header prefix.
	jwtRegex = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
	// pemPrivateKeyRegex matches PEM-encoded private key blocks (multiline via (?s) dot-all).
	pemPrivateKeyRegex = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]+PRIVATE KEY-----[\s\S]*?-----END [A-Z ]+PRIVATE KEY-----`)
	// slackTokenRegex matches Slack API tokens (bot, app, user, workspace, legacy).
	slackTokenRegex = regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{20,}`)
	// googleAPIKeyRegex matches Google API keys.
	googleAPIKeyRegex = regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)
)

// Scrub returns s with common secret material replaced by "<redacted>".
// It is intentionally conservative: it only rewrites matches for known key
// names and well-known token prefixes so non-secret fields pass through
// unchanged. Safe to call on arbitrary strings (JSON, plain text, logs).
func Scrub(s string) string {
	s = keyRegex.ReplaceAllStringFunc(s, func(m string) string {
		i := strings.Index(m, ":")
		if i < 0 {
			return m
		}
		return m[:i+1] + `"<redacted>"`
	})
	s = openaiRegex.ReplaceAllString(s, "<redacted>")
	s = bearerRegex.ReplaceAllString(s, "Bearer <redacted>")
	s = githubTokenRegex.ReplaceAllString(s, "<redacted>")
	s = githubFineGrainedPATRegex.ReplaceAllString(s, "<redacted>")
	s = awsKeyRegex.ReplaceAllString(s, "<redacted>")
	s = jwtRegex.ReplaceAllString(s, "<redacted>")
	s = pemPrivateKeyRegex.ReplaceAllString(s, "<redacted>")
	s = slackTokenRegex.ReplaceAllString(s, "<redacted>")
	s = googleAPIKeyRegex.ReplaceAllString(s, "<redacted>")
	return s
}
