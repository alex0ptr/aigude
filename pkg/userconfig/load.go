package userconfig

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// LoadDir reads every .toml file directly in dir as the User Config of one
// User. Subfolders, hidden files and editor leftovers (anything not ending in
// .toml) are ignored. All files are checked and all problems returned
// together as Problems; the result is all or nothing.
func LoadDir(dir string) (*Snapshot, error) {
	snapshot, _, err := loadStrict(dir)
	return snapshot, err
}

// loadStrict is LoadDir that also returns the User each file contributes.
func loadStrict(dir string) (*Snapshot, map[string]User, error) {
	results, err := readFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	users, problems, userByFile := assemble(results, nil)
	if len(problems) > 0 {
		slog.Debug("User Config is invalid", "problems", len(problems))
		return nil, nil, problems
	}
	return newSnapshot(users, nil), userByFile, nil
}

// fileResult is what reading one file yielded.
type fileResult struct {
	path     string
	parsed   parsed
	problems Problems
}

// readFiles reads and parses every config file of dir, ordered by file name.
func readFiles(dir string) ([]fileResult, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("users folder %s: %w", dir, err)
	}

	var names []string
	for _, e := range entries {
		if isConfigFile(dir, e) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	slog.Debug("Loading User Config files", "dir", dir, "files", len(names))

	results := make([]fileResult, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			results = append(results, fileResult{path: path, problems: Problems{{File: path, Message: err.Error()}}})
			continue
		}
		parsed, problems := parseFile(path, data)
		if len(problems) > 0 {
			slog.Debug("User Config file has problems", "file", path, "problems", len(problems))
		}
		results = append(results, fileResult{path: path, parsed: parsed, problems: problems})
	}
	return results, nil
}

// assemble turns the files into the Users that are in force and the problems
// to report, in file order.
//
// Only a file without problems claims its User Id; a file that comes second
// with the same Id gets a problem for it. A file that has problems claims
// nothing, so a typo in one file can take no other User's Id. It stays in
// force with its entry from lastValid, if there is one, until another file has
// taken that Id; then it gets the duplicate reported. Files that already held
// their Id claim it first, so a new file cannot push out an existing User.
//
// userByFile maps each file path to the User it contributes, to be handed
// back as lastValid on the next run.
func assemble(results []fileResult, lastValid map[string]User) (users []User, problems Problems, userByFile map[string]User) {
	ownerOfID := claimIDs(results, lastValid)
	fileProblems := make([]Problems, len(results))
	for i, r := range results {
		fileProblems[i] = r.problemsWithDuplicateID(ownerOfID)
	}

	userByFile = map[string]User{}
	for i, r := range results {
		if len(fileProblems[i]) == 0 {
			users = append(users, r.parsed.user)
			userByFile[r.path] = r.parsed.user
			continue
		}
		problems = append(problems, fileProblems[i]...)
		last, ok := lastValid[r.path]
		if !ok {
			slog.Debug("Invalid file has no earlier valid setup to keep", "file", r.path)
			continue
		}
		if other, taken := ownerOfID[last.ID]; taken && other != r.path {
			slog.Warn("Dropping the last valid setup of a file, its User Id is taken by another file now",
				"file", r.path, "user", last.ID, "takenBy", other)
			continue
		}
		slog.Debug("Keeping the last valid setup of an invalid file", "file", r.path, "user", last.ID)
		ownerOfID[last.ID] = r.path
		users = append(users, last)
		userByFile[r.path] = last
	}
	return users, problems, userByFile
}

// claimIDs lets the files without problems claim their User Id and returns
// the path that owns each Id. Files that held their Id the last time they were
// valid come first, the rest follow in file order.
func claimIDs(results []fileResult, lastValid map[string]User) map[string]string {
	var claimants []fileResult
	for _, r := range results {
		if len(r.problems) == 0 {
			claimants = append(claimants, r)
		}
	}
	sort.SliceStable(claimants, func(a, b int) bool {
		return claimants[a].keepsItsID(lastValid) && !claimants[b].keepsItsID(lastValid)
	})

	ownerOfID := map[string]string{}
	for _, r := range claimants {
		if other, taken := ownerOfID[r.parsed.user.ID]; taken {
			slog.Debug("User Id is taken already", "file", r.path, "user", r.parsed.user.ID, "takenBy", other)
			continue
		}
		ownerOfID[r.parsed.user.ID] = r.path
	}
	return ownerOfID
}

// keepsItsID says whether the file had the same User Id the last time it was valid.
func (r fileResult) keepsItsID(lastValid map[string]User) bool {
	last, ok := lastValid[r.path]
	return ok && last.ID == r.parsed.user.ID
}

// problemsWithDuplicateID returns the problems of the file, plus one when its
// User Id belongs to another file.
func (r fileResult) problemsWithDuplicateID(ownerOfID map[string]string) Problems {
	id := r.parsed.user.ID
	other, taken := ownerOfID[id]
	if id == "" || !taken || other == r.path {
		return r.problems
	}
	slog.Debug("Reporting duplicate User Id", "file", r.path, "user", id, "takenBy", other)
	return append(slices.Clone(r.problems), Problem{
		File: r.path, Line: r.parsed.idPos.line, Column: r.parsed.idPos.column,
		Message: fmt.Sprintf("User Id %q is already used by %s", id, other),
	})
}

// isConfigFile says whether a directory entry is a User Config file.
func isConfigFile(dir string, e os.DirEntry) bool {
	name := e.Name()
	if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".toml") {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, name))
	return err == nil && info.Mode().IsRegular()
}
