package session

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxStdoutCapture is the maximum bytes captured from stdout for gate nodes.
// Prevents unbounded memory growth from broken or malicious agent output.
const maxStdoutCapture = 10 * 1024 * 1024 // 10 MB

// limitedBuffer captures up to maxStdoutCapture bytes, silently discarding the rest.
type limitedBuffer struct {
	buf bytes.Buffer
}

func (lb *limitedBuffer) Write(p []byte) (int, error) {
	remaining := maxStdoutCapture - lb.buf.Len()
	if remaining <= 0 {
		return len(p), nil // accept but discard
	}
	if len(p) > remaining {
		p = p[:remaining]
	}
	lb.buf.Write(p)
	return len(p), nil
}

func (lb *limitedBuffer) Bytes() []byte { return lb.buf.Bytes() }

// ExecSpawner implements Spawner by executing a shell command.
type ExecSpawner struct{}

// Spawn executes opts.Command via sh -c in the background. It sets BELAYER_*
// environment variables and returns a channel that receives a SpawnResult
// when the process exits.
func (e *ExecSpawner) Spawn(ctx context.Context, opts SpawnOpts) (<-chan SpawnResult, error) {
	if opts.Command == "" {
		return nil, fmt.Errorf("node %q: command is empty", opts.NodeName)
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", opts.Command)
	cmd.Dir = opts.WorkDir
	cmd.Cancel = func() error {
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = 10 * time.Second

	cmd.Env = append(os.Environ(),
		"BELAYER_TASK_ID="+opts.TaskID,
		"BELAYER_NODE="+opts.NodeName,
		"BELAYER_ATTEMPT="+strconv.Itoa(opts.Attempt),
		"BELAYER_WORK_DIR="+opts.WorkDir,
	)

	var stderrBuf bytes.Buffer
	var stdoutBuf limitedBuffer

	// Open log file for tailing if configured.
	var logFile *os.File
	if opts.LogFile != "" {
		if err := os.MkdirAll(filepath.Dir(opts.LogFile), 0o755); err != nil {
			return nil, fmt.Errorf("create log dir for node %q: %w", opts.NodeName, err)
		}
		var err error
		logFile, err = os.Create(opts.LogFile)
		if err != nil {
			return nil, fmt.Errorf("create log file for node %q: %w", opts.NodeName, err)
		}
	}

	stdoutWriters := []io.Writer{os.Stdout}
	stderrWriters := []io.Writer{os.Stderr, &stderrBuf}
	if opts.CaptureStdout {
		stdoutWriters = append(stdoutWriters, &stdoutBuf)
	}
	if logFile != nil {
		stdoutWriters = append(stdoutWriters, logFile)
		stderrWriters = append(stderrWriters, logFile)
	}
	cmd.Stdout = io.MultiWriter(stdoutWriters...)
	cmd.Stderr = io.MultiWriter(stderrWriters...)

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return nil, fmt.Errorf("start command for node %q: %w", opts.NodeName, err)
	}

	exitCh := make(chan SpawnResult, 1)
	go func() {
		defer func() {
			if logFile != nil {
				logFile.Close()
			}
		}()
		err := cmd.Wait()
		var exitErr error
		if err != nil {
			stderr := strings.TrimSpace(stderrBuf.String())
			if stderr != "" {
				exitErr = fmt.Errorf("node %q command exited: %w\nstderr: %s", opts.NodeName, err, stderr)
			} else {
				exitErr = fmt.Errorf("node %q command exited: %w", opts.NodeName, err)
			}
		}
		result := SpawnResult{
			Error:  exitErr,
			Stderr: stderrBuf.Bytes(),
		}
		if opts.CaptureStdout {
			result.Stdout = stdoutBuf.Bytes()
		}
		exitCh <- result
		close(exitCh)
	}()

	return exitCh, nil
}
