package cli

import _ "modernc.org/sqlite"

// SQLite is linked into the v6 baseline so later phases can add a local-first
// runtime without reintroducing the previous Temporal stack.
