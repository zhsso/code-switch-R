package main

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStaticWebUIServesIndexAndSPAFallback(t *testing.T) {
	assets := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>webui</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('ok')")},
	}
	handler := staticWebUI(fs.FS(assets))

	for _, target := range []string{"/", "/settings", "/nested/client/route"} {
		t.Run(target, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("unexpected status %d for %s", recorder.Code, target)
			}
			if body := recorder.Body.String(); !strings.Contains(body, "webui") {
				t.Fatalf("unexpected body for %s: %q", target, body)
			}
			if cache := recorder.Header().Get("Cache-Control"); cache != "no-cache" {
				t.Fatalf("unexpected index cache policy %q", cache)
			}
		})
	}
}

func TestStaticWebUIServesImmutableAssets(t *testing.T) {
	handler := staticWebUI(fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("index")},
		"assets/app.js": &fstest.MapFile{Data: []byte("asset")},
	})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if recorder.Code != http.StatusOK || recorder.Body.String() != "asset" {
		t.Fatalf("unexpected asset response: %d %q", recorder.Code, recorder.Body.String())
	}
	if cache := recorder.Header().Get("Cache-Control"); cache != "public, max-age=31536000, immutable" {
		t.Fatalf("unexpected asset cache policy %q", cache)
	}
}

func TestStaticWebUIWithoutBuildReturnsServiceUnavailable(t *testing.T) {
	recorder := httptest.NewRecorder()
	staticWebUI(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unexpected status %d", recorder.Code)
	}
}
