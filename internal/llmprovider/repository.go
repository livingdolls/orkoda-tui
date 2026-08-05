package llmprovider

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("LLM provider configuration not found")

type Record struct {
	Name         string
	BaseURL      string
	DefaultModel string
	JSONMode     string
	Timeout      time.Duration
	Headers      map[string]string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) (*Repository, error) {
	if db == nil {
		return nil, fmt.Errorf("LLM provider database is required")
	}
	return &Repository{db: db}, nil
}

func (r *Repository) List(ctx context.Context) ([]Record, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("LLM provider repository is unavailable")
	}
	rows, err := r.db.QueryContext(ctx, `
        SELECT name, base_url, default_model, json_mode, timeout_ms, headers_json, created_at, updated_at
        FROM llm_provider_configs
        ORDER BY name
    `)
	if err != nil {
		return nil, fmt.Errorf("list LLM provider configurations: %w", err)
	}
	defer rows.Close()
	items := []Record{}
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read LLM provider configurations: %w", err)
	}
	return items, nil
}

func (r *Repository) Get(ctx context.Context, name string) (Record, error) {
	if r == nil || r.db == nil {
		return Record{}, fmt.Errorf("LLM provider repository is unavailable")
	}
	row := r.db.QueryRowContext(ctx, `
        SELECT name, base_url, default_model, json_mode, timeout_ms, headers_json, created_at, updated_at
        FROM llm_provider_configs
        WHERE name = ?
    `, strings.ToLower(strings.TrimSpace(name)))
	item, err := scanRecord(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	return item, err
}

func (r *Repository) Upsert(ctx context.Context, item Record) (Record, error) {
	if r == nil || r.db == nil {
		return Record{}, fmt.Errorf("LLM provider repository is unavailable")
	}
	headers, err := json.Marshal(item.Headers)
	if err != nil {
		return Record{}, fmt.Errorf("encode LLM provider headers: %w", err)
	}
	now := time.Now().UTC().UnixMilli()
	if _, err := r.db.ExecContext(ctx, `
        INSERT INTO llm_provider_configs(
            name, base_url, default_model, json_mode, timeout_ms, headers_json, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(name) DO UPDATE SET
            base_url = excluded.base_url,
            default_model = excluded.default_model,
            json_mode = excluded.json_mode,
            timeout_ms = excluded.timeout_ms,
            headers_json = excluded.headers_json,
            updated_at = excluded.updated_at
    `, item.Name, item.BaseURL, item.DefaultModel, item.JSONMode, item.Timeout.Milliseconds(), string(headers), now, now); err != nil {
		return Record{}, fmt.Errorf("save LLM provider configuration: %w", err)
	}
	return r.Get(ctx, item.Name)
}

func (r *Repository) Delete(ctx context.Context, name string) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("LLM provider repository is unavailable")
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM llm_provider_configs WHERE name = ?`, strings.ToLower(strings.TrimSpace(name)))
	if err != nil {
		return fmt.Errorf("delete LLM provider configuration: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted LLM provider count: %w", err)
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanRecord(row rowScanner) (Record, error) {
	var item Record
	var timeoutMS, createdAt, updatedAt int64
	var headersJSON string
	if err := row.Scan(
		&item.Name, &item.BaseURL, &item.DefaultModel, &item.JSONMode,
		&timeoutMS, &headersJSON, &createdAt, &updatedAt,
	); err != nil {
		return Record{}, err
	}
	if err := json.Unmarshal([]byte(headersJSON), &item.Headers); err != nil {
		return Record{}, fmt.Errorf("decode LLM provider headers: %w", err)
	}
	item.Timeout = time.Duration(timeoutMS) * time.Millisecond
	item.CreatedAt = time.UnixMilli(createdAt).UTC()
	item.UpdatedAt = time.UnixMilli(updatedAt).UTC()
	return item, nil
}
