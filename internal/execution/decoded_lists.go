package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// ListToolRunsDecoded reads SQLite TEXT JSON through strings before converting
// it to json.RawMessage. modernc SQLite returns TEXT as string, not []byte.
func (r *Repository) ListToolRunsDecoded(ctx context.Context, executionID string) ([]ToolRun, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+toolRunColumns+` FROM tool_runs WHERE execution_id=? ORDER BY sequence`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ToolRun, 0)
	for rows.Next() {
		var item ToolRun
		var inputSummary, outputSummary string
		var code, message sql.NullString
		var started, completed sql.NullInt64
		var created, updated int64
		if err := rows.Scan(
			&item.ID, &item.ExecutionID, &item.Sequence, &item.Tool, &item.Status,
			&inputSummary, &outputSummary, &code, &message,
			&started, &completed, &created, &updated,
		); err != nil {
			return nil, err
		}
		item.InputSummaryJSON = json.RawMessage(inputSummary)
		item.OutputSummaryJSON = json.RawMessage(outputSummary)
		item.ErrorCode = code.String
		item.ErrorMessage = message.String
		item.CreatedAt = time.UnixMilli(created).UTC()
		item.UpdatedAt = time.UnixMilli(updated).UTC()
		if started.Valid {
			value := time.UnixMilli(started.Int64).UTC()
			item.StartedAt = &value
		}
		if completed.Valid {
			value := time.UnixMilli(completed.Int64).UTC()
			item.CompletedAt = &value
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ListCheckpointsDecoded(ctx context.Context, executionID string) ([]Checkpoint, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,execution_id,sequence,base_commit_sha,workspace_head_sha,patch_checksum,patch_bytes,changed_files_json,patch_text,created_at FROM patch_checkpoints WHERE execution_id=? ORDER BY sequence`, executionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Checkpoint, 0)
	for rows.Next() {
		var item Checkpoint
		var changedFiles string
		var created int64
		if err := rows.Scan(
			&item.ID, &item.ExecutionID, &item.Sequence, &item.BaseCommitSHA,
			&item.WorkspaceHeadSHA, &item.PatchChecksum, &item.PatchBytes,
			&changedFiles, &item.PatchText, &created,
		); err != nil {
			return nil, err
		}
		item.ChangedFilesJSON = json.RawMessage(changedFiles)
		item.CreatedAt = time.UnixMilli(created).UTC()
		items = append(items, item)
	}
	return items, rows.Err()
}
