package backend

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestRequestLoggerLevel(t *testing.T) {
	log := logrus.New()
	var buf bytes.Buffer
	log.SetOutput(&buf)
	log.SetLevel(logrus.DebugLevel)

	// Create a handler with RequestLogger
	logger := RequestLogger(log, "/debug-path")
	handler := logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Case 1: Normal path -> INFO
	reqInfo := httptest.NewRequest(http.MethodGet, "/info-path", nil)
	wInfo := httptest.NewRecorder()
	handler.ServeHTTP(wInfo, reqInfo)
	if !bytes.Contains(buf.Bytes(), []byte("level=info")) {
		t.Errorf("Expected info level for normal path, got: %s", buf.String())
	}
	buf.Reset()

	// Case 2: Debug path -> DEBUG
	reqDebug := httptest.NewRequest(http.MethodGet, "/debug-path", nil)
	wDebug := httptest.NewRecorder()
	handler.ServeHTTP(wDebug, reqDebug)
	if !bytes.Contains(buf.Bytes(), []byte("level=debug")) {
		t.Errorf("Expected debug level for debug path, got: %s", buf.String())
	}
	buf.Reset()

	// Case 3: Debug path but error -> INFO
	handlerError := logger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	reqError := httptest.NewRequest(http.MethodGet, "/debug-path", nil)
	wError := httptest.NewRecorder()
	handlerError.ServeHTTP(wError, reqError)
	if !bytes.Contains(buf.Bytes(), []byte("level=info")) {
		t.Errorf("Expected info level for error even on debug path, got: %s", buf.String())
	}
}
