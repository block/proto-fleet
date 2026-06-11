package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// The firmware file lifecycle is served by plain JSON/multipart HTTP endpoints
// rather than protobuf RPCs, so these helpers sit next to the RPC client
// instead of going through invoke().

const firmwareAPIPrefix = "api/v1/firmware"

const (
	contentTypeJSON        = "application/json"
	contentTypeOctetStream = "application/octet-stream"
)

// Fallback limits mirror the web client for servers whose firmware config
// omits the corresponding fields.
const (
	defaultFirmwareChunkSizeBytes   int64 = 32 * 1024 * 1024
	defaultFirmwareMaxFileSizeBytes int64 = 500 * 1024 * 1024
)

type firmwareConfig struct {
	AllowedExtensions []string `json:"allowed_extensions"`
	MaxFileSizeBytes  int64    `json:"max_file_size_bytes"`
	ChunkSizeBytes    int64    `json:"chunk_size_bytes"`
}

type firmwareCheckRequest struct {
	SHA256 string `json:"sha256"`
}

type firmwareCheckResponse struct {
	Exists         bool   `json:"exists"`
	FirmwareFileID string `json:"firmware_file_id,omitempty"`
}

type firmwareFileInfo struct {
	ID         string `json:"id"`
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	UploadedAt string `json:"uploaded_at"`
}

type firmwareListResponse struct {
	Files []firmwareFileInfo `json:"files"`
}

type firmwareDeleteAllResponse struct {
	DeletedCount int `json:"deleted_count"`
}

type firmwareInitiateRequest struct {
	Filename string `json:"filename"`
	FileSize int64  `json:"file_size"`
}

type firmwareInitiateResponse struct {
	UploadID string `json:"upload_id"`
}

type firmwareUploadResponse struct {
	FirmwareFileID string `json:"firmware_file_id"`
}

// firmwareURL builds an endpoint URL under the firmware HTTP API, which lives
// next to the RPC root on the same normalized base URL.
func (c *Client) firmwareURL(parts ...string) *url.URL {
	return c.baseURL.JoinPath(append([]string{firmwareAPIPrefix}, parts...)...)
}

// ensureFirmwareSession wraps ensureSession with a firmware-specific message:
// the firmware HTTP endpoints accept session cookies only, so an API key is
// not a substitute for username/password here. The credential hint is added
// only when credentials are actually missing, so login failures with other
// causes (unreachable server, wrong password) report their real error.
func (c *Client) ensureFirmwareSession(ctx context.Context) error {
	err := c.ensureSession(ctx)
	if err == nil {
		return nil
	}
	if c.username == "" || c.password == "" {
		return fmt.Errorf("firmware commands require --username/--password (%s/%s) because the firmware API does not accept API keys: %w", envFleetUsername, envFleetPassword, err)
	}
	return fmt.Errorf("establish firmware session: %w", err)
}

// firmwareTransferClient returns a client without an overall timeout for
// firmware data transfer: a large upload (or a single chunk on a slow link)
// can exceed the default 30s budget. It shares the cookie jar and transport
// so session auth and TLS settings still apply; cancellation comes from ctx.
func (c *Client) firmwareTransferClient() *http.Client {
	return &http.Client{Jar: c.httpClient.Jar, Transport: c.httpClient.Transport}
}

// firmwareRequest describes one firmware REST call for doFirmware.
type firmwareRequest struct {
	method        string
	url           *url.URL
	body          io.Reader
	contentLength int64  // set explicitly when body is not a type http.NewRequest can measure
	contentType   string // omitted when empty
	contentRange  string // omitted when empty
	transfer      bool   // use the timeout-free transfer client for large bodies
	out           any    // decoded from the JSON response body when non-nil
}

func (c *Client) doFirmware(ctx context.Context, r firmwareRequest) error {
	// JoinPath keeps the path relative when the base URL has none, so strip
	// the base prefix and re-anchor to render a stable "GET /api/v1/..." label.
	method := r.method + " /" + strings.TrimPrefix(strings.TrimPrefix(r.url.Path, c.baseURL.Path), "/")

	if err := c.ensureFirmwareSession(ctx); err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, r.method, r.url.String(), r.body)
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}
	if r.contentLength > 0 {
		httpReq.ContentLength = r.contentLength
	}
	if r.contentType != "" {
		httpReq.Header.Set("Content-Type", r.contentType)
	}
	if r.contentRange != "" {
		httpReq.Header.Set("Content-Range", r.contentRange)
	}
	httpReq.Header.Set("Accept", contentTypeJSON)

	httpClient := c.httpClient
	if r.transfer {
		httpClient = c.firmwareTransferClient()
	}
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("call %s: %w", method, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return &APIError{Method: method, Status: httpResp.Status, Body: respBody}
	}

	if r.out == nil || len(bytes.TrimSpace(respBody)) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, r.out); err != nil {
		return fmt.Errorf("decode %s response: %w", method, err)
	}
	return nil
}

// FirmwareConfig fetches upload constraints, substituting the web client's
// fallback chunk and max sizes when the server omits them.
func (c *Client) FirmwareConfig(ctx context.Context) (*firmwareConfig, error) {
	cfg := &firmwareConfig{}
	if err := c.doFirmware(ctx, firmwareRequest{
		method: http.MethodGet,
		url:    c.firmwareURL("config"),
		out:    cfg,
	}); err != nil {
		return nil, err
	}
	if len(cfg.AllowedExtensions) == 0 {
		return nil, fmt.Errorf("firmware config did not include allowed extensions")
	}
	if cfg.MaxFileSizeBytes <= 0 {
		cfg.MaxFileSizeBytes = defaultFirmwareMaxFileSizeBytes
	}
	if cfg.ChunkSizeBytes <= 0 {
		cfg.ChunkSizeBytes = defaultFirmwareChunkSizeBytes
	}
	return cfg, nil
}

// FirmwareCheck asks the server whether a firmware file with the given
// SHA-256 hex digest already exists.
func (c *Client) FirmwareCheck(ctx context.Context, sha256Hex string) (*firmwareCheckResponse, error) {
	body, err := json.Marshal(firmwareCheckRequest{SHA256: sha256Hex})
	if err != nil {
		return nil, fmt.Errorf("marshal firmware check request: %w", err)
	}
	resp := &firmwareCheckResponse{}
	if err := c.doFirmware(ctx, firmwareRequest{
		method:      http.MethodPost,
		url:         c.firmwareURL("check"),
		body:        bytes.NewReader(body),
		contentType: contentTypeJSON,
		out:         resp,
	}); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) FirmwareList(ctx context.Context) (*firmwareListResponse, error) {
	resp := &firmwareListResponse{}
	if err := c.doFirmware(ctx, firmwareRequest{
		method: http.MethodGet,
		url:    c.firmwareURL("files"),
		out:    resp,
	}); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) FirmwareDelete(ctx context.Context, fileID string) error {
	return c.doFirmware(ctx, firmwareRequest{
		method: http.MethodDelete,
		url:    c.firmwareURL("files", url.PathEscape(fileID)),
	})
}

func (c *Client) FirmwareDeleteAll(ctx context.Context) (*firmwareDeleteAllResponse, error) {
	resp := &firmwareDeleteAllResponse{}
	if err := c.doFirmware(ctx, firmwareRequest{
		method: http.MethodDelete,
		url:    c.firmwareURL("files"),
		out:    resp,
	}); err != nil {
		return nil, err
	}
	return resp, nil
}

// FirmwareUploadDirect streams the file as a single multipart request. The
// body is piped so the file is never buffered in memory, which means the
// request goes out with chunked transfer encoding instead of a Content-Length.
func (c *Client) FirmwareUploadDirect(ctx context.Context, filename string, file io.Reader, progress progressFunc) (string, error) {
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", filename)
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		if _, err := io.Copy(part, &countingReader{r: file, fn: progress}); err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		_ = pw.CloseWithError(mw.Close())
	}()

	resp := &firmwareUploadResponse{}
	if err := c.doFirmware(ctx, firmwareRequest{
		method:      http.MethodPost,
		url:         c.firmwareURL("upload"),
		body:        pr,
		contentType: mw.FormDataContentType(),
		transfer:    true,
		out:         resp,
	}); err != nil {
		return "", err
	}
	return resp.FirmwareFileID, nil
}

// FirmwareUploadChunked uploads the file through the initiate/chunk/complete
// flow. Chunks are sent sequentially because the server rejects out-of-order
// ranges.
func (c *Client) FirmwareUploadChunked(ctx context.Context, filename string, file io.ReaderAt, size, chunkSize int64, progress progressFunc) (string, error) {
	body, err := json.Marshal(firmwareInitiateRequest{Filename: filename, FileSize: size})
	if err != nil {
		return "", fmt.Errorf("marshal chunked upload initiate request: %w", err)
	}
	initiate := &firmwareInitiateResponse{}
	if err := c.doFirmware(ctx, firmwareRequest{
		method:      http.MethodPost,
		url:         c.firmwareURL("upload", "chunked"),
		body:        bytes.NewReader(body),
		contentType: contentTypeJSON,
		out:         initiate,
	}); err != nil {
		return "", err
	}
	if initiate.UploadID == "" {
		return "", fmt.Errorf("chunked upload initiate did not return an upload id")
	}

	for start := int64(0); start < size; start += chunkSize {
		end := min(start+chunkSize, size)
		section := io.NewSectionReader(file, start, end-start)
		if err := c.doFirmware(ctx, firmwareRequest{
			method:        http.MethodPut,
			url:           c.firmwareURL("upload", "chunked", initiate.UploadID),
			body:          &countingReader{r: section, base: start, fn: progress},
			contentLength: end - start,
			contentType:   contentTypeOctetStream,
			contentRange:  fmt.Sprintf("bytes %d-%d/%d", start, end-1, size),
			transfer:      true,
		}); err != nil {
			return "", err
		}
	}

	resp := &firmwareUploadResponse{}
	if err := c.doFirmware(ctx, firmwareRequest{
		method:   http.MethodPost,
		url:      c.firmwareURL("upload", "chunked", initiate.UploadID, "complete"),
		transfer: true,
		out:      resp,
	}); err != nil {
		return "", err
	}
	return resp.FirmwareFileID, nil
}
