package cliapi

import (
	"bytes"
	"errors"
	"io"
	"math"
	"mime/multipart"
	"strconv"
	"time"
)

// Local ceilings are independent of server-controlled metadata. Chunk timeout is
// an absolute per-request budget, not an unlimited sliding progress deadline.
const (
	maxChunkBytes         int64 = 64 * megaByte
	maxMetadataBytes      int64 = 8 * megaByte
	maxChunkResponseBytes int64 = 64 * 1024
	maxLeaseResponseBytes int64 = 4 * 1024
	chunkRequestTimeout         = 30 * time.Minute
)

func validateTransferConfig(maxFileMB, chunkMB int) error {
	if maxFileMB <= 0 || int64(maxFileMB) > math.MaxInt64/megaByte || chunkMB <= 0 || int64(chunkMB) > maxChunkBytes/megaByte {
		return errors.New("server transfer limits are invalid or exceed client safety limits (maximum chunk: 64 MiB)")
	}
	return nil
}

func readBoundedBody(r io.Reader, max int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("server response exceeds client safety limit")
	}
	return b, nil
}

// Encode only multipart framing in memory. The file body remains streaming and
// bounded. ContentLength lets the transport reject a prematurely exhausted file.
func multipartChunk(r io.Reader, uuid string, offset, size, total int64) (io.Reader, string, int64, error) {
	var frame bytes.Buffer
	writer := multipart.NewWriter(&frame)
	if _, err := writer.CreateFormFile("file", "uploadedfile"); err != nil {
		return nil, "", 0, err
	}
	prefix := append([]byte(nil), frame.Bytes()...)
	frame.Reset()
	for _, field := range []struct{ name, value string }{
		{"filesize", strconv.FormatInt(total, 10)}, {"offset", strconv.FormatInt(offset, 10)}, {"uuid", uuid},
	} {
		if err := writer.WriteField(field.name, field.value); err != nil {
			return nil, "", 0, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", 0, err
	}
	suffix := frame.Bytes()
	return io.MultiReader(bytes.NewReader(prefix), io.LimitReader(r, size), bytes.NewReader(suffix)), writer.FormDataContentType(), int64(len(prefix)) + size + int64(len(suffix)), nil
}
