package management

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
)

type authUploadError struct {
	Code int
	Err  error
}

func (e *authUploadError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

func (e *authUploadError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newAuthUploadError(code int, format string, args ...any) error {
	return &authUploadError{
		Code: code,
		Err:  fmt.Errorf(format, args...),
	}
}

func authUploadStatusCode(err error) int {
	var uploadErr *authUploadError
	if errors.As(err, &uploadErr) && uploadErr.Code > 0 {
		return uploadErr.Code
	}
	return http.StatusInternalServerError
}

type authUploadFailure struct {
	Name  string
	Error string
	Code  int
}

type authUploadSummary struct {
	Mode     string
	Imported int
	Skipped  int
	Failed   []authUploadFailure
}

func (s *authUploadSummary) addFailure(name string, err error) {
	if s == nil || err == nil {
		return
	}
	s.Failed = append(s.Failed, authUploadFailure{
		Name:  strings.TrimSpace(name),
		Error: err.Error(),
		Code:  authUploadStatusCode(err),
	})
}

func (s *authUploadSummary) merge(other authUploadSummary) {
	if s == nil {
		return
	}
	s.Imported += other.Imported
	s.Skipped += other.Skipped
	s.Failed = append(s.Failed, other.Failed...)
	if s.Mode == "" {
		s.Mode = other.Mode
		return
	}
	if other.Mode != "" && s.Mode != other.Mode {
		s.Mode = "multipart"
	}
}

func (s *authUploadSummary) singleFailure() *authUploadFailure {
	if s == nil {
		return nil
	}
	if s.Imported != 0 || len(s.Failed) != 1 {
		return nil
	}
	return &s.Failed[0]
}

func (s *authUploadSummary) toResponse() gin.H {
	status := "ok"
	if len(s.Failed) > 0 && s.Imported > 0 {
		status = "partial"
	} else if len(s.Failed) > 0 {
		status = "error"
	}

	response := gin.H{
		"status":   status,
		"imported": s.Imported,
	}
	if s.Mode != "" {
		response["mode"] = s.Mode
	}
	if s.Skipped > 0 {
		response["skipped"] = s.Skipped
	}
	if len(s.Failed) > 0 {
		failed := make([]gin.H, 0, len(s.Failed))
		for _, item := range s.Failed {
			entry := gin.H{"error": item.Error}
			if item.Name != "" {
				entry["name"] = item.Name
			}
			failed = append(failed, entry)
		}
		response["failed"] = failed
	}
	return response
}

func (h *Handler) handleMultipartAuthUpload(ctx context.Context, c *gin.Context) (*authUploadSummary, bool, error) {
	contentType := strings.ToLower(strings.TrimSpace(c.GetHeader("Content-Type")))
	if !strings.HasPrefix(contentType, "multipart/form-data") {
		return nil, false, nil
	}

	form, err := c.MultipartForm()
	if err != nil {
		return nil, true, newAuthUploadError(http.StatusBadRequest, "failed to read multipart form: %v", err)
	}

	files := form.File["file"]
	if len(files) == 0 {
		return nil, true, newAuthUploadError(http.StatusBadRequest, "missing multipart file")
	}

	summary := &authUploadSummary{}
	for _, fileHeader := range files {
		summary.merge(h.processMultipartAuthFile(ctx, fileHeader))
	}

	if len(files) > 1 {
		summary.Mode = "multipart"
	}
	if summary.Mode == "" {
		summary.Mode = "single"
	}
	if failure := summary.singleFailure(); failure != nil {
		return summary, true, &authUploadError{
			Code: failure.Code,
			Err:  errors.New(failure.Error),
		}
	}
	return summary, true, nil
}

func (h *Handler) processMultipartAuthFile(ctx context.Context, fileHeader *multipart.FileHeader) authUploadSummary {
	summary := authUploadSummary{}
	if fileHeader == nil {
		summary.addFailure("", newAuthUploadError(http.StatusBadRequest, "uploaded file is missing"))
		return summary
	}

	file, err := fileHeader.Open()
	if err != nil {
		summary.addFailure(fileHeader.Filename, newAuthUploadError(http.StatusBadRequest, "failed to open uploaded file: %v", err))
		return summary
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		summary.addFailure(fileHeader.Filename, newAuthUploadError(http.StatusBadRequest, "failed to read uploaded file: %v", err))
		return summary
	}

	name := filepath.Base(strings.TrimSpace(fileHeader.Filename))
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		summary.Mode = "single"
		if err := h.saveAuthJSON(ctx, name, data); err != nil {
			summary.addFailure(name, err)
			return summary
		}
		summary.Imported = 1
	case ".zip":
		summary = h.importAuthZipArchive(ctx, name, data)
	default:
		summary.addFailure(name, newAuthUploadError(http.StatusBadRequest, "file must be .json or .zip"))
	}

	return summary
}

func (h *Handler) saveAuthJSON(ctx context.Context, name string, data []byte) error {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." {
		return newAuthUploadError(http.StatusBadRequest, "invalid file name")
	}
	if !strings.HasSuffix(strings.ToLower(name), ".json") {
		return newAuthUploadError(http.StatusBadRequest, "file must be .json")
	}
	if err := os.MkdirAll(h.cfg.AuthDir, 0o700); err != nil {
		return newAuthUploadError(http.StatusInternalServerError, "failed to prepare auth dir: %v", err)
	}

	dst := filepath.Join(h.cfg.AuthDir, name)
	if !filepath.IsAbs(dst) {
		if abs, errAbs := filepath.Abs(dst); errAbs == nil {
			dst = abs
		}
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return newAuthUploadError(http.StatusInternalServerError, "failed to write file: %v", err)
	}
	if err := h.registerAuthFromFile(ctx, dst, data); err != nil {
		code := http.StatusInternalServerError
		if strings.Contains(strings.ToLower(err.Error()), "invalid auth file") {
			code = http.StatusBadRequest
		}
		return &authUploadError{Code: code, Err: err}
	}
	return nil
}

func (h *Handler) importAuthZipArchive(ctx context.Context, archiveName string, data []byte) authUploadSummary {
	summary := authUploadSummary{Mode: "zip"}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		summary.addFailure(archiveName, newAuthUploadError(http.StatusBadRequest, "invalid zip file: %v", err))
		return summary
	}

	seen := make(map[string]struct{})
	for _, file := range reader.File {
		if file == nil || file.FileInfo().IsDir() {
			summary.Skipped++
			continue
		}

		name := zipEntryBaseName(file.Name)
		if name == "" {
			summary.Skipped++
			continue
		}
		if !strings.HasSuffix(strings.ToLower(name), ".json") {
			summary.Skipped++
			continue
		}

		key := strings.ToLower(name)
		if _, exists := seen[key]; exists {
			summary.Skipped++
			continue
		}
		seen[key] = struct{}{}

		entry, err := file.Open()
		if err != nil {
			summary.addFailure(name, newAuthUploadError(http.StatusBadRequest, "failed to open zip entry: %v", err))
			continue
		}

		entryData, readErr := io.ReadAll(entry)
		closeErr := entry.Close()
		if readErr != nil {
			summary.addFailure(name, newAuthUploadError(http.StatusBadRequest, "failed to read zip entry: %v", readErr))
			continue
		}
		if closeErr != nil {
			summary.addFailure(name, newAuthUploadError(http.StatusBadRequest, "failed to close zip entry: %v", closeErr))
			continue
		}

		if err := h.saveAuthJSON(ctx, name, entryData); err != nil {
			summary.addFailure(name, err)
			continue
		}
		summary.Imported++
	}

	if summary.Imported == 0 && len(summary.Failed) == 0 {
		summary.addFailure(archiveName, newAuthUploadError(http.StatusBadRequest, "zip file does not contain any .json auth files"))
	}

	return summary
}

func zipEntryBaseName(name string) string {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return ""
	}
	cleanName = strings.ReplaceAll(cleanName, "\\", "/")
	base := path.Base(cleanName)
	if base == "." || base == "/" {
		return ""
	}
	return filepath.Base(base)
}
