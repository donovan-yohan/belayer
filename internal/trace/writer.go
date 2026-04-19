package trace

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// RotateThreshold is the fragment size threshold in bytes (exported for tests).
const RotateThreshold = 128 * 1024 * 1024

// rotateThreshold is the internal variable used for size checks; tests may
// override it via SetRotateThresholdForTest.
var rotateThreshold int64 = RotateThreshold

// SetRotateThresholdForTest overrides the rotation threshold and returns the
// previous value so the caller can restore it via defer.
func SetRotateThresholdForTest(newVal int64) int64 {
	old := rotateThreshold
	rotateThreshold = newVal
	return old
}

// Fragment locates a single appended payload inside a fragment file.
type Fragment struct {
	Path   string // absolute path of the fragment file at write time
	Offset int64  // byte offset in the file where the payload begins (before the newline)
	Length int64  // length of the payload in bytes (not counting the trailing newline)
}

// Writer is the per-daemon trace spill writer. A single Writer serves all
// (session, agent) pairs; callers identify the target with Append args.
type Writer interface {
	Append(sessionID, agentName string, payload []byte) (Fragment, error)
	CloseAgent(sessionID, agentName string) error // seals and zstd-compresses the current fragment
	Close() error                                  // seals all open fragments
}

// agentFragment tracks the state of the active fragment for one (session, agent) pair.
type agentFragment struct {
	dir           string // baseDir/session/agent
	currentPath   string
	currentFile   *os.File
	size          int64
	fragmentIndex int // 1-based: 0001, 0002, …

	// sealWg tracks in-flight async compression goroutines so CloseAgent can
	// wait for them to finish before returning.
	sealWg sync.WaitGroup
}

type fsWriter struct {
	mu       sync.Mutex
	baseDir  string
	fragments map[string]*agentFragment // key: sessionID + "\x00" + agentName
}

// NewWriter returns a filesystem-backed Writer rooted at baseDir.
// Files live under baseDir/<sessionID>/<agentName>/<NNNN>.jsonl (active)
// or .jsonl.zst (sealed).
func NewWriter(baseDir string) (Writer, error) {
	return &fsWriter{
		baseDir:   baseDir,
		fragments: make(map[string]*agentFragment),
	}, nil
}

// fragmentKey builds the map key for a (sessionID, agentName) pair.
func fragmentKey(sessionID, agentName string) string {
	return sessionID + "\x00" + agentName
}

// fragmentName returns the zero-padded file name for the given 1-based index.
func fragmentName(index int) string {
	return fmt.Sprintf("%04d.jsonl", index)
}

// scanHighestIndex scans dir for existing fragment files and returns the
// highest index found. Returns 0 if no fragments exist yet.
func scanHighestIndex(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	highest := 0
	for _, e := range entries {
		name := e.Name()
		// Match files like 0001.jsonl (active) or 0001.jsonl.zst (sealed).
		base := strings.TrimSuffix(name, ".zst")
		base = strings.TrimSuffix(base, ".jsonl")
		n, err := strconv.Atoi(base)
		if err != nil {
			continue
		}
		if n > highest {
			highest = n
		}
	}
	return highest, nil
}

// openOrCreateFragment opens (for append) the fragment at the given index inside
// dir, applies partial-tail truncation, and returns the agentFragment.
func openOrCreateFragment(dir string, index int) (*agentFragment, error) {
	path := filepath.Join(dir, fragmentName(index))

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open fragment %q: %w", path, err)
	}

	size, err := partialTailTruncate(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("truncate fragment %q: %w", path, err)
	}

	return &agentFragment{
		dir:           dir,
		currentPath:   path,
		currentFile:   f,
		size:          size,
		fragmentIndex: index,
	}, nil
}

// partialTailTruncate inspects f for an incomplete (unterminated) last record,
// truncates up to and including the last '\n', and returns the resulting file
// size. The file must be opened for read+write.
//
// Recovery scans backward through the file in 64 KB chunks until a newline is
// found or BOF is reached. If no newline exists in the entire file the file is
// truncated to 0 (one giant partial record).
func partialTailTruncate(f *os.File) (int64, error) {
	const chunk = 64 * 1024 // 64 KB

	fileSize, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, err
	}
	if fileSize == 0 {
		return 0, nil
	}

	// Scan backward in chunk-sized windows until we find a '\n' or reach BOF.
	pos := fileSize
	for pos > 0 {
		readFrom := pos - chunk
		if readFrom < 0 {
			readFrom = 0
		}
		buf := make([]byte, pos-readFrom)
		if _, err := f.ReadAt(buf, readFrom); err != nil {
			return 0, err
		}

		if idx := bytes.LastIndexByte(buf, '\n'); idx >= 0 {
			keepTo := readFrom + int64(idx) + 1
			if keepTo == fileSize {
				// File ends exactly on a newline — nothing to truncate.
				if _, err := f.Seek(0, io.SeekEnd); err != nil {
					return 0, err
				}
				return fileSize, nil
			}
			if err := f.Truncate(keepTo); err != nil {
				return 0, err
			}
			if _, err := f.Seek(0, io.SeekEnd); err != nil {
				return 0, err
			}
			return keepTo, nil
		}

		pos = readFrom
	}

	// No newline found in the entire file — one giant partial record. Truncate to 0.
	if err := f.Truncate(0); err != nil {
		return 0, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return 0, err
	}
	return 0, nil
}

// getOrCreateAgentFragment returns the agentFragment for (sessionID, agentName),
// creating it if it does not yet exist. Must be called with w.mu held.
func (w *fsWriter) getOrCreateAgentFragment(sessionID, agentName string) (*agentFragment, error) {
	key := fragmentKey(sessionID, agentName)
	if af, ok := w.fragments[key]; ok {
		return af, nil
	}

	dir := filepath.Join(w.baseDir, sessionID, agentName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir %q: %w", dir, err)
	}

	highest, err := scanHighestIndex(dir)
	if err != nil {
		return nil, err
	}
	index := highest
	if index == 0 {
		index = 1
	}

	// If the highest fragment is a sealed .zst, start a new one.
	if highest > 0 {
		sealedPath := filepath.Join(dir, fmt.Sprintf("%04d.jsonl.zst", highest))
		if _, err := os.Stat(sealedPath); err == nil {
			index = highest + 1
		}
	}

	af, err := openOrCreateFragment(dir, index)
	if err != nil {
		return nil, err
	}

	w.fragments[key] = af
	return af, nil
}

// seal closes the current file and asynchronously compresses it.
// The caller must hold w.mu when entering; seal releases the file handle
// immediately and fires a goroutine for the compression.
func (w *fsWriter) seal(af *agentFragment) error {
	if af.currentFile == nil {
		return nil
	}

	path := af.currentPath
	f := af.currentFile
	af.currentFile = nil

	// Close the file before compressing.
	if err := f.Close(); err != nil {
		return fmt.Errorf("close before seal: %w", err)
	}

	af.sealWg.Add(1)
	go func() {
		defer af.sealWg.Done()
		_ = compressFragment(path) // best-effort; log errors if needed
	}()

	return nil
}

// compressFragment compresses src (.jsonl) to src+".zst", then removes src.
func compressFragment(src string) error {
	tmpPath := src + ".zst.tmp"

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("open tmp: %w", err)
	}

	enc, err := zstd.NewWriter(out)
	if err != nil {
		out.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("zstd.NewWriter: %w", err)
	}

	if _, err := io.Copy(enc, in); err != nil {
		enc.Close()
		out.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("copy: %w", err)
	}
	if err := enc.Close(); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("enc.Close: %w", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close tmp: %w", err)
	}

	finalPath := src + ".zst"
	if err := os.Rename(tmpPath, finalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	if err := os.Remove(src); err != nil {
		return fmt.Errorf("remove src: %w", err)
	}

	return nil
}

// Append writes payload as a newline-terminated record to the fragment for
// (sessionID, agentName) and returns a Fragment describing its location.
func (w *fsWriter) Append(sessionID, agentName string, payload []byte) (Fragment, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	af, err := w.getOrCreateAgentFragment(sessionID, agentName)
	if err != nil {
		return Fragment{}, err
	}

	// Rotate if the current fragment has reached the threshold.
	if af.size >= rotateThreshold {
		if err := w.seal(af); err != nil {
			return Fragment{}, fmt.Errorf("seal before rotate: %w", err)
		}
		af.fragmentIndex++
		newPath := filepath.Join(af.dir, fragmentName(af.fragmentIndex))
		nf, err := os.OpenFile(newPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
		if err != nil {
			return Fragment{}, fmt.Errorf("open new fragment: %w", err)
		}
		af.currentPath = newPath
		af.currentFile = nf
		af.size = 0
	}

	offset := af.size

	// Write payload + newline.
	record := make([]byte, len(payload)+1)
	copy(record, payload)
	record[len(payload)] = '\n'

	if _, err := af.currentFile.Write(record); err != nil {
		return Fragment{}, fmt.Errorf("write record: %w", err)
	}
	af.size += int64(len(record))

	return Fragment{
		Path:   af.currentPath,
		Offset: offset,
		Length: int64(len(payload)),
	}, nil
}

// CloseAgent seals the current fragment for (sessionID, agentName),
// waits for compression to complete, and removes the entry from the map.
func (w *fsWriter) CloseAgent(sessionID, agentName string) error {
	w.mu.Lock()
	key := fragmentKey(sessionID, agentName)
	af, ok := w.fragments[key]
	if !ok {
		w.mu.Unlock()
		return nil
	}
	delete(w.fragments, key)

	if err := w.seal(af); err != nil {
		w.mu.Unlock()
		return err
	}
	w.mu.Unlock()

	// Wait for the async compression goroutine to finish.
	af.sealWg.Wait()
	return nil
}

// Close seals all open fragments and waits for all compressions to complete.
func (w *fsWriter) Close() error {
	w.mu.Lock()
	afs := make([]*agentFragment, 0, len(w.fragments))
	for _, af := range w.fragments {
		afs = append(afs, af)
	}
	w.fragments = make(map[string]*agentFragment)

	var firstErr error
	for _, af := range afs {
		if err := w.seal(af); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	w.mu.Unlock()

	// Wait for all in-flight compressions.
	for _, af := range afs {
		af.sealWg.Wait()
	}
	return firstErr
}
