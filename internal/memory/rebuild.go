package memory

import "fmt"

// RebuildIndex reads all markdown files from the MemFS and populates
// the SQLite FTS5 index. Called on fresh workspace clone to restore
// the derived index from the authoritative markdown files.
func RebuildIndex(fs *MemFS, store *SQLiteMemory) error {
	repos, err := fs.ListRepos()
	if err != nil {
		return fmt.Errorf("rebuild: list repos: %w", err)
	}

	for _, repo := range repos {
		// Rebuild core entries.
		coreEntries, err := fs.ReadCoreFile(repo)
		if err != nil {
			return fmt.Errorf("rebuild: read core for repo %q: %w", repo, err)
		}
		for _, e := range coreEntries {
			if err := store.WriteCore(e.SessionID, e.Key, e.Value); err != nil {
				return fmt.Errorf("rebuild: write core entry (repo=%q key=%q): %w", repo, e.Key, err)
			}
		}

		// Rebuild archival entries.
		archivalEntries, err := fs.ReadArchivalFiles(repo)
		if err != nil {
			return fmt.Errorf("rebuild: read archival for repo %q: %w", repo, err)
		}
		for _, e := range archivalEntries {
			if err := store.WriteArchival(e.SessionID, e.Content, e.Tags, e.Source); err != nil {
				return fmt.Errorf("rebuild: write archival entry (repo=%q): %w", repo, err)
			}
		}
	}

	return nil
}
