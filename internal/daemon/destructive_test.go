package daemon

import "testing"

func TestDetectDestructive(t *testing.T) {
	positives := []struct {
		cmd  string
		kind string
	}{
		// rm-rf variants
		{"rm -rf /tmp/foo", "rm-rf"},
		{"rm -Rf /tmp/foo", "rm-rf"},
		{"rm -fr /tmp/foo", "rm-rf"},
		{"rm -fR /workspace/.belayer", "rm-rf"},
		{"sudo rm -rf /", "rm-rf"},
		// git reset --hard
		{"git reset --hard", "git-reset-hard"},
		{"git reset --hard HEAD~1", "git-reset-hard"},
		{"GIT reset --hard", "git-reset-hard"}, // case-insensitive
		// git force-push
		{"git push --force", "git-force-push"},
		{"git push -f", "git-force-push"},
		{"git push origin main --force", "git-force-push"},
		{"git push --force-with-lease", "git-force-push"},
		// git clean
		{"git clean -f", "git-clean"},
		{"git clean -fd", "git-clean"},
		{"git clean -fx", "git-clean"},
		{"git clean -fdx", "git-clean"},
		// sql-drop
		{"DROP TABLE users", "sql-drop"},
		{"DROP DATABASE mydb", "sql-drop"},
		{"TRUNCATE TABLE sessions", "sql-drop"},
		{"drop table foo", "sql-drop"}, // lower case
		// dd to device
		{"dd if=/dev/zero of=/dev/sda", "dd-to-device"},
		{"dd if=disk.img of=/dev/sdb bs=4M", "dd-to-device"},
	}

	for _, tc := range positives {
		kind, ok := DetectDestructive(tc.cmd)
		if !ok {
			t.Errorf("expected match for %q, got no match", tc.cmd)
			continue
		}
		if kind != tc.kind {
			t.Errorf("cmd=%q: expected kind %q, got %q", tc.cmd, tc.kind, kind)
		}
	}

	negatives := []string{
		// rm without -r flag
		"rm file.txt",
		"rm -f file.txt",
		// git reset without --hard
		"git reset",
		"git reset HEAD",
		"git reset --soft HEAD~1",
		// git push without force
		"git push origin main",
		"git push",
		// git clean without -f
		"git clean -n",
		// dd writing to a file, not a device
		"dd if=/dev/zero of=disk.img bs=4M count=1",
		// SQL that is not DROP/TRUNCATE
		"SELECT * FROM users",
		"CREATE TABLE foo (id int)",
		// Quoted strings (known false-negative; accepted trade-off)
		// The following would be a false-positive — document that here.
		// "echo 'rm -rf /'" would match; that is the accepted limitation.
	}

	for _, cmd := range negatives {
		_, ok := DetectDestructive(cmd)
		if ok {
			t.Errorf("unexpected match for negative case %q", cmd)
		}
	}
}
