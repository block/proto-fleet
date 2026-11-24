package frigaterelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

const goodControl = "Package: frigate\nVersion: 1.4.0\nArchitecture: arm64\nMaintainer: Someone <a@example.com>\nDescription: control server\n with a continuation line\n"

func TestParseDebGzipControl(t *testing.T) {
	deb := buildDeb(t, "control.tar.gz", controlTar(t, "./control", goodControl, true))
	c, err := ParseDeb(bytesReader(deb))
	require.NoError(t, err)
	require.Equal(t, Control{Package: "frigate", Version: "1.4.0", Architecture: "arm64"}, c)
}

func TestParseDebPlainControlWithoutDotSlash(t *testing.T) {
	deb := buildDeb(t, "control.tar", controlTar(t, "control", goodControl, false))
	c, err := ParseDeb(bytesReader(deb))
	require.NoError(t, err)
	require.Equal(t, "frigate", c.Package)
}

func TestParseDebUnsupportedCompression(t *testing.T) {
	deb := buildDeb(t, "control.tar.zst", []byte("zstd bytes"))
	_, err := ParseDeb(bytesReader(deb))
	require.ErrorIs(t, err, ErrUnsupportedCompression)
}

func TestParseDebRejectsMalformed(t *testing.T) {
	good := buildDeb(t, "control.tar.gz", controlTar(t, "./control", goodControl, true))
	for name, data := range map[string][]byte{
		"not ar":            []byte("hello world, this is not a package"),
		"empty":             nil,
		"truncated":         good[:len(good)/2],
		"bad header magic":  append([]byte(arMagic), make([]byte, arHeaderLen)...),
		"no control file":   buildDeb(t, "control.tar.gz", controlTar(t, "./postinst", "x", true)),
		"missing fields":    buildDeb(t, "control.tar.gz", controlTar(t, "./control", "Package: frigate\n", true)),
		"malformed line":    buildDeb(t, "control.tar.gz", controlTar(t, "./control", "Package frigate\n", true)),
		"corrupt gzip":      buildDeb(t, "control.tar.gz", []byte("definitely not gzip")),
		"only data members": []byte(arMagic),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDeb(bytesReader(data))
			require.ErrorIs(t, err, ErrPackageInvalid)
		})
	}
}

func TestReadDebControlFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frigate.deb")
	require.NoError(t, os.WriteFile(path, buildDeb(t, "control.tar.gz", controlTar(t, "./control", goodControl, true)), 0o600))
	c, err := ReadDebControl(path)
	require.NoError(t, err)
	require.Equal(t, "arm64", c.Architecture)

	_, err = ReadDebControl(filepath.Join(t.TempDir(), "missing.deb"))
	require.Error(t, err)
}

func TestControlMatches(t *testing.T) {
	s, err := ParseStatement(statementBytes(t, nil))
	require.NoError(t, err)

	require.NoError(t, Control{Package: "frigate", Version: "1.4.0", Architecture: "arm64"}.Matches(s))
	require.NoError(t, Control{Package: "frigate", Version: "1.4.0-1", Architecture: "arm64"}.Matches(s))
	require.ErrorIs(t, Control{Package: "other", Version: "1.4.0", Architecture: "arm64"}.Matches(s), ErrPackageInvalid)
	require.ErrorIs(t, Control{Package: "frigate", Version: "1.4.0", Architecture: "amd64"}.Matches(s), ErrPackageInvalid)
	require.ErrorIs(t, Control{Package: "frigate", Version: "1.4.01", Architecture: "arm64"}.Matches(s), ErrPackageInvalid)
	require.ErrorIs(t, Control{Package: "frigate", Version: "1.5.0", Architecture: "arm64"}.Matches(s), ErrPackageInvalid)
}

func TestControlMatchesSplitsTheRevisionOnTheLastHyphen(t *testing.T) {
	s, err := ParseStatement(statementBytes(t, nil))
	require.NoError(t, err)
	match := func(version string) error {
		return Control{Package: "frigate", Version: version, Architecture: "arm64"}.Matches(s)
	}
	require.NoError(t, match("1.4.0-1"))
	require.NoError(t, match("1.4.0-1ubuntu2"))
	require.ErrorIs(t, match("1.4.0-rc1-1"), ErrPackageInvalid, "the upstream part is 1.4.0-rc1")
	require.ErrorIs(t, match("1.4.0-1-1"), ErrPackageInvalid)
	require.ErrorIs(t, match("1.4.0-"), ErrPackageInvalid, "an empty revision is not a revision")
	require.ErrorIs(t, match("1.4.0--1"), ErrPackageInvalid)

	// A hyphen in the statement version is the upstream part's own.
	rc, err := ParseStatement(statementBytes(t, func(m *statementJSON) { m.Version = "1.4.0-rc1" }))
	require.NoError(t, err)
	require.NoError(t, Control{Package: "frigate", Version: "1.4.0-rc1-1", Architecture: "arm64"}.Matches(rc))
	require.NoError(t, Control{Package: "frigate", Version: "1.4.0-rc1", Architecture: "arm64"}.Matches(rc))
	require.ErrorIs(t, Control{Package: "frigate", Version: "1.4.0-1", Architecture: "arm64"}.Matches(rc), ErrPackageInvalid)
}

// arMember writes one ar member with a header that claims size bytes, whatever
// the data length, to model a hostile package.
func arMember(name, claimedSize string, data []byte) []byte {
	var out bytes.Buffer
	fmt.Fprintf(&out, "%-16s%-12d%-6d%-6d%-8s%-10s`\n", name+"/", 0, 0, 0, "100644", claimedSize)
	out.Write(data)
	return out.Bytes()
}

func TestParseDebBoundsDecompressionAndClaimedSizes(t *testing.T) {
	// A control.tar.gz that is a few KiB on the wire but inflates far past the
	// 1 MiB cap, once as an oversized control file and once as an oversized
	// entry ahead of it.
	var bigControl bytes.Buffer
	bigControl.WriteString("Package: frigate\nVersion: 1.4.0\nArchitecture: arm64\nDescription: ")
	bigControl.Write(bytes.Repeat([]byte("A"), 3<<20))
	bigControl.WriteString("\n")
	oversizedControl := controlTar(t, "./control", bigControl.String(), true)
	require.Less(t, len(oversizedControl), maxControlBytes, "the archive itself is small, so only the inflated size is hostile")

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	filler := bytes.Repeat([]byte{0}, 3<<20)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./filler", Mode: 0o644, Size: int64(len(filler)), Typeflag: tar.TypeReg}))
	_, err := tw.Write(filler)
	require.NoError(t, err)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "./control", Mode: 0o644, Size: int64(len(goodControl)), Typeflag: tar.TypeReg}))
	_, err = tw.Write([]byte(goodControl))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, err = zw.Write(tarBuf.Bytes())
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.Less(t, gz.Len(), maxControlBytes)

	arHead := []byte(arMagic)
	debianBinary := arMember("debian-binary", "4", []byte("2.0\n"))
	huge := append(append(append([]byte(nil), arHead...), debianBinary...), arMember("control.tar.gz", "9999999999", []byte("tiny"))...)
	hugeOther := append(append(append([]byte(nil), arHead...), debianBinary...), arMember("data.tar.gz", "9999999999", []byte("tiny"))...)
	justOver := append(append(append([]byte(nil), arHead...), debianBinary...), arMember("control.tar.gz", strconv.Itoa(maxControlBytes+1), []byte("tiny"))...)
	negative := append(append(append([]byte(nil), arHead...), debianBinary...), arMember("control.tar.gz", "-1", nil)...)
	for name, tc := range map[string]struct {
		data     []byte
		contains string
	}{
		"control file inflates past the cap":  {buildDeb(t, "control.tar.gz", oversizedControl), "unexpected EOF"},
		"entry before control inflates":       {buildDeb(t, "control.tar.gz", gz.Bytes()), "unexpected EOF"},
		"control member claims a huge size":   {huge, "control archive is too large"},
		"other member claims a huge size":     {hugeOther, "truncated package"},
		"control member claims just too much": {justOver, "control archive is too large"},
		"negative claimed size":               {negative, "corrupt ar member size"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseDeb(bytesReader(tc.data))
			require.ErrorIs(t, err, ErrPackageInvalid)
			require.ErrorContains(t, err, tc.contains)
		})
	}
}
