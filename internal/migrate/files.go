// Package migrate implements Palma's PostgreSQL SQL-file migration tool.
package migrate

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var filename = regexp.MustCompile(`^([0-9]+)_([a-z][a-z0-9_]*)\.sql$`)
var migrationName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type Migration struct {
	Version                  int64
	Name, Up, Down, Checksum string
}

func Load(directory string) ([]Migration, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("migrations: read directory: %w", err)
	}
	byVersion := map[int64]*Migration{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		match := filename.FindStringSubmatch(entry.Name())
		if match == nil {
			return nil, fmt.Errorf("migrations: invalid filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("migrations: invalid version in %q", entry.Name())
		}
		if byVersion[version] != nil {
			return nil, fmt.Errorf("migrations: duplicate version %d", version)
		}
		content, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		up, down, err := splitSections(string(content))
		if err != nil {
			return nil, fmt.Errorf("migrations: %s: %w", entry.Name(), err)
		}
		byVersion[version] = &Migration{Version: version, Name: match[2], Up: up, Down: down}
	}
	result := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		if m.Up == "" || m.Down == "" {
			return nil, fmt.Errorf("migrations: version %d requires both Up and Down sections", m.Version)
		}
		m.Checksum = fmt.Sprintf("%x", sha256.Sum256([]byte(m.Up+"\x00"+m.Down)))
		result = append(result, *m)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	return result, nil
}

// Create reserves one file exclusively. Versions increase independently of the clock.
func Create(directory, name string) (string, error) {
	if !migrationName.MatchString(name) {
		return "", fmt.Errorf("migrations: name must be lowercase snake_case and start with a letter")
	}
	if err := os.MkdirAll(directory, 0755); err != nil {
		return "", err
	}
	lockPath := filepath.Join(directory, ".pfw-create.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", fmt.Errorf("migrations: cannot reserve creation lock (another create may be running): %w", err)
	}
	defer os.Remove(lockPath)
	if err := lock.Close(); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}
	var latest int64
	for _, entry := range entries {
		if match := filename.FindStringSubmatch(entry.Name()); match != nil {
			v, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil {
				return "", err
			}
			if v > latest {
				latest = v
			}
		}
	}
	if latest == int64(^uint64(0)>>1) {
		return "", fmt.Errorf("migrations: version limit reached")
	}
	path := filepath.Join(directory, fmt.Sprintf("%06d_%s.sql", latest+1, name))
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return "", err
	}
	_, writeErr := fmt.Fprintf(file, "-- +pfw Up\n-- Write the up SQL for %s here. Do not add BEGIN/COMMIT.\n\n-- +pfw Down\n-- Write the down SQL for %s here.\n", name, name)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("migrations: write file: %w", errors.Join(writeErr, closeErr))
	}
	return path, nil
}

var errEmptySQL = errors.New("SQL body is empty (comments only); write the migration before running it")

func splitSections(source string) (string, string, error) {
	type marker struct {
		start, end int
		label      string
	}
	var markers []marker
	err := scanSQL(source, false, func(start, end int) error {
		lineStart := strings.LastIndex(source[:start], "\n") + 1
		if strings.TrimSpace(source[lineStart:start]) != "" {
			return nil
		}
		label := strings.TrimSpace(source[start:end])
		if !strings.HasPrefix(label, "-- +pfw") {
			return nil
		}
		if label != "-- +pfw Up" && label != "-- +pfw Down" {
			return fmt.Errorf("unknown migration section marker")
		}
		if end < len(source) {
			end++
		}
		markers = append(markers, marker{start: start, end: end, label: label})
		return nil
	})
	if err != nil {
		return "", "", err
	}
	if len(markers) != 2 || markers[0].label != "-- +pfw Up" || markers[1].label != "-- +pfw Down" {
		return "", "", fmt.Errorf("exactly one -- +pfw Up followed by one -- +pfw Down is required")
	}
	if prefix := source[:markers[0].start]; strings.TrimSpace(prefix) != "" {
		if err := validateSQL(prefix); !errors.Is(err, errEmptySQL) {
			return "", "", fmt.Errorf("only comments may precede the Up section")
		}
	}
	up, down := source[markers[0].end:markers[1].start], source[markers[1].end:]
	for _, section := range []struct{ name, body string }{{"Up", up}, {"Down", down}} {
		if err := validateSQL(section.body); err != nil {
			return "", "", fmt.Errorf("%s section: %w", section.name, err)
		}
	}
	return up, down, nil
}

// Ignore comments and quoted bodies when recognizing top-level transaction
// control. PostgreSQL owns the syntax validation of the remaining SQL.
func validateSQL(source string) error { return scanSQL(source, true, nil) }

func scanSQL(source string, check bool, onComment func(int, int) error) error {
	first := true
	hasSQL := false
	for i := 0; i < len(source); {
		c := source[i]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			i++
			continue
		}
		if strings.HasPrefix(source[i:], "--") {
			start := i
			for i < len(source) && source[i] != '\n' {
				i++
			}
			if onComment != nil {
				if err := onComment(start, i); err != nil {
					return err
				}
			}
			continue
		}
		if strings.HasPrefix(source[i:], "/*") {
			i += 2
			depth := 1
			for i < len(source) && depth > 0 {
				if strings.HasPrefix(source[i:], "/*") {
					depth++
					i += 2
				} else if strings.HasPrefix(source[i:], "*/") {
					depth--
					i += 2
				} else {
					i++
				}
			}
			if depth != 0 {
				return fmt.Errorf("unterminated SQL comment")
			}
			continue
		}
		if c == '\'' || c == '"' {
			hasSQL = true
			quote := c
			escaped := quote == '\'' && i > 0 && (source[i-1] == 'e' || source[i-1] == 'E') && (i < 2 || !identifierByte(source[i-2]))
			i++
			closed := false
			for i < len(source) {
				if source[i] == '\\' && escaped {
					i += 2
					continue
				}
				if source[i] == quote {
					if i+1 < len(source) && source[i+1] == quote {
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				i++
			}
			if !closed {
				return fmt.Errorf("unterminated SQL quote")
			}
			first = false
			continue
		}
		if c == '$' && (i == 0 || !identifierByte(source[i-1])) {
			end := i + 1
			for end < len(source) && ((source[end] >= 'a' && source[end] <= 'z') || (source[end] >= 'A' && source[end] <= 'Z') || (source[end] >= '0' && source[end] <= '9') || source[end] == '_') {
				end++
			}
			if end < len(source) && source[end] == '$' {
				tag := source[i : end+1]
				close := strings.Index(source[end+1:], tag)
				if close < 0 {
					return fmt.Errorf("unterminated SQL dollar quote")
				}
				i = end + 1 + close + len(tag)
				first = false
				continue
			}
		}
		if c == ';' {
			first = true
			i++
			continue
		}
		if first {
			end := i
			for end < len(source) && ((source[end] >= 'a' && source[end] <= 'z') || (source[end] >= 'A' && source[end] <= 'Z')) {
				end++
			}
			word := strings.ToUpper(source[i:end])
			hasSQL = true
			if check {
				switch word {
				case "BEGIN", "START", "COMMIT", "END", "ROLLBACK", "ABORT", "PREPARE":
					return fmt.Errorf("transaction control is managed by pfw; %s is not allowed", word)
				}
			}
			first = false
		}
		i++
	}
	if check && !hasSQL {
		return errEmptySQL
	}
	return nil
}

func identifierByte(c byte) bool {
	return c >= 128 || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '$'
}
