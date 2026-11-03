package userconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// ErrUnknownUser is returned when no file in force belongs to the User.
var ErrUnknownUser = errors.New("no file belongs to this User")

// ErrConflict is returned by Save when the file changed on disk after the text it is based on was read.
var ErrConflict = errors.New("the file changed on disk in the meantime")

// File is the file of one User as it is on disk.
type File struct {
	// Name is the name of the file in the users folder.
	Name string
	// Text is the content of the file, comments included.
	Text string
	// Hash identifies the content; Save refuses to overwrite a file whose content has another one.
	Hash string
}

// hashOf identifies a content.
func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// pathOf is the file that holds the User in force.
func (s *Store) pathOf(userID string) (string, bool) {
	for path, user := range s.lastValid {
		if user.ID == userID {
			return path, true
		}
	}
	return "", false
}

// File reads the file of the User as it is on disk now.
func (s *Store) File(userID string) (File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, ok := s.pathOf(userID)
	if !ok {
		return File{}, ErrUnknownUser
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read the file of %s: %w", userID, err)
	}
	return File{Name: filepath.Base(path), Text: string(data), Hash: hashOf(data)}, nil
}

// Save replaces the file of the User with the text, if that is a valid User Config of this very User and the file on
// disk still has the content with the hash baseHash. Invalid text is returned as Problems with their positions and is
// never written; a file that changed meanwhile is an ErrConflict. A valid text is written atomically and is in force
// when Save returns, and subscribers are told.
func (s *Store) Save(userID, text, baseHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, ok := s.pathOf(userID)
	if !ok {
		return ErrUnknownUser
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read the file of %s: %w", userID, err)
	}
	if hashOf(current) != baseHash {
		slog.Warn("Saving refused, the file changed on disk", "user", userID, "file", path)
		return ErrConflict
	}
	if problems := s.check(path, userID, []byte(text)); len(problems) > 0 {
		slog.Info("Saving refused, the text is not a valid User Config", "user", userID, "problems", len(problems))
		return problems
	}
	if err := writeAtomically(path, []byte(text)); err != nil {
		return fmt.Errorf("write the file of %s: %w", userID, err)
	}
	slog.Info("User Config saved", "user", userID, "file", path, "bytes", len(text))
	s.reloadLocked()
	return nil
}

// check validates the text as the content of the file, among the other files, and returns the problems found in it.
func (s *Store) check(path, userID string, text []byte) Problems {
	results, err := readFiles(s.dir)
	if err != nil {
		return Problems{{File: path, Message: err.Error()}}
	}
	parsedText, problems := parseFile(path, text)
	if len(problems) == 0 && parsedText.user.ID != userID {
		problems = Problems{{File: path, Line: parsedText.idPos.line, Column: parsedText.idPos.column,
			Message: fmt.Sprintf("the User Id %q cannot be changed here, the file belongs to %q", parsedText.user.ID, userID)}}
	}
	for i := range results {
		if results[i].path == path {
			results[i] = fileResult{path: path, parsed: parsedText, problems: problems}
		}
	}
	_, all, _ := assemble(results, s.lastValid)
	var found Problems
	for _, p := range all {
		if p.File == path {
			found = append(found, p)
		}
	}
	return found
}

// writeAtomically replaces the file in one step: the new content is written next to it and renamed over it.
func writeAtomically(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp.Name()) }()
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
