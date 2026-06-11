package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/urfave/cli/v3"
)

// firmwareCommand stays handwritten: the firmware file lifecycle is plain
// multipart/chunked HTTP rather than protobuf RPC, so the CLI generator does
// not cover it. Applying an uploaded file to miners remains the generated
// `minercommand firmware-update` command.
func firmwareCommand() *cli.Command {
	return &cli.Command{
		Name:  "firmware",
		Usage: "Upload and manage firmware files",
		Commands: []*cli.Command{
			firmwareConfigCommand(),
			firmwareCheckCommand(),
			firmwareUploadCommand(),
			firmwareListCommand(),
			firmwareDeleteCommand(),
			firmwareDeleteAllCommand(),
		},
	}
}

func firmwareConfigCommand() *cli.Command {
	return &cli.Command{
		Name:  "config",
		Usage: "Show firmware upload constraints (allowed extensions, size limits)",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, _, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			cfg, err := client.FirmwareConfig(ctx)
			if err != nil {
				return err
			}
			return printJSON(cfg)
		},
	}
}

func firmwareCheckCommand() *cli.Command {
	return &cli.Command{
		Name:      "check",
		Usage:     "Check whether a firmware file with the same SHA-256 already exists on the server",
		ArgsUsage: "<path>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			path, err := singleArg(cmd, "<path>")
			if err != nil {
				return err
			}
			digest, err := fileSHA256(path)
			if err != nil {
				return err
			}

			client, _, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			resp, err := client.FirmwareCheck(ctx, digest)
			if err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func firmwareUploadCommand() *cli.Command {
	return &cli.Command{
		Name:      "upload",
		Usage:     "Upload a firmware file, reusing the server copy when checksums match",
		ArgsUsage: "<path>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "Upload even when a file with the same checksum already exists on the server"},
			&cli.BoolFlag{Name: "quiet", Usage: "Suppress progress output on stderr"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			path, err := singleArg(cmd, "<path>")
			if err != nil {
				return err
			}

			client, _, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			var progress io.Writer
			if !cmd.Bool("quiet") {
				progress = os.Stderr
			}
			result, reused, err := runFirmwareUpload(ctx, client, path, cmd.Bool("force"), progress)
			if err != nil {
				return err
			}
			if reused && progress != nil {
				_, _ = fmt.Fprintln(progress, "file with identical sha256 already on server; skipped upload (use --force to re-upload)")
			}
			return printJSON(result)
		},
	}
}

func firmwareListCommand() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "List firmware files stored on the server",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, _, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			resp, err := client.FirmwareList(ctx)
			if err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

func firmwareDeleteCommand() *cli.Command {
	return &cli.Command{
		Name:      "delete",
		Usage:     "Delete a firmware file by id",
		ArgsUsage: "<firmware-file-id>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			fileID, err := singleArg(cmd, "<firmware-file-id>")
			if err != nil {
				return err
			}

			client, _, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			if err := client.FirmwareDelete(ctx, fileID); err != nil {
				return err
			}
			// The server replies 204 with no body; echo the id so the command
			// still prints a JSON result like every other fleetcli command.
			return printJSON(struct {
				DeletedFileID string `json:"deleted_file_id"`
			}{DeletedFileID: fileID})
		},
	}
}

func firmwareDeleteAllCommand() *cli.Command {
	return &cli.Command{
		Name:  "delete-all",
		Usage: "Delete every firmware file stored on the server",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			client, _, err := openClient(ctx, cmd)
			if err != nil {
				return err
			}
			defer func() { _ = client.Close() }()

			resp, err := client.FirmwareDeleteAll(ctx)
			if err != nil {
				return err
			}
			return printJSON(resp)
		},
	}
}

type firmwareUploadResult struct {
	FirmwareFileID string `json:"firmware_file_id"`
}

// runFirmwareUpload drives the full upload flow: fetch config, validate the
// local file, hash it, reuse the server copy on a checksum hit (unless force),
// and otherwise stream a direct or chunked upload depending on size. A nil
// progress writer suppresses all progress output.
func runFirmwareUpload(ctx context.Context, client *Client, path string, force bool, progress io.Writer) (firmwareUploadResult, bool, error) {
	var result firmwareUploadResult

	f, err := os.Open(path)
	if err != nil {
		return result, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return result, false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() {
		return result, false, fmt.Errorf("%s is a directory", path)
	}
	filename := filepath.Base(path)
	size := info.Size()

	cfg, err := client.FirmwareConfig(ctx)
	if err != nil {
		return result, false, err
	}
	if err := validateFirmwareFile(filename, size, cfg); err != nil {
		return result, false, err
	}

	if progress != nil {
		_, _ = fmt.Fprintf(progress, "computing sha256 of %s...\n", filename)
	}
	digest, err := sha256Hex(f)
	if err != nil {
		return result, false, fmt.Errorf("hash %s: %w", path, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return result, false, fmt.Errorf("rewind %s: %w", path, err)
	}

	check, err := client.FirmwareCheck(ctx, digest)
	if err != nil {
		return result, false, err
	}
	if check.Exists && check.FirmwareFileID != "" && !force {
		result.FirmwareFileID = check.FirmwareFileID
		return result, true, nil
	}

	reporter := newProgressPrinter(progress, size)
	var fileID string
	if size <= cfg.ChunkSizeBytes {
		fileID, err = client.FirmwareUploadDirect(ctx, filename, f, reporter)
	} else {
		fileID, err = client.FirmwareUploadChunked(ctx, filename, f, size, cfg.ChunkSizeBytes, reporter)
	}
	if reporter != nil {
		_, _ = fmt.Fprintln(progress)
	}
	if err != nil {
		return result, false, err
	}
	result.FirmwareFileID = fileID
	return result, false, nil
}

// validateFirmwareFile applies the same local checks as the web client before
// any bytes are hashed or uploaded.
func validateFirmwareFile(filename string, size int64, cfg *firmwareConfig) error {
	if filename == "" {
		return fmt.Errorf("firmware file must have a name")
	}
	if !hasAllowedExtension(filename, cfg.AllowedExtensions) {
		return fmt.Errorf("unsupported firmware file type %q (allowed: %s)", filename, strings.Join(cfg.AllowedExtensions, ", "))
	}
	if size == 0 {
		return fmt.Errorf("firmware file %q is empty", filename)
	}
	if size > cfg.MaxFileSizeBytes {
		return fmt.Errorf("firmware file %q is %d bytes, exceeding the maximum of %d bytes", filename, size, cfg.MaxFileSizeBytes)
	}
	return nil
}

// hasAllowedExtension suffix-matches because allowed extensions include
// multi-dot suffixes like ".tar.gz" that filepath.Ext cannot represent.
func hasAllowedExtension(name string, allowed []string) bool {
	lower := strings.ToLower(name)
	for _, ext := range allowed {
		if strings.HasSuffix(lower, strings.ToLower(ext)) {
			return true
		}
	}
	return false
}

// fileSHA256 streams path through SHA-256 and returns the lowercase hex
// digest the firmware check endpoint expects.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	digest, err := sha256Hex(f)
	if err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return digest, nil
}

func sha256Hex(r io.Reader) (string, error) {
	hasher := sha256.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return "", fmt.Errorf("hash content: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// singleArg returns the exactly-one positional argument or a usage error.
func singleArg(cmd *cli.Command, what string) (string, error) {
	if cmd.Args().Len() != 1 {
		return "", fmt.Errorf("expected exactly one argument: %s", what)
	}
	return cmd.Args().First(), nil
}

// progressFunc receives the cumulative number of uploaded bytes; a nil
// function disables progress reporting.
type progressFunc func(sent int64)

// newProgressPrinter returns a progressFunc that rewrites a percent line on w
// each time the integer percent changes; a nil writer disables reporting.
func newProgressPrinter(w io.Writer, total int64) progressFunc {
	if w == nil || total <= 0 {
		return nil
	}
	lastPercent := int64(-1)
	return func(sent int64) {
		percent := sent * 100 / total
		if percent == lastPercent {
			return
		}
		lastPercent = percent
		_, _ = fmt.Fprintf(w, "\rfirmware upload: %d%% (%d/%d bytes)", percent, sent, total)
	}
}

// countingReader reports a running byte total to fn as the wrapped reader is
// consumed; base offsets the total for chunks that resume mid-file.
type countingReader struct {
	r    io.Reader
	base int64
	n    int64
	fn   progressFunc
}

func (cr *countingReader) Read(p []byte) (int, error) {
	n, err := cr.r.Read(p)
	if n > 0 {
		cr.n += int64(n)
		if cr.fn != nil {
			cr.fn(cr.base + cr.n)
		}
	}
	return n, err //nolint:wrapcheck // io.Reader contract: io.EOF must be returned unwrapped
}
