package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// RotatingWriter writes logs to one file and rotates by max size.
type RotatingWriter struct {
	path       string
	maxBytes   int64
	maxBackups int

	mu   sync.Mutex
	file *os.File
	size int64
}

// NewRotatingWriter creates a size-rotated file writer. The destination
// directory is created so command setup can rely on logging being available.
func NewRotatingWriter(path string, maxBytes int64, maxBackups int) (*RotatingWriter, error) {
	if path == "" {
		return nil, fmt.Errorf("log path is required")
	}
	if maxBytes <= 0 {
		return nil, fmt.Errorf("maxBytes must be positive")
	}
	if maxBackups < 0 {
		return nil, fmt.Errorf("maxBackups must be non-negative")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	writer := &RotatingWriter{path: path, maxBytes: maxBytes, maxBackups: maxBackups}
	if err := writer.openLocked(); err != nil {
		return nil, err
	}
	return writer, nil
}

// Write appends log bytes and rotates before the write when the current file
// plus p would exceed the configured size.
func (writer *RotatingWriter) Write(p []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()

	if writer.file == nil {
		if err := writer.openLocked(); err != nil {
			return 0, err
		}
	}
	if writer.size+int64(len(p)) > writer.maxBytes {
		if err := writer.rotateLocked(); err != nil {
			return 0, err
		}
	}

	n, err := writer.file.Write(p)
	writer.size += int64(n)
	return n, err
}

func (writer *RotatingWriter) openLocked() error {
	file, err := os.OpenFile(writer.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file %q: %w", writer.path, err)
	}
	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return fmt.Errorf("stat log file %q: %w", writer.path, err)
	}
	writer.file = file
	writer.size = stat.Size()
	return nil
}

func (writer *RotatingWriter) rotateLocked() error {
	if writer.file != nil {
		if err := writer.file.Close(); err != nil {
			return fmt.Errorf("close log file before rotate: %w", err)
		}
		writer.file = nil
	}

	rotated := fmt.Sprintf("%s.%s", writer.path, time.Now().UTC().Format("20060102-150405.000000000"))
	if err := os.Rename(writer.path, rotated); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("rotate log file: %w", err)
	}
	if err := writer.openLocked(); err != nil {
		return err
	}
	return writer.cleanupBackupsLocked()
}

func (writer *RotatingWriter) cleanupBackupsLocked() error {
	paths, err := filepath.Glob(writer.path + ".*")
	if err != nil {
		return fmt.Errorf("glob rotated logs: %w", err)
	}

	type fileInfo struct {
		path    string
		modTime time.Time
	}
	infos := make([]fileInfo, 0, len(paths))
	for _, path := range paths {
		stat, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("stat rotated log %q: %w", path, err)
		}
		infos = append(infos, fileInfo{path: path, modTime: stat.ModTime()})
	}
	sort.Slice(infos, func(left, right int) bool {
		return infos[left].modTime.After(infos[right].modTime)
	})
	for index, info := range infos {
		if index < writer.maxBackups {
			continue
		}
		if err := os.Remove(info.path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove old rotated log %q: %w", info.path, err)
		}
	}
	return nil
}
