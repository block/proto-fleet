package firmware_test

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/block/proto-fleet/sdk/mdk/firmware"
	"github.com/block/proto-fleet/sdk/mdk/firmware/firmwaretest"
)

func TestSelectBuildByMetadata(t *testing.T) {
	file := firmwaretest.Archive(t, map[string][]byte{
		"renamed/dev.bin":    firmwaretest.Image(t, "prod", "f1", 2),
		"renamed/prod.bin":   firmwaretest.Image(t, "dev", "f1", 2),
		"board.bin":          firmwaretest.Image(t, "prod", "evb5", 2),
		"board.unsigned.bin": []byte("unsigned payload"),
		"notes.txt":          []byte("release"),
	})
	for _, build := range []string{"prod", "dev"} {
		a, err := firmware.Read(file, build)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Images) != 1 || a.Build != build || a.Version != "2.0.0" {
			t.Fatalf("unexpected selection: %+v", a)
		}
		var first, second bytes.Buffer
		if err := a.Write(&first); err != nil {
			t.Fatal(err)
		}
		if err := a.Write(&second); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first.Bytes(), second.Bytes()) {
			t.Fatal("non-deterministic normalized archive")
		}
	}
	if _, err := firmware.Read(file, ""); err == nil {
		t.Fatal("accepted ambiguous build")
	}
	if _, err := firmware.Read(file, "mfgtest"); err == nil {
		t.Fatal("accepted missing build")
	}
}

func TestArchiveRejectsInvalidImages(t *testing.T) {
	for name, images := range map[string]map[string][]byte{
		"path traversal":      {"../app.bin": firmwaretest.Image(t, "prod", "f1", 2)},
		"truncated":           {"app.bin": []byte("invalid")},
		"duplicate basenames": {"a/app.bin": firmwaretest.Image(t, "prod", "f1", 2), "b/app.bin": firmwaretest.Image(t, "prod", "f2", 2)},
		"mixed versions":      {"app.bin": firmwaretest.Image(t, "prod", "f1", 2), "other.bin": firmwaretest.Image(t, "prod", "f2", 3)},
		"empty image":         {"app.bin": nil},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := firmware.Read(firmwaretest.Archive(t, images), "prod"); err == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
}

func TestArchiveRejectsTooManyImages(t *testing.T) {
	images := map[string][]byte{}
	for i := range firmware.MaxImages + 1 {
		images[fmt.Sprintf("board-%02d.bin", i)] = firmwaretest.Image(t, "prod", "f1", 2)
	}
	_, err := firmware.Read(firmwaretest.Archive(t, images), "prod")
	if err == nil || !strings.Contains(err.Error(), "exceeds 64 images") {
		t.Fatalf("accepted %d images: %v", len(images), err)
	}
}

// declaredArchive writes entries whose central directory declares a size, so
// limits are checked without allocating the declared bytes.
func declaredArchive(t *testing.T, entries []zip.FileHeader) string {
	t.Helper()
	var out bytes.Buffer
	z := zip.NewWriter(&out)
	for _, header := range entries {
		header.Method = zip.Store
		if _, err := z.CreateRaw(&header); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(t.TempDir(), "declared.zip")
	if err := os.WriteFile(filename, out.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func TestArchiveRejectsDeclaredSizesBeforeReading(t *testing.T) {
	image := uint64(firmware.MaxImageBytes)
	for name, entries := range map[string][]zip.FileHeader{
		"image over limit": {{Name: "app.bin", UncompressedSize64: image + 1}},
		"total over limit": {
			{Name: "a.bin", UncompressedSize64: image}, {Name: "b.bin", UncompressedSize64: image},
			{Name: "c.bin", UncompressedSize64: image}, {Name: "d.bin", UncompressedSize64: image},
			{Name: "e.bin", UncompressedSize64: 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := firmware.Read(declaredArchive(t, entries), "prod")
			if err == nil || !strings.Contains(err.Error(), "size limits") {
				t.Fatalf("expected size limit rejection, got %v", err)
			}
		})
	}
	symlink := zip.FileHeader{Name: "app.bin", UncompressedSize64: 4}
	symlink.SetMode(os.ModeSymlink | 0o777)
	_, err := firmware.Read(declaredArchive(t, []zip.FileHeader{symlink}), "prod")
	if err == nil || !strings.Contains(err.Error(), "unsafe firmware image path") {
		t.Fatalf("accepted symlink entry: %v", err)
	}
}
