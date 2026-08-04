package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var errIdempotencyConflict = errors.New("idempotency key was already used with a different request")

type IdempotencyStore interface {
	Reserve(context.Context, string, string, string, string, time.Duration) (bool, error)
}

type idempotencyResponseStore interface {
	SaveResponse(context.Context, string, int, []byte) error
	LoadResponse(context.Context, string) (int, []byte, bool, error)
}

type SQLIdempotencyStore struct {
	db *sql.DB
}

func NewSQLIdempotencyStore(db *sql.DB) (*SQLIdempotencyStore, error) {
	if db == nil {
		return nil, fmt.Errorf("database is required")
	}
	return &SQLIdempotencyStore{db: db}, nil
}

func (s *SQLIdempotencyStore) Reserve(ctx context.Context, key, method, path, requestHash string, ttl time.Duration) (bool, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 256 {
		return false, fmt.Errorf("idempotency key must contain between 1 and 256 characters")
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	now := time.Now().UTC()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at <= ?`, now.UnixMilli())
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO idempotency_keys (key, method, path, request_hash, status_code, response_json, created_at, expires_at)
		VALUES (?, ?, ?, ?, 0, '', ?, ?)
	`, key, method, path, requestHash, now.UnixMilli(), now.Add(ttl).UnixMilli())
	if err == nil {
		return true, nil
	}
	var existingMethod, existingPath, existingHash string
	if queryErr := s.db.QueryRowContext(ctx, `SELECT method, path, request_hash FROM idempotency_keys WHERE key = ?`, key).Scan(&existingMethod, &existingPath, &existingHash); queryErr != nil {
		return false, fmt.Errorf("read idempotency key: %w", queryErr)
	}
	if existingMethod != method || existingPath != path || existingHash != requestHash {
		return false, errIdempotencyConflict
	}
	return false, nil
}

func (s *SQLIdempotencyStore) SaveResponse(ctx context.Context, key string, status int, response []byte) error {
	_, err := s.db.ExecContext(ctx, `UPDATE idempotency_keys SET status_code = ?, response_json = ? WHERE key = ?`, status, string(response), strings.TrimSpace(key))
	if err != nil {
		return fmt.Errorf("save idempotency response: %w", err)
	}
	return nil
}

func (s *SQLIdempotencyStore) LoadResponse(ctx context.Context, key string) (int, []byte, bool, error) {
	var status int
	var response string
	if err := s.db.QueryRowContext(ctx, `SELECT status_code, response_json FROM idempotency_keys WHERE key = ?`, strings.TrimSpace(key)).Scan(&status, &response); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil, false, nil
		}
		return 0, nil, false, fmt.Errorf("load idempotency response: %w", err)
	}
	if status == 0 || response == "" {
		return 0, nil, false, nil
	}
	return status, []byte(response), true, nil
}

func idempotencyMiddleware(store IdempotencyStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
		if store == nil || key == "" || c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		var source io.Reader = bytes.NewReader(nil)
		if c.Request.Body != nil {
			source = c.Request.Body
		}
		body, err := io.ReadAll(io.LimitReader(source, 4*1024*1024+1))
		if err != nil || len(body) > 4*1024*1024 {
			c.Abort()
			writeError(c, http.StatusBadRequest, "request body is too large")
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		hash := sha256.Sum256(body)
		reserved, err := store.Reserve(c.Request.Context(), key, c.Request.Method, c.Request.URL.RequestURI(), hex.EncodeToString(hash[:]), 24*time.Hour)
		if errors.Is(err, errIdempotencyConflict) {
			c.Abort()
			writeError(c, http.StatusConflict, err.Error())
			return
		}
		if err != nil {
			c.Abort()
			writeError(c, http.StatusInternalServerError, "idempotency reservation failed")
			return
		}
		if !reserved {
			c.Abort()
			if responseStore, ok := store.(idempotencyResponseStore); ok {
				status, response, found, loadErr := responseStore.LoadResponse(c.Request.Context(), key)
				if loadErr != nil {
					writeError(c, http.StatusInternalServerError, "idempotency response lookup failed")
					return
				}
				if found {
					c.Data(status, "application/json; charset=utf-8", response)
					return
				}
			}
			writeError(c, http.StatusConflict, "idempotency key has already been processed")
			return
		}
		if responseStore, ok := store.(idempotencyResponseStore); ok {
			writer := &idempotentResponseWriter{ResponseWriter: c.Writer}
			c.Writer = writer
			c.Next()
			writer.commit()
			c.Writer = writer.ResponseWriter
			if err := responseStore.SaveResponse(c.Request.Context(), key, writer.statusCode(), writer.body.Bytes()); err != nil {
				// The mutation already completed; surface a diagnostic without
				// turning a successful command into a false failure.
				return
			}
			return
		}
		c.Next()
	}
}

type idempotentResponseWriter struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
	wrote  bool
}

func (w *idempotentResponseWriter) WriteHeader(code int) {
	if !w.wrote {
		w.status = code
	}
}

func (w *idempotentResponseWriter) WriteHeaderNow() {
	if !w.wrote {
		if w.status == 0 {
			w.status = http.StatusOK
		}
	}
}

func (w *idempotentResponseWriter) Write(data []byte) (int, error) {
	if !w.wrote {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		w.wrote = true
	}
	return w.body.Write(data)
}

func (w *idempotentResponseWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}

func (w *idempotentResponseWriter) Status() int {
	return w.statusCode()
}

func (w *idempotentResponseWriter) Size() int { return w.body.Len() }

func (w *idempotentResponseWriter) Written() bool { return w.wrote }

func (w *idempotentResponseWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (w *idempotentResponseWriter) commit() {
	if w.status != 0 {
		w.ResponseWriter.WriteHeader(w.status)
	}
	if w.body.Len() > 0 {
		_, _ = w.ResponseWriter.Write(w.body.Bytes())
	}
}
