package handler

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type TokenParser interface {
	ParseToken(string) (uuid.UUID, error)
}

type userIDKey struct{}

func AuthMiddleware(parser TokenParser, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			parts := strings.Fields(authHeader)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				w.Header().Set("WWW-Authenticate", "Bearer")
				if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
					log.Error("Failed to write unauthorized response", "error", err)
				}
				return
			}
			rawToken := parts[1]

			userID, err := parser.ParseToken(rawToken)
			if err != nil {
				w.Header().Set("WWW-Authenticate", "Bearer")
				if err := writeErrorResponse(w, http.StatusUnauthorized, "Unauthorized"); err != nil {
					log.Error("Failed to write unauthorized response", "error", err)
				}
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	id, ok := ctx.Value(userIDKey{}).(uuid.UUID)
	return id, ok
}

func Logger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			requestId := middleware.GetReqID(r.Context())
			w.Header().Set("X-Request-ID", requestId)
			next.ServeHTTP(ww, r)

			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}

			log.Info("request completed",
				"request_id", requestId,
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", r.RemoteAddr,
			)
		})
	}
}

func Recovery(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}

				log.Error("Request panicked",
					"request_id", middleware.GetReqID(r.Context()),
					"panic", recovered,
					"stack", string(debug.Stack()),
				)
				if ww.Status() != 0 {
					// Abort a partial response instead of returning it as complete.
					panic(http.ErrAbortHandler)
				}
				if err := writeErrorResponse(ww, http.StatusInternalServerError, "internal server error"); err != nil {
					log.Error("Failed to write recovery response", "error", err)
				}
			}()
			next.ServeHTTP(ww, r)
		})
	}
}
