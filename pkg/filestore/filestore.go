// Package filestore is the business.AudioStore that keeps audio as files in a folder of the data folder.
package filestore

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/alex0ptr/aigude/business"
)

// episodesDir is the folder inside the data folder that holds the audio files.
const episodesDir = "episodes"

// Files keeps audio as one flat file per key under <dataDir>/episodes. A location is the path of the file relative
// to the data folder.
type Files struct {
	dataDir string
}

var _ business.AudioStore = (*Files)(nil)

// New returns a Files store below the data folder.
func New(dataDir string) *Files {
	return &Files{dataDir: dataDir}
}

// path is the file of a location in the data folder.
func (f *Files) path(location string) string {
	return filepath.Join(f.dataDir, location)
}

// Store writes the audio to a file named after the key and returns its location.
func (f *Files) Store(key string, audio []byte) (string, error) {
	location := filepath.Join(episodesDir, key)
	path := f.path(location)
	slog.Debug("Writing audio file", "path", path, "bytes", len(audio))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("store audio %s: %w", key, err)
	}
	if err := os.WriteFile(path, audio, 0o644); err != nil {
		_ = f.Delete(location)
		return "", fmt.Errorf("store audio %s: %w", key, err)
	}
	return location, nil
}

// Open opens the audio file of a location for reading and seeking.
func (f *Files) Open(location string) (business.AudioReader, time.Time, error) {
	path := f.path(location)
	slog.Debug("Opening audio file", "path", path)
	file, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("open audio %s: %w", location, err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, time.Time{}, fmt.Errorf("open audio %s: %w", location, err)
	}
	return file, info.ModTime(), nil
}

// Delete removes the audio file of a location. A file that is already gone is not an error.
func (f *Files) Delete(location string) error {
	path := f.path(location)
	slog.Debug("Removing audio file", "path", path)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete audio %s: %w", location, err)
	}
	return nil
}
