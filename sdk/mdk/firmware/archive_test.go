package firmware_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		a, err := firmware.Read(t.Context(), file, build)
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
		a.Release()
	}
	if _, err := firmware.Read(t.Context(), file, ""); err == nil {
		t.Fatal("accepted ambiguous build")
	}
	if _, err := firmware.Read(t.Context(), file, "mfgtest"); err == nil {
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
			if _, err := firmware.Read(t.Context(), firmwaretest.Archive(t, images), "prod"); err == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
}

func TestArchiveBoundsCentralDirectory(t *testing.T) {
	image := firmwaretest.Image(t, "prod", "f1", 2)
	for name, extra := range map[string]func(map[string][]byte){
		"ignored entries over limit": func(images map[string][]byte) {
			for i := range firmware.MaxEntries + 1 {
				images[fmt.Sprintf("notes/%04d.txt", i)] = nil
			}
		},
		"directory bytes over limit": func(images map[string][]byte) {
			for i := range 300 {
				images[fmt.Sprintf("%03d-%s.txt", i, strings.Repeat("n", 4000))] = nil
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			images := map[string][]byte{"app.bin": image}
			extra(images)
			_, err := firmware.Read(t.Context(), firmwaretest.Archive(t, images), "prod")
			if err == nil || !strings.Contains(err.Error(), "directory bytes") {
				t.Fatalf("expected central directory rejection, got %v", err)
			}
		})
	}
	small, err := firmware.Read(t.Context(), firmwaretest.Archive(t, map[string][]byte{"app.bin": image, "notes.txt": []byte("release")}), "prod")
	if err != nil {
		t.Fatalf("rejected a small archive: %v", err)
	}
	small.Release()
}

func TestArchiveIgnoresForgedDirectoryOffset(t *testing.T) {
	images := map[string][]byte{"app.bin": firmwaretest.Image(t, "prod", "f1", 2)}
	for i := range firmware.MaxEntries + 1 {
		images[fmt.Sprintf("notes/%04d.txt", i)] = nil
	}
	file := firmwaretest.Archive(t, images)
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	end := bytes.LastIndex(data, []byte("PK\x05\x06"))
	forgeEndRecord(data[end:], 1, uint32(end))
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := firmware.Read(t.Context(), file, "prod"); err == nil || !strings.Contains(err.Error(), "directory bytes") {
		t.Fatalf("expected central directory rejection, got %v", err)
	}
}

func forgeEndRecord(record []byte, entries uint16, directoryOffset uint32) {
	binary.LittleEndian.PutUint16(record[8:], entries)
	binary.LittleEndian.PutUint16(record[10:], entries)
	binary.LittleEndian.PutUint32(record[16:], directoryOffset)
}

func TestArchiveHoldsReadSlotUntilRelease(t *testing.T) {
	file := firmwaretest.Archive(t, map[string][]byte{"app.bin": firmwaretest.Image(t, "prod", "f1", 2)})
	var held []*firmware.Archive
	t.Cleanup(func() {
		for _, a := range held {
			a.Release()
		}
	})
	for range firmware.MaxConcurrentReads {
		a, err := firmware.Read(t.Context(), file, "prod")
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, a)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := firmware.Read(ctx, file, "prod"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read beyond the unreleased archive limit: %v", err)
	}
	held[0].Release()
	held[0].Release()
	if held[0].Images != nil {
		t.Fatal("released archive kept its images")
	}
	a, err := firmware.Read(t.Context(), file, "prod")
	if err != nil {
		t.Fatalf("read after release: %v", err)
	}
	a.Release()
}

func TestArchiveRejectsTooManyImages(t *testing.T) {
	images := map[string][]byte{}
	for i := range firmware.MaxImages + 1 {
		images[fmt.Sprintf("board-%02d.bin", i)] = firmwaretest.Image(t, "prod", "f1", 2)
	}
	_, err := firmware.Read(t.Context(), firmwaretest.Archive(t, images), "prod")
	if err == nil || !strings.Contains(err.Error(), "exceeds 64 images") {
		t.Fatalf("accepted %d images: %v", len(images), err)
	}
}

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
			_, err := firmware.Read(t.Context(), declaredArchive(t, entries), "prod")
			if err == nil || !strings.Contains(err.Error(), "size limits") {
				t.Fatalf("expected size limit rejection, got %v", err)
			}
		})
	}
	symlink := zip.FileHeader{Name: "app.bin", UncompressedSize64: 4}
	symlink.SetMode(os.ModeSymlink | 0o777)
	_, err := firmware.Read(t.Context(), declaredArchive(t, []zip.FileHeader{symlink}), "prod")
	if err == nil || !strings.Contains(err.Error(), "unsafe firmware image path") {
		t.Fatalf("accepted symlink entry: %v", err)
	}
}
