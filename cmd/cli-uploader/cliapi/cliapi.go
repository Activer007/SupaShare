package cliapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliflags"
	"github.com/Activer007/SupaShare/internal/encryption"
	"github.com/Activer007/SupaShare/internal/encryption/end2end"
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/safedisplay"
	"github.com/Activer007/SupaShare/internal/serviceurl"
	"github.com/schollz/progressbar/v3"
)

var gokapiUrl string
var apiKey string
var e2eKey []byte

const megaByte = 1024 * 1024

type header struct {
	Key   string
	Value string
}

// ErrInvalidRequest is returned when the API returns a 400
var ErrInvalidRequest = errors.New("400 Bad Request")

// ErrUnauthorised is returned when the API returns a 401
var ErrUnauthorised = errors.New("unauthorised")

// ErrNotFound is returned when the API returns a 404
var ErrNotFound = errors.New("404 Not Found")

// ErrFileTooBig is returned when the file size exceeds the allowed limit
var ErrFileTooBig = errors.New("file too big")

// ErrE2eKeyIncorrect is returned when the e2e key is incorrect
var ErrE2eKeyIncorrect = errors.New("e2e key incorrect")

// ErrE2eNotSetUp is returned when end-to-end encryption has not been set up on the server yet
var ErrE2eNotSetUp = errors.New("e2e has not been set up on the server")

// Init initialises the API client with the given url and key.
// The key is used for authentication.
// The end2endKey is used for end-to-end encryption.
func Init(rawURL, key string, end2endKey []byte) error {
	normalized, err := serviceurl.Normalize(rawURL)
	if err != nil {
		gokapiUrl, apiKey, e2eKey = "", "", nil
		return err
	}
	gokapiUrl, apiKey, e2eKey = normalized+"/api", key, end2endKey
	return nil
}

// GetVersion returns the version of the SupaShare server
func GetVersion() (string, int, error) {
	result, err := getUrl(gokapiUrl+"/info/version", []header{}, false)
	if err != nil {
		return "", 0, err
	}
	type expectedFormat struct {
		Version    string
		VersionInt int
	}
	var parsedResult expectedFormat
	err = json.Unmarshal([]byte(result), &parsedResult)
	if err != nil {
		return "", 0, err
	}
	if parsedResult.Version == "" {
		parsedResult.Version = "unknown"
	}
	return parsedResult.Version, parsedResult.VersionInt, nil
}

// GetConfig returns the upload configuration of the SupaShare server
func GetConfig() (int, int, bool, error) {
	result, err := getUrl(gokapiUrl+"/info/config", []header{}, false)
	if err != nil {
		return 0, 0, false, err
	}
	type expectedFormat struct {
		MaxFilesize               int
		MaxChunksize              int
		EndToEndEncryptionEnabled bool
	}
	var parsedResult expectedFormat
	err = json.Unmarshal([]byte(result), &parsedResult)
	if err != nil {
		return 0, 0, false, err
	}
	if err := validateTransferConfig(parsedResult.MaxFilesize, parsedResult.MaxChunksize); err != nil {
		return 0, 0, false, err
	}
	return parsedResult.MaxFilesize, parsedResult.MaxChunksize, parsedResult.EndToEndEncryptionEnabled, nil
}

func getUrl(url string, headers []header, longTimeout bool) (string, error) {
	timeout := 30 * time.Second
	if longTimeout {
		timeout = 30 * time.Minute
	}
	client := newHTTPClient(timeout, false)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Add("apikey", apiKey)
	for _, addHeader := range headers {
		req.Header.Add(addHeader.Key, addHeader.Value)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 400:
		return "", ErrInvalidRequest
	case 401:
		return "", ErrUnauthorised
	case 404:
		return "", ErrNotFound
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	body, err := readBoundedBody(resp.Body, maxMetadataBytes)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// UploadFile uploads a file to the SupaShare server
func UploadFile(uploadParams cliflags.FlagConfig) (models.FileApiOutput, error) {
	var progressBar *progressbar.ProgressBar
	file, err := os.OpenFile(uploadParams.File, os.O_RDONLY, 0664)
	if err != nil {
		fmt.Println("ERROR: Could not open file to upload")
		fmt.Println(safedisplay.Text(err.Error()))
		os.Exit(4)
	}
	defer file.Close()
	maxSize, chunkSize, isE2e, err := GetConfig()
	if err != nil {
		return models.FileApiOutput{}, err
	}

	if len(e2eKey) == 0 || !isE2e || uploadParams.DisableE2e {
		isE2e = false
	}
	fileStat, err := file.Stat()
	if err != nil {
		return models.FileApiOutput{}, err
	}
	if !fileStat.Mode().IsRegular() || fileStat.Size() < 0 {
		return models.FileApiOutput{}, errors.New("upload requires a regular file")
	}
	sizeBytes := fileStat.Size()
	realSize := fileStat.Size()
	if isE2e {
		encryptedSize := encryption.CalculateEncryptedFilesize(sizeBytes)
		if encryptedSize < sizeBytes {
			return models.FileApiOutput{}, ErrFileTooBig
		}
		sizeBytes = encryptedSize
	}
	if sizeBytes > int64(maxSize)*megaByte {
		return models.FileApiOutput{}, ErrFileTooBig
	}
	uuid := helper.GenerateRandomString(30)

	if !uploadParams.JsonOutput {
		progressBar = progressbar.DefaultBytes(-1, "uploading")
	}

	if isE2e {
		cipher, err := encryption.GetRandomCipher()
		if err != nil {
			return models.FileApiOutput{}, err
		}
		stream, err := encryption.GetEncryptReader(cipher, file)
		if err != nil {
			return models.FileApiOutput{}, err
		}
		for i := int64(0); i < sizeBytes; {
			amount := min(int64(chunkSize)*megaByte, sizeBytes-i)
			err = uploadChunk(stream, uuid, i, amount, sizeBytes, progressBar)
			i += amount
			if err != nil {
				return models.FileApiOutput{}, err
			}
		}
		metaData, err := completeChunk(uuid, "Encrypted File", sizeBytes, realSize, true, uploadParams, progressBar)
		if err != nil {
			return models.FileApiOutput{}, err
		}

		e2eFile := models.E2EFile{
			Uuid:     uuid,
			Id:       metaData.Id,
			Filename: getFileName(file, uploadParams),
			Cipher:   cipher,
		}
		err = addE2EFileInfo(e2eFile)
		if err != nil {
			return models.FileApiOutput{}, err
		}
		hashContent, err := getHashContent(e2eFile)
		metaData.UrlDownload = metaData.UrlDownload + "#" + hashContent
		metaData.Name = getFileName(file, uploadParams)
		return metaData, err
	}

	for i := int64(0); i < sizeBytes; {
		amount := min(int64(chunkSize)*megaByte, sizeBytes-i)
		err = uploadChunk(file, uuid, i, amount, sizeBytes, progressBar)
		i += amount
		if err != nil {
			return models.FileApiOutput{}, err
		}
	}
	metaData, err := completeChunk(uuid, nameToBase64(file, uploadParams), sizeBytes, realSize, false, uploadParams, progressBar)
	if err != nil {
		return models.FileApiOutput{}, err
	}
	return metaData, nil
}

// DownloadFile downloads a file from the SupaShare server
func DownloadFile(params cliflags.FlagConfig) error {
	info, err := getFileInfo(params.DownloadId)
	if err != nil {
		return err
	}
	limit := params.MaxDownloadBytes
	if limit == 0 {
		limit = cliflags.DefaultMaxDownloadBytes
	}
	if limit < 0 || info.SizeBytes < 0 || info.SizeBytes > limit {
		return errors.New("download exceeds client byte limit or has invalid size")
	}
	// Match the server's legacy ordinary/raw read compatibility. Collection
	// and encrypted records still require the exact advertised metadata size.
	legacyUnknownSize := info.SizeBytes == 0 && !info.IsFileRequest && info.FileRequestId == "" &&
		!info.IsEncrypted && !info.IsEndToEndEncrypted
	var cipher []byte
	if info.IsEndToEndEncrypted {
		if len(e2eKey) == 0 {
			return errors.New("E2E key is not configured")
		}
		var name string
		cipher, name, err = getE2eCipher(params.DownloadId)
		if err != nil {
			return err
		}
		if params.FileName == "" {
			info.Name = name
		}
	}
	if params.OutputPath == "" {
		params.OutputPath = "."
	}
	if params.FileName == "" {
		params.FileName = strings.NewReplacer("/", "_", "\\", "_").Replace(info.Name)
	}
	name := filepath.Base(params.FileName)
	if name == "." || name == ".." || name == "" {
		return errors.New("invalid download filename")
	}
	if err = os.Mkdir(params.OutputPath, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	root, err := os.OpenRoot(params.OutputPath)
	if err != nil {
		return err
	}
	defer root.Close()
	// Pin the output directory, and never follow or truncate an existing leaf.
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot exclusively create download; choose a new filename")
	}
	defer file.Close()
	// Do not unlink by pathname on failure: another directory writer could have
	// replaced that name. Keep the private partial for explicit user recovery.
	fail := func(err error) error {
		return fmt.Errorf("download incomplete; private partial may remain, verify before cleanup: %w", err)
	}
	req, err := http.NewRequest("GET", gokapiUrl+"/files/download/"+params.DownloadId, nil)
	if err != nil {
		return fail(err)
	}
	req.Header.Add("apikey", apiKey)
	resp, err := newHTTPClient(30*time.Minute, true).Do(req)
	if err != nil {
		return fail(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fail(fmt.Errorf("download HTTP status %d", resp.StatusCode))
	}
	expectedWireSize := info.SizeBytes
	if legacyUnknownSize {
		expectedWireSize = resp.ContentLength
	}
	if resp.ContentLength > limit {
		return fail(errors.New("download content length exceeds client byte limit"))
	}
	if expectedWireSize >= 0 && resp.ContentLength >= 0 && resp.ContentLength != expectedWireSize {
		return fail(errors.New("download content length does not match metadata"))
	}
	wireBudget := expectedWireSize
	if wireBudget < 0 {
		wireBudget = limit
	}
	wire := &downloadReader{r: resp.Body, remaining: wireBudget}
	var body io.Reader = wire
	if info.IsEndToEndEncrypted {
		body, err = encryption.GetDecryptReader(cipher, wire)
		if err != nil {
			return fail(err)
		}
	}
	var output io.Reader = &downloadReader{r: body, remaining: limit}
	if !params.JsonOutput {
		bar := progressbar.DefaultBytes(expectedWireSize, "Downloading")
		defer bar.Close()
		output = io.TeeReader(output, bar)
	}
	_, err = io.Copy(file, output)
	if err != nil {
		return fail(err)
	}
	// E2E SizeBytes is ciphertext size; plaintext size is not exposed by this API.
	// Consume to authenticated EOF and check the wire length whenever known.
	if expectedWireSize >= 0 && wire.remaining != 0 {
		return fail(errors.New("download ended before advertised size"))
	}
	if err = file.Sync(); err != nil {
		return fail(err)
	}
	if err = file.Close(); err != nil {
		return fail(err)
	}
	if params.RemoveRemote {
		if err = deleteRemoteFile(params.DownloadId); err != nil {
			return err
		}
	}
	if params.JsonOutput {
		fmt.Println("{\"result\":\"OK\"}")
	} else {
		fmt.Println("File downloaded successfully")
		fmt.Println("File saved to : " + safedisplay.Text(filepath.Join(params.OutputPath, name)))
	}
	return nil
}

// getE2eCipher retrieves the per-file cipher and real filename for an E2E encrypted file
func getE2eCipher(fileId string) ([]byte, string, error) {
	e2eInfo, err := GetE2eInfo(true)
	if err != nil {
		return nil, "", err
	}
	for _, f := range e2eInfo.Files {
		if f.Id == fileId {
			return f.Cipher, f.Filename, nil
		}
	}
	return nil, "", errors.New("file not found in E2E metadata")
}

func nameToBase64(f *os.File, uploadParams cliflags.FlagConfig) string {
	return "base64:" + base64.StdEncoding.EncodeToString([]byte(getFileName(f, uploadParams)))
}

func getFileName(f *os.File, uploadParams cliflags.FlagConfig) string {
	if uploadParams.FileName != "" {
		return uploadParams.FileName
	}
	return filepath.Base(f.Name())
}

func getFileInfo(id string) (models.FileApiOutput, error) {
	result, err := getUrl(gokapiUrl+"/files/list/"+id, []header{}, false)
	if err != nil {
		return models.FileApiOutput{}, err
	}
	var parsedResult models.FileApiOutput
	err = json.Unmarshal([]byte(result), &parsedResult)
	if err != nil {
		return models.FileApiOutput{}, err
	}
	return parsedResult, nil
}

func deleteRemoteFile(id string) error {
	_, err := getUrl(gokapiUrl+"/files/delete", []header{{"id", id}}, false)
	return err
}

func uploadChunk(f io.Reader, uuid string, offset, chunkSize, filesize int64, progressBar *progressbar.ProgressBar) error {
	if chunkSize <= 0 || chunkSize > maxChunkBytes || offset < 0 || filesize < 0 || offset >= filesize {
		return errors.New("invalid upload chunk bounds")
	}
	remaining := filesize - offset
	if remaining < chunkSize {
		chunkSize = remaining
	}
	bodyReader, contentType, dataSize, err := multipartChunk(f, uuid, offset, chunkSize, filesize)
	if err != nil {
		return err
	}
	if progressBar != nil {
		bodyReader = io.TeeReader(bodyReader, progressBar)
	}

	r, err := http.NewRequest("POST", gokapiUrl+"/chunk/add", bodyReader)
	if err != nil {
		return err
	}
	r.ContentLength = dataSize
	r.Header.Set("Content-Type", contentType)
	r.Header.Set("apikey", apiKey)
	client := newHTTPClient(chunkRequestTimeout, false)
	resp, err := client.Do(r)

	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bodyContent, err := readBoundedBody(resp.Body, maxChunkResponseBytes)
	if err != nil {
		return err
	}
	response := string(bodyContent)
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorised
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("failed to upload chunk: status code " + strconv.Itoa(resp.StatusCode))
	}
	if response != "{\"result\":\"OK\"}" {
		return errors.New("failed to upload chunk: unexpected response")
	}
	return nil
}

func completeChunk(uid, filename string, filesize, realsize int64, useE2e bool, uploadParams cliflags.FlagConfig, progressBar *progressbar.ProgressBar) (models.FileApiOutput, error) {
	type expectedFormat struct {
		FileInfo models.FileApiOutput `json:"FileInfo"`
	}
	if progressBar != nil {
		_ = progressBar.Finish()
	}
	if !uploadParams.JsonOutput {
		fmt.Println("Finalising...")
	}
	result, err := getUrl(gokapiUrl+"/chunk/complete", []header{
		{"uuid", uid},
		{"filename", filename},
		{"filesize", strconv.FormatInt(filesize, 10)},
		{"realsize", strconv.FormatInt(realsize, 10)},
		{"isE2E", strconv.FormatBool(useE2e)},
		{"allowedDownloads", strconv.Itoa(uploadParams.ExpiryDownloads)},
		{"expiryDays", strconv.Itoa(uploadParams.ExpiryDays)},
		{"password", uploadParams.Password},
		{"contenttype", "application/octet-stream"},
	}, true)
	if err != nil {
		return models.FileApiOutput{}, err
	}
	var parsedResult expectedFormat
	err = json.Unmarshal([]byte(result), &parsedResult)
	if err != nil {
		return models.FileApiOutput{}, err
	}
	return parsedResult.FileInfo, nil
}

func acquireE2eLease() (string, error) {
	return acquireE2eLeaseContext(context.Background())
}

// Retry only admission rejection, never an already-started metadata transaction.
func acquireE2eLeaseContext(parent context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	client := newHTTPClient(2*time.Second, false)
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, gokapiUrl+"/e2e/mutex/lock", nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("apikey", apiKey)
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		busy := resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusTooManyRequests
		if busy {
			resp.Body.Close()
			if attempt == 3 {
				return "", errors.New("E2E metadata is busy; retry the operation")
			}
			timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			resp.Body.Close()
			return "", fmt.Errorf("E2E lease acquisition HTTP status %d", resp.StatusCode)
		}
		var response struct {
			LeaseToken string `json:"leaseToken"`
		}
		body, readErr := readBoundedBody(resp.Body, maxLeaseResponseBytes)
		err = readErr
		if err == nil {
			err = json.Unmarshal(body, &response)
		}
		resp.Body.Close()
		if err != nil {
			return "", err
		}
		if response.LeaseToken == "" {
			return "", errors.New("server did not provide an E2E lease; upgrade the server")
		}
		return response.LeaseToken, nil
	}
	return "", errors.New("E2E metadata is busy; retry the operation")
}

func releaseE2eLease(lease string) error {
	if lease == "" {
		return errors.New("missing E2E lease")
	}
	_, err := getUrl(gokapiUrl+"/e2e/mutex/unlock", []header{{"e2eLease", lease}}, false)
	return err
}

// GetE2eInfo returns the end-to-end encryption information of the SupaShare server for this user
func GetE2eInfo(_ bool) (models.E2EInfoPlainText, error) {
	// Keep the exported call shape, but never leak a lease across calls.
	lease, err := acquireE2eLease()
	if err != nil {
		return models.E2EInfoPlainText{}, err
	}
	defer releaseE2eLease(lease)
	return readE2eInfo(lease)
}

func readE2eInfo(lease string) (models.E2EInfoPlainText, error) {
	var result models.E2EInfoEncrypted
	var fileInfo models.E2EInfoPlainText

	resultJson, err := getUrl(gokapiUrl+"/e2e/get", []header{{"e2eLease", lease}}, false)
	if err != nil {
		return models.E2EInfoPlainText{}, err
	}
	err = json.Unmarshal([]byte(resultJson), &result)
	if err != nil {
		return models.E2EInfoPlainText{}, err
	}
	if !result.HasBeenSetUp() {
		return models.E2EInfoPlainText{}, ErrE2eNotSetUp
	}
	fileInfo, err = end2end.DecryptData(result, e2eKey)
	if err != nil {
		return models.E2EInfoPlainText{}, ErrE2eKeyIncorrect
	}
	return fileInfo, nil
}

func addE2EFileInfo(file models.E2EFile) error {
	lease, err := acquireE2eLease()
	if err != nil {
		return err
	}
	defer releaseE2eLease(lease)
	infoPlain, err := readE2eInfo(lease)
	if err != nil {
		return err
	}
	infoPlain.Files = append(infoPlain.Files, file)
	output, err := end2end.EncryptData(infoPlain.Files, e2eKey)
	if err != nil {
		return err
	}
	return setE2eInfo(output, lease)
}

// setE2eInfo sets the end-to-end encryption information of the SupaShare server for this user
// it does NOT set a lock itself on the SupaShare server
func setE2eInfo(input models.E2EInfoEncrypted, lease string) error {
	if lease == "" {
		return errors.New("missing E2E lease")
	}
	outputJson, err := json.Marshal(input)
	if err != nil {
		return err
	}
	content := base64.StdEncoding.EncodeToString(outputJson)

	apiURL := gokapiUrl + "/e2e/set"

	bodyData := map[string]string{
		"content": content,
	}
	bodyBytes, err := json.Marshal(bodyData)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apikey", apiKey)
	req.Header.Set("e2eLease", lease)

	resp, err := newHTTPClient(30*time.Second, false).Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("E2E metadata update rejected: HTTP status %d", resp.StatusCode)
	}
	return nil
}

func getHashContent(input models.E2EFile) (string, error) {
	output, err := json.Marshal(models.E2EHashContent{
		Filename: input.Filename,
		Cipher:   base64.StdEncoding.EncodeToString(input.Cipher),
	})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(output), nil
}
