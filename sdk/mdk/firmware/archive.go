// Package firmware prepares hashboard release archives for the MDK upload API.
// It reads selection metadata only; Frigate remains responsible for signing and
// hardware compatibility validation before deployment.
package firmware

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
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
	// maxConcurrentReads bounds archives held in memory by one process.
	maxConcurrentReads = 2
)

var readSlots = make(chan struct{}, maxConcurrentReads)

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
}

// Read selects one build by embedded metadata, never by the filename. Empty
// build accepts only an already-selected, unambiguous archive.
func Read(filename, build string) (*Archive, error) {
	if build != "" && !ValidBuild(build) {
		return nil, fmt.Errorf("build must be prod, dev, or mfgtest")
	}
	readSlots <- struct{}{}
	defer func() { <-readSlots }()
	if err := checkDirectory(filename); err != nil {
		return nil, err
	}
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open hashboard release zip: %w", err)
	}
	defer z.Close()
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
	directoryEndLen     = 22
	zip64LocatorLen     = 20
	zip64DirectoryEnd   = 56
	maxDirectoryComment = 1 << 16
)

// checkDirectory bounds the central directory before archive/zip allocates a
// record for each entry. archive/zip reads records from the directory offset
// up to the end record regardless of the declared count, so the physical span
// is what bounds allocation; the declared count only fails fast.
func checkDirectory(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return fmt.Errorf("open hashboard release zip: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat hashboard release zip: %w", err)
	}
	size := info.Size()
	tail := make([]byte, min(size, directoryEndLen+maxDirectoryComment))
	tailOffset := size - int64(len(tail))
	if _, err := f.ReadAt(tail, tailOffset); err != nil {
		return fmt.Errorf("read hashboard release zip: %w", err)
	}
	end := -1
	for i := len(tail) - directoryEndLen; i >= 0; i-- {
		if bytes.Equal(tail[i:i+4], []byte("PK\x05\x06")) {
			end = i
			break
		}
	}
	if end < 0 {
		return fmt.Errorf("open hashboard release zip: missing end of central directory")
	}
	record := tail[end:]
	entries := uint64(binary.LittleEndian.Uint16(record[10:12]))
	directoryOffset := uint64(binary.LittleEndian.Uint32(record[16:20]))
	directoryEnd := uint64(tailOffset) + uint64(end)
	if entries == 0xffff || directoryOffset == 0xffffffff || binary.LittleEndian.Uint32(record[12:16]) == 0xffffffff {
		if directoryEnd < zip64LocatorLen {
			return fmt.Errorf("open hashboard release zip: invalid zip64 locator")
		}
		locator := make([]byte, zip64LocatorLen)
		if _, err := f.ReadAt(locator, int64(directoryEnd-zip64LocatorLen)); err != nil || !bytes.Equal(locator[:4], []byte("PK\x06\x07")) {
			return fmt.Errorf("open hashboard release zip: invalid zip64 locator")
		}
		recordOffset := binary.LittleEndian.Uint64(locator[8:16])
		record64 := make([]byte, zip64DirectoryEnd)
		if recordOffset >= uint64(size) {
			return fmt.Errorf("open hashboard release zip: invalid zip64 directory")
		}
		if _, err := f.ReadAt(record64, int64(recordOffset)); err != nil || !bytes.Equal(record64[:4], []byte("PK\x06\x06")) {
			return fmt.Errorf("open hashboard release zip: invalid zip64 directory")
		}
		entries = binary.LittleEndian.Uint64(record64[32:40])
		directoryOffset = binary.LittleEndian.Uint64(record64[48:56])
		directoryEnd = recordOffset
	}
	if directoryOffset > directoryEnd {
		return fmt.Errorf("open hashboard release zip: invalid central directory offset")
	}
	if entries > MaxEntries || directoryEnd-directoryOffset > MaxDirectoryBytes {
		return fmt.Errorf("firmware archive exceeds %d entries or %d directory bytes", MaxEntries, MaxDirectoryBytes)
	}
	return nil
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
