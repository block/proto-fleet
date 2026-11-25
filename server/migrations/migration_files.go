package migrations

import (
	"io"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"time"
)

type migrationFiles struct {
	directory string
	scripts   map[string]string
	entries   []fs.DirEntry
}

func newMigrationFiles(directory string, scripts map[string]string) migrationFiles {
	entries := make([]fs.DirEntry, 0, len(scripts))
	for filename, body := range scripts {
		entries = append(entries, migrationEntry{filename: filename, size: int64(len(body)), mode: 0o444})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return migrationFiles{directory: directory, scripts: scripts, entries: entries}
}

func (m migrationFiles) script(name string) (string, bool) {
	if !fs.ValidPath(name) || path.Dir(name) != m.directory {
		return "", false
	}
	body, ok := m.scripts[path.Base(name)]
	return body, ok
}

func (m migrationFiles) ReadFile(name string) ([]byte, error) {
	if body, ok := m.script(name); ok {
		return []byte(body), nil
	}
	return nil, &fs.PathError{Op: "read", Path: name, Err: fs.ErrNotExist}
}

func (m migrationFiles) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "." && m.directory != "." {
		return []fs.DirEntry{migrationEntry{filename: m.directory, mode: fs.ModeDir | 0o555}}, nil
	}
	if name != m.directory {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	return slices.Clone(m.entries), nil
}

func (m migrationFiles) Open(name string) (fs.File, error) {
	if name == "." || name == m.directory {
		entries, err := m.ReadDir(name)
		if err != nil {
			return nil, err
		}
		return &migrationDirectory{migrationEntry: migrationEntry{filename: path.Base(name), mode: fs.ModeDir | 0o555}, entries: entries}, nil
	}
	body, ok := m.script(name)
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &migrationFile{
		migrationEntry: migrationEntry{filename: path.Base(name), size: int64(len(body)), mode: 0o444},
		Reader:         strings.NewReader(body),
	}, nil
}

type migrationEntry struct {
	filename string
	size     int64
	mode     fs.FileMode
}

func (e migrationEntry) Name() string               { return e.filename }
func (e migrationEntry) Size() int64                { return e.size }
func (e migrationEntry) Mode() fs.FileMode          { return e.mode }
func (e migrationEntry) ModTime() time.Time         { return time.Time{} }
func (e migrationEntry) IsDir() bool                { return e.mode.IsDir() }
func (e migrationEntry) Sys() any                   { return nil }
func (e migrationEntry) Type() fs.FileMode          { return e.mode.Type() }
func (e migrationEntry) Info() (fs.FileInfo, error) { return e, nil }
func (e migrationEntry) Stat() (fs.FileInfo, error) { return e, nil }
func (e migrationEntry) Close() error               { return nil }

type migrationFile struct {
	migrationEntry
	*strings.Reader
}

type migrationDirectory struct {
	migrationEntry
	entries []fs.DirEntry
}

func (d *migrationDirectory) Read([]byte) (int, error) { return 0, fs.ErrInvalid }

func (d *migrationDirectory) ReadDir(n int) ([]fs.DirEntry, error) {
	if n > 0 && len(d.entries) == 0 {
		return nil, io.EOF
	}
	if n <= 0 || n > len(d.entries) {
		n = len(d.entries)
	}
	entries := d.entries[:n]
	d.entries = d.entries[n:]
	return entries, nil
}
