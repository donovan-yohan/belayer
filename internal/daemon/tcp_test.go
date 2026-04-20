package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTCP_DaemonStartsAndHealthReachable verifies that when a daemon is started
// with a TCPAddr of "127.0.0.1:0", the OS-assigned port is reachable and
// GET /health returns 200 without any auth header (no auth configured yet).
func TestTCP_DaemonStartsAndHealthReachable(t *testing.T) {
	// Darwin limits sun_path to 104 bytes; use /tmp for socket paths.
	socketDir, err := os.MkdirTemp("/tmp", "bl")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(socketDir) })

	socketPath := filepath.Join(socketDir, "d.sock")
	dbPath := filepath.Join(t.TempDir(), "belayer.db")

	d, err := New(Config{
		DBPath:     dbPath,
		SocketPath: socketPath,
		TCPAddr:    "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	serveErr := make(chan error, 1)
	go func() { serveErr <- d.Start(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-serveErr:
		case <-time.After(3 * time.Second):
		}
	})

	// Wait until the unix socket is reachable — at that point Start has already
	// bound the TCP listener and written tcpPort (both happen before Serve blocks).
	ready := false
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		select {
		case startErr := <-serveErr:
			t.Fatalf("Start returned early: %v", startErr)
		default:
		}
		c, dialErr := net.Dial("unix", socketPath)
		if dialErr == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		t.Fatal("daemon never became reachable on its unix socket")
	}

	port := d.TCPPort()
	if port == 0 {
		t.Fatal("TCPPort() returned 0 after daemon is serving")
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /health via TCP: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /health via TCP: got %d, want 200", resp.StatusCode)
	}
}
