package management

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestUploadAuthFile_ZipMultipartImportsJSONEntries(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	manager := coreauth.NewManager(nil, nil, nil)
	handler := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	handler.tokenStore = &memoryAuthStore{}

	zipPayload := buildZipPayload(t, map[string]string{
		"nested/codex-a.json": `{"type":"codex","email":"zip-a@example.com"}`,
		"deep/path/codex-b.json": `{"type":"codex","email":"zip-b@example.com"}`,
		"notes.txt": `ignore me`,
	})

	request := newMultipartUploadRequest(t, map[string][]byte{
		"bundle.zip": zipPayload,
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request

	handler.UploadAuthFile(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got := int(payload["imported"].(float64)); got != 2 {
		t.Fatalf("expected imported=2, got %d", got)
	}
	if got := int(payload["skipped"].(float64)); got != 1 {
		t.Fatalf("expected skipped=1, got %d", got)
	}

	assertFileExists(t, filepath.Join(authDir, "codex-a.json"))
	assertFileExists(t, filepath.Join(authDir, "codex-b.json"))

	records := manager.List()
	if len(records) != 2 {
		t.Fatalf("expected 2 auth records, got %d", len(records))
	}
}

func TestUploadAuthFile_MultipartBatchImportsMultipleJSONFiles(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	manager := coreauth.NewManager(nil, nil, nil)
	handler := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	handler.tokenStore = &memoryAuthStore{}

	request := newMultipartUploadRequest(t, map[string][]byte{
		"first.json":  []byte(`{"type":"codex","email":"first@example.com"}`),
		"second.json": []byte(`{"type":"codex","email":"second@example.com"}`),
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request

	handler.UploadAuthFile(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d with body %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got := int(payload["imported"].(float64)); got != 2 {
		t.Fatalf("expected imported=2, got %d", got)
	}

	assertFileExists(t, filepath.Join(authDir, "first.json"))
	assertFileExists(t, filepath.Join(authDir, "second.json"))

	records := manager.List()
	if len(records) != 2 {
		t.Fatalf("expected 2 auth records, got %d", len(records))
	}
}

func newMultipartUploadRequest(t *testing.T, files map[string][]byte) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, data := range files {
		part, err := writer.CreateFormFile("file", name)
		if err != nil {
			t.Fatalf("failed to create multipart file %s: %v", name, err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatalf("failed to write multipart file %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v0/management/auth-files", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func buildZipPayload(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buffer bytes.Buffer
	zipWriter := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := zipWriter.Create(name)
		if err != nil {
			t.Fatalf("failed to create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatalf("failed to write zip entry %s: %v", name, err)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("failed to close zip writer: %v", err)
	}
	return buffer.Bytes()
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file %s to exist, got err %v", path, err)
	}
}
