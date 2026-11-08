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
)

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
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, fmt.Errorf("open hashboard release zip: %w", err)
	}
	defer z.Close()
	result := &Archive{}
	seen := map[string]bool{}
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
