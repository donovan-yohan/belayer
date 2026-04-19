package daemon

import (
	"net/http"
	"strconv"
	"strings"
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
