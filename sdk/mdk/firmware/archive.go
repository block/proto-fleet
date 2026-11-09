// Package firmware prepares hashboard release archives for the MDK upload API.
// It reads selection metadata only; Frigate remains responsible for signing and
// hardware compatibility validation before deployment.
package firmware

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/vmihailenco/msgpack/v5"
)

const (
	MaxImages     = 64
	MaxImageBytes = 64 << 20
	MaxTotalBytes = 256 << 20
	ChunkBytes    = 1 << 20
	// MaxEntries bounds every archive entry, including ignored ones.
	MaxEntries = 1024
	// MaxDirectoryBytes bounds the central directory archive/zip parses before
	// any entry is inspected.
	MaxDirectoryBytes = 1 << 20
	// MaxConcurrentReads bounds unreleased archives held in memory by one
	// process.
	MaxConcurrentReads = 2
)

var readSlots = make(chan struct{}, MaxConcurrentReads)

var errDirectoryLimit = fmt.Errorf("firmware archive exceeds %d entries or %d directory bytes", MaxEntries, MaxDirectoryBytes)

type Metadata struct {
	Build            string `msgpack:"build_type"`
	HardwareRevision string `msgpack:"hw_rev"`
	Major            uint32 `msgpack:"ver_major"`
	Minor            uint32 `msgpack:"ver_minor"`
	Patch            uint32 `msgpack:"ver_patch"`
}

func (m Metadata) Version() string { return fmt.Sprintf("%d.%d.%d", m.Major, m.Minor, m.Patch) }
func ValidBuild(build string) bool { return build == "prod" || build == "dev" || build == "mfgtest" }

// DecodeMetadata follows hbmeta's 1 KiB vector table + six-byte header layout.
func DecodeMetadata(data []byte) (Metadata, error) {
	var m Metadata
	if len(data) < 1030 {
		return m, fmt.Errorf("truncated hashboard metadata header")
	}
	n := int(binary.LittleEndian.Uint16(data[1028:1030]))
	if n == 0 || n > len(data)-1030 {
		return m, fmt.Errorf("invalid hashboard metadata length")
	}
	if err := msgpack.Unmarshal(data[1030:1030+n], &m); err != nil {
		return m, fmt.Errorf("decode hashboard metadata: %w", err)
	}
	m.Build = strings.ToLower(strings.TrimSpace(m.Build))
	m.HardwareRevision = strings.ToLower(strings.TrimSpace(m.HardwareRevision))
	if !ValidBuild(m.Build) || m.HardwareRevision == "" {
		return m, fmt.Errorf("invalid hashboard build or hardware revision")
	}
	return m, nil
}

type Image struct {
	Name     string
	Data     []byte
	Metadata Metadata
}

func (i Image) SHA256() string { return fmt.Sprintf("%x", sha256.Sum256(i.Data)) }

type Archive struct {
	Images  []Image
	Build   string
	Version string
	release func()
}

// Release drops the decoded images and frees the archive's read slot. Callers
// must release every archive Read returns; later calls do nothing.
func (a *Archive) Release() {
	a.Images = nil
	if a.release != nil {
		a.release()
		a.release = nil
	}
}

// Read selects one build by embedded metadata, never by the filename. Empty
// build accepts only an already-selected, unambiguous archive. The archive
// holds one of MaxConcurrentReads process-wide slots until Release, so Read
// waits, or fails once ctx ends, while that many archives are unreleased.
func Read(ctx context.Context, filename, build string) (*Archive, error) {
	if build != "" && !ValidBuild(build) {
		return nil, fmt.Errorf("build must be prod, dev, or mfgtest")
	}
	select {
	case readSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("wait to read firmware archive: %w", ctx.Err())
	}
	archive, err := read(filename, build)
	if err != nil {
		<-readSlots
		return nil, err
	}
	archive.release = func() { <-readSlots }
	return archive, nil
}

func read(filename, build string) (*Archive, error) {
	if err := checkDirectory(filename); err != nil {
		return nil, err
	}
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open hashboard release zip: %w", err)
	}
	defer func() { _ = z.Close() }()
	if len(z.File) > MaxEntries {
		return nil, fmt.Errorf("firmware archive exceeds %d entries", MaxEntries)
	}
	// Validate every declared size before decompressing any image.
	var candidates []*zip.File
	var total uint64
	for _, f := range z.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix(f.Name, ".bin") || strings.Contains(f.Name, ".unsigned.") {
			continue
		}
		if !f.Mode().IsRegular() || path.IsAbs(f.Name) || path.Clean(f.Name) != f.Name || strings.HasPrefix(f.Name, "../") || strings.Contains(f.Name, "\\") {
			return nil, fmt.Errorf("unsafe firmware image path %q", f.Name)
		}
		if f.UncompressedSize64 == 0 || f.UncompressedSize64 > MaxImageBytes || total+f.UncompressedSize64 > MaxTotalBytes {
			return nil, fmt.Errorf("firmware archive exceeds image or total size limits")
		}
		total += f.UncompressedSize64
		candidates = append(candidates, f)
	}
	result := &Archive{}
	seen := map[string]bool{}
	for _, f := range candidates {
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(r, MaxImageBytes+1))
		closeErr := r.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if len(data) > MaxImageBytes {
			return nil, fmt.Errorf("firmware image exceeds size limit")
		}
		meta, err := DecodeMetadata(data)
		if err != nil {
			return nil, fmt.Errorf("image %q: %w", f.Name, err)
		}
		if meta.HardwareRevision == "evb" || meta.HardwareRevision == "evb3" || meta.HardwareRevision == "evb5" || build != "" && meta.Build != build {
			continue
		}
		if len(result.Images) >= MaxImages {
			return nil, fmt.Errorf("firmware upload exceeds %d images", MaxImages)
		}
		name := path.Base(f.Name)
		if seen[strings.ToLower(name)] {
			return nil, fmt.Errorf("duplicate firmware image name %q", name)
		}
		seen[strings.ToLower(name)] = true
		if result.Build != "" && (result.Build != meta.Build || result.Version != meta.Version()) {
			return nil, fmt.Errorf("firmware archive must select exactly one build and version")
		}
		result.Build, result.Version = meta.Build, meta.Version()
		result.Images = append(result.Images, Image{Name: name, Data: data, Metadata: meta})
	}
	if len(result.Images) == 0 {
		return nil, fmt.Errorf("no deployable hashboard images for build %q", build)
	}
	sort.Slice(result.Images, func(i, j int) bool { return result.Images[i].Name < result.Images[j].Name })
	return result, nil
}

const (
	directoryEndLen      = 22
	directoryHeaderLen   = 46
	zip64LocatorLen      = 20
	zip64DirectoryEndLen = 56
	// directoryEndSearch is archive/zip's widest end record search window.
	directoryEndSearch = 65 * 1024
)

// checkDirectory bounds the central directory before zip.OpenReader allocates
// a record for each entry. It must locate and walk the directory exactly as
// archive/zip does: from the end record's declared size rather than its
// declared offset, reading records until one lacks the header signature
// whatever count the end record declares.
func checkDirectory(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open hashboard release zip: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat hashboard release zip: %w", err)
	}
	starts, records, err := directoryStarts(f, info.Size())
	if err != nil {
		return fmt.Errorf("open hashboard release zip: %w", err)
	}
	// archive/zip preallocates its entry list from the declared count.
	if records > MaxEntries {
		return errDirectoryLimit
	}
	for _, start := range starts {
		if err := walkDirectory(f, start); err != nil {
			return err
		}
	}
	return nil
}

// directoryStarts returns every offset archive/zip may start reading directory
// records from, and the declared record count.
func directoryStarts(r io.ReaderAt, size int64) ([]int64, uint64, error) {
	tail := make([]byte, min(size, directoryEndSearch))
	tailOffset := size - int64(len(tail))
	if _, err := r.ReadAt(tail, tailOffset); err != nil && !errors.Is(err, io.EOF) {
		return nil, 0, err
	}
	at := -1
	for i := len(tail) - directoryEndLen; i >= 0; i-- {
		if bytes.Equal(tail[i:i+4], []byte("PK\x05\x06")) {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, 0, errors.New("missing end of central directory")
	}
	end := tailOffset + int64(at)
	record := tail[at:]
	records := uint64(binary.LittleEndian.Uint16(record[10:12]))
	directorySize := uint64(binary.LittleEndian.Uint32(record[12:16]))
	directoryOffset := uint64(binary.LittleEndian.Uint32(record[16:20]))
	if records == 0xffff || directorySize == 0xffff || directoryOffset == 0xffffffff {
		if end >= zip64LocatorLen {
			locator := make([]byte, zip64LocatorLen)
			if _, err := r.ReadAt(locator, end-zip64LocatorLen); err != nil {
				return nil, 0, err
			}
			if bytes.Equal(locator[:4], []byte("PK\x06\x07")) && binary.LittleEndian.Uint32(locator[4:8]) == 0 && binary.LittleEndian.Uint32(locator[16:20]) == 1 {
				recordOffset := binary.LittleEndian.Uint64(locator[8:16])
				if recordOffset > math.MaxInt64 {
					return nil, 0, errors.New("invalid zip64 end of central directory")
				}
				end = int64(recordOffset)
				record64 := make([]byte, zip64DirectoryEndLen)
				if _, err := r.ReadAt(record64, end); err != nil || !bytes.Equal(record64[:4], []byte("PK\x06\x06")) {
					return nil, 0, errors.New("invalid zip64 end of central directory")
				}
				records = binary.LittleEndian.Uint64(record64[32:40])
				directorySize = binary.LittleEndian.Uint64(record64[40:48])
				directoryOffset = binary.LittleEndian.Uint64(record64[48:56])
			}
		}
	}
	if directorySize > math.MaxInt64 || directoryOffset > math.MaxInt64 {
		return nil, 0, errors.New("invalid central directory")
	}
	start := end - int64(directorySize)
	if start < 0 || start >= size {
		return nil, 0, errors.New("invalid central directory offset")
	}
	starts := []int64{start}
	// A positive base offset makes archive/zip try the declared offset itself.
	if declared := int64(directoryOffset); start > declared {
		starts = append(starts, declared)
	}
	return starts, records, nil
}

// walkDirectory follows records from start as archive/zip's reader loop does,
// rejecting the archive once it passes either directory limit.
func walkDirectory(r io.ReaderAt, start int64) error {
	header := make([]byte, directoryHeaderLen)
	offset := start
	for entries := 0; ; entries++ {
		if entries > MaxEntries || offset-start > MaxDirectoryBytes {
			return errDirectoryLimit
		}
		if _, err := r.ReadAt(header, offset); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read hashboard release zip: %w", err)
		}
		if !bytes.Equal(header[:4], []byte("PK\x01\x02")) {
			return nil
		}
		offset += directoryHeaderLen + int64(binary.LittleEndian.Uint16(header[28:30])) +
			int64(binary.LittleEndian.Uint16(header[30:32])) + int64(binary.LittleEndian.Uint16(header[32:34]))
	}
}

// Write emits deterministic selected bytes. Different builds have different
// payload checksums, preserving Fleet's existing artifact/provenance contract.
func (a *Archive) Write(w io.Writer) error {
	z := zip.NewWriter(w)
	for _, image := range a.Images {
		entry, err := z.CreateHeader(&zip.FileHeader{Name: image.Name, Method: zip.Store})
		if err != nil {
			return err
		}
		if _, err := io.Copy(entry, bytes.NewReader(image.Data)); err != nil {
			return err
		}
	}
	return z.Close()
}
