package daemon

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/donovan-yohan/belayer/internal/store"
)

// writeEventHeaders sets the standard Belayer response headers on event-returning
// endpoints. It is safe to call before WriteHeader since it only modifies w.Header().
//
// Headers set:
//   - X-Belayer-Schema: belayer-log/v1 (always)
//   - X-Last-Event-Id: global max event id at response time (omitted on store error)
//   - X-Event-Count: number of events in this response
//   - X-Session-Status: session status string (omitted on store error or unknown session)
//   - X-Log-Level: session log_level (omitted on store error or unknown session)
//   - X-Agent-Roster: comma-separated agent names (omitted on store error)
func (d *Daemon) writeEventHeaders(w http.ResponseWriter, sessionID string, eventCount int) {
	w.Header().Set("X-Belayer-Schema", "belayer-log/v1")

	if lastID, err := d.store.MaxEventID(); err == nil {
		w.Header().Set("X-Last-Event-Id", strconv.FormatInt(lastID, 10))
	}

	w.Header().Set("X-Event-Count", strconv.Itoa(eventCount))

	if sess, err := d.store.GetSession(sessionID); err == nil {
		w.Header().Set("X-Session-Status", sess.Status)
		w.Header().Set("X-Log-Level", sess.LogLevel)
	}

	if roster, err := d.rosterCSV(sessionID); err == nil {
		w.Header().Set("X-Agent-Roster", roster)
	}
}

// rosterCSV returns a comma-separated list of agent names for the session.
func (d *Daemon) rosterCSV(sessionID string) (string, error) {
	runs, err := d.store.ListAgentRuns(sessionID)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(runs))
	for _, r := range runs {
		names = append(names, r.Name)
	}
	return strings.Join(names, ","), nil
}

// wantsCompactTSV reports whether the request should be served in compact
// TSV form. ?format=compact always wins (query-param precedence); otherwise
// we accept an Accept header that includes text/tab-separated-values. Any
// other value (including an explicit ?format=json) forces JSON.
func wantsCompactTSV(r *http.Request) bool {
	switch r.URL.Query().Get("format") {
	case "compact":
		return true
	case "":
		// fall through to Accept negotiation
	default:
		return false
	}
	accept := r.Header.Get("Accept")
	if accept == "" {
		return false
	}
	for _, part := range strings.Split(accept, ",") {
		// Strip q= and other parameters.
		mediaType := strings.TrimSpace(part)
		if semi := strings.Index(mediaType, ";"); semi >= 0 {
			mediaType = strings.TrimSpace(mediaType[:semi])
		}
		if strings.EqualFold(mediaType, "text/tab-separated-values") {
			return true
		}
	}
	return false
}

// writeCompactTSV writes events as a compact TSV response (one line per event).
// Format: <id>\t<agent>\t<type>\t<summary>\n
// summary is the first 120 chars of Data with literal \n → \\n and \t → \\t.
// Content-Type: text/tab-separated-values; charset=utf-8
func writeCompactTSV(w http.ResponseWriter, events []store.SessionEvent) {
	w.Header().Set("Content-Type", "text/tab-separated-values; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	for _, evt := range events {
		agent := extractAgentName(evt.Data)
		if agent == "unknown" {
			agent = ""
		}
		summary := compactSummary(evt.Data)
		line := fmt.Sprintf("%d\t%s\t%s\t%s\n",
			evt.ID,
			escapeTSVField(agent),
			escapeTSVField(evt.Type),
			escapeTSVField(summary),
		)
		fmt.Fprint(w, line)
	}
}

// compactSummary returns the first 120 characters of data with embedded newlines
// and tabs escaped, and trailing whitespace trimmed.
func compactSummary(data string) string {
	runes := []rune(data)
	if len(runes) > 120 {
		runes = runes[:120]
	}
	s := string(runes)
	s = strings.TrimRight(s, " \t\n\r")
	return s
}

// escapeTSVField replaces literal newlines and tabs in a TSV field value so the
// output remains valid tab-separated format.
func escapeTSVField(s string) string {
	s = strings.ReplaceAll(s, "\t", `\t`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}
