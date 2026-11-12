package app_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zloymelnik/task-tracker/api/openapi"
	"github.com/zloymelnik/task-tracker/internal/app"
	"github.com/zloymelnik/task-tracker/internal/handler"
	"github.com/zloymelnik/task-tracker/internal/service"
)

func TestDocumentationRoutes(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			h := handler.New(&service.Service{}, log)
			// Documentation must work without a token or a token parser.
			router := app.NewRouter(h, enabled, nil, log)
			for _, path := range []string{"/openapi.json", "/swagger/index.html", "/swagger/swagger-ui.css"} {
				t.Run(path, func(t *testing.T) {
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
					want := http.StatusNotFound
					if enabled {
						want = http.StatusOK
					}
					if rec.Code != want {
						t.Fatalf("status=%d want=%d body=%s", rec.Code, want, rec.Body.String())
					}
					if !enabled {
						return
					}
					switch path {
					case "/openapi.json":
						if got := rec.Header().Get("Content-Type"); got != "application/json" {
							t.Errorf("Content-Type=%q", got)
						}
						if !bytes.Equal(rec.Body.Bytes(), openapi.SwaggerJSON) {
							t.Error("response differs from embedded specification")
						}
						var spec struct {
							Swagger  string                     `json:"swagger"`
							BasePath string                     `json:"basePath"`
							Paths    map[string]json.RawMessage `json:"paths"`
						}
						if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
							t.Fatal(err)
						}
						if spec.Swagger != "2.0" || spec.BasePath != "/api/v1" || spec.Paths["/teams/{team_id}/stats"] == nil {
							t.Errorf("unexpected specification: %+v", spec)
						}
					case "/swagger/index.html":
						if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
							t.Error("UI must return HTML")
						}
						if !strings.Contains(rec.Body.String(), "SwaggerUIBundle") || !strings.Contains(rec.Body.String(), "/openapi.json") {
							t.Error("UI missing initialization or specification URL")
						}
					case "/swagger/swagger-ui.css":
						if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/css") || rec.Body.Len() == 0 {
							t.Error("Swagger CSS missing")
						}
					}
				})
			}
		})
	}
}
