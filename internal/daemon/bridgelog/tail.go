package bridgelog

import (
	"bufio"
	"log"
	"os"
	"strings"
)

// TailLines returns the last n lines of the file at path as a single string
// with newlines between lines. It returns a sentinel string on any error
// (including ENOENT) and on an empty file, with debug logging.
//
// Implementation uses a ring-buffer over a bufio.Scanner so it handles
// files of any size without loading the full content into memory; for
// the typical bridge-stderr.log (<10 MB) this is more than adequate.
func TailLines(path string, n int) string {
	if n <= 0 {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("bridgelog.TailLines: %s: file not found", path)
		} else {
			log.Printf("bridgelog.TailLines: %s: open: %v", path, err)
		}
		return "(stderr log missing or empty)"
	}
	defer f.Close()

	// Ring buffer of the last n lines seen.
	ring := make([]string, n)
	head := 0  // next write position
	count := 0 // total lines seen

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		ring[head%n] = scanner.Text()
		head++
		count++
	}
	if err := scanner.Err(); err != nil {
		log.Printf("bridgelog.TailLines: %s: scan: %v", path, err)
		return "(stderr log missing or empty)"
	}
	if count == 0 {
		log.Printf("bridgelog.TailLines: %s: file is empty", path)
		return "(stderr log missing or empty)"
	}

	// Reconstruct in order.
	take := count
	if take > n {
		take = n
	}
	lines := make([]string, take)
	start := head - take
	for i := 0; i < take; i++ {
		lines[i] = ring[(start+i)%n]
	}
	return strings.Join(lines, "\n")
}
