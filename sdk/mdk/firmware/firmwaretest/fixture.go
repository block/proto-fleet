// Package firmwaretest builds metadata-bearing images for simulator tests.
// These are not signed firmware and must never be deployed to real hardware.
package firmwaretest

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/block/proto-fleet/sdk/mdk/firmware"
	"github.com/vmihailenco/msgpack/v5"
)

func Image(t testing.TB, build, hardware string, major uint32) []byte {
	t.Helper()
	meta, err := msgpack.Marshal(&firmware.Metadata{Build: build, HardwareRevision: hardware, Major: major})
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 1030)
	binary.LittleEndian.PutUint32(data[1024:1028], crc32.ChecksumIEEE(meta))
	binary.LittleEndian.PutUint16(data[1028:1030], uint16(len(meta)))
	return append(append(data, meta...), bytes.Repeat([]byte{0xcd}, 4096)...)
}

func Archive(t testing.TB, images map[string][]byte) string {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "release.zip")
	f, err := os.Create(filename)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, data := range images {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return filename
}
