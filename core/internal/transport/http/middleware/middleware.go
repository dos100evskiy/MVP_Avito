package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/avito/kuhnya/core/internal/domain"
)

type ctxKey string

const (
	ctxKeyRequestID ctxKey = "request_id"
	ctxKeyVenueID   ctxKey = "venue_id"
)

// RequestID проставляет X-Request-Id в ответ и кладёт его в контекст —
// используется в логах для сквозной трассировки одного запроса.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Logging логирует метод/путь/статус/длительность каждого запроса.
func Logging(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)
			log.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", r.Context().Value(ctxKeyRequestID),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Recover перехватывает панику в хендлере, логирует и отдаёт 500 вместо падения процесса.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered", "error", rec, "path", r.URL.Path)
					http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// VenueAuth — упрощённая аутентификация заведения по статическому API-key
// в заголовке X-API-Key (см. README: "допустимые упрощения для MVP").
// Хеш ключа сверяется с venues.api_key_hash, id заведения кладётся в контекст.
func VenueAuth(venues domain.VenueRepository) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-API-Key")
			if key == "" {
				http.Error(w, `{"error":"missing X-API-Key"}`, http.StatusUnauthorized)
				return
			}
			sum := sha256.Sum256([]byte(key))
			hash := hex.EncodeToString(sum[:])

			venue, err := venues.GetByAPIKeyHash(r.Context(), hash)
			if err != nil || venue == nil || !venue.IsActive {
				http.Error(w, `{"error":"invalid api key"}`, http.StatusUnauthorized)
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyVenueID, venue.ID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func VenueIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(ctxKeyVenueID).(string)
	return v, ok
}
