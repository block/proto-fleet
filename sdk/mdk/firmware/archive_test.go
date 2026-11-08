package firmware_test

import (
	"bytes"
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
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := firmware.Read(firmwaretest.Archive(t, images), "prod"); err == nil {
				t.Fatal("accepted invalid archive")
			}
		})
	}
}
