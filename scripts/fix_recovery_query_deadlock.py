from pathlib import Path

path = Path("internal/execution/recovery.go")
text = path.read_text()
old = '''\tfor legacyRows.Next() {
\t\tvar workflowID, detailsJSON string
\t\tif err := legacyRows.Scan(&workflowID, &detailsJSON); err != nil {
\t\t\tlegacyRows.Close()
\t\t\treturn 0, fmt.Errorf("scan legacy Executor dispatch transition: %w", err)
\t\t}
\t\tvar details struct {
\t\t\tDispatchJobID string `json:"dispatch_job_id"`
\t\t}
\t\tif err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
\t\t\tlegacyRows.Close()
\t\t\treturn 0, fmt.Errorf("decode legacy Executor dispatch transition: %w", err)
\t\t}
\t\tdispatchID := strings.TrimSpace(details.DispatchJobID)
\t\tif dispatchID == "" {
\t\t\tcontinue
\t\t}
\t\tvar jobType, status, message string
\t\terr := r.db.QueryRowContext(ctx, `
\t\t\tSELECT type, status,
\t\t\t\tCOALESCE(NULLIF(TRIM(last_error), ''), 'Executor dispatch exhausted all retries.')
\t\t\tFROM jobs WHERE id = ?
\t\t`, dispatchID).Scan(&jobType, &status, &message)
\t\tif errors.Is(err, sql.ErrNoRows) {
\t\t\tcontinue
\t\t}
\t\tif err != nil {
\t\t\tlegacyRows.Close()
\t\t\treturn 0, fmt.Errorf("load legacy Executor dispatch %s: %w", dispatchID, err)
\t\t}
\t\tif jobType == "workflow.execute" && status == "DEAD" {
\t\t\tcandidates[workflowID] = deadExecutionDispatch{
\t\t\t\tworkflowID: workflowID, dispatchID: dispatchID, message: message,
\t\t\t}
\t\t}
\t}
\tif err := legacyRows.Close(); err != nil {
\t\treturn 0, fmt.Errorf("close legacy Executor dispatch rows: %w", err)
\t}
\tif err := legacyRows.Err(); err != nil {
\t\treturn 0, fmt.Errorf("iterate legacy Executor dispatch transitions: %w", err)
\t}
'''
new = '''\tlegacyDispatches := make([]deadExecutionDispatch, 0)
\tfor legacyRows.Next() {
\t\tvar workflowID, detailsJSON string
\t\tif err := legacyRows.Scan(&workflowID, &detailsJSON); err != nil {
\t\t\tlegacyRows.Close()
\t\t\treturn 0, fmt.Errorf("scan legacy Executor dispatch transition: %w", err)
\t\t}
\t\tvar details struct {
\t\t\tDispatchJobID string `json:"dispatch_job_id"`
\t\t}
\t\tif err := json.Unmarshal([]byte(detailsJSON), &details); err != nil {
\t\t\tlegacyRows.Close()
\t\t\treturn 0, fmt.Errorf("decode legacy Executor dispatch transition: %w", err)
\t\t}
\t\tdispatchID := strings.TrimSpace(details.DispatchJobID)
\t\tif dispatchID != "" {
\t\t\tlegacyDispatches = append(legacyDispatches, deadExecutionDispatch{
\t\t\t\tworkflowID: workflowID,
\t\t\t\tdispatchID: dispatchID,
\t\t\t})
\t\t}
\t}
\tif err := legacyRows.Err(); err != nil {
\t\tlegacyRows.Close()
\t\treturn 0, fmt.Errorf("iterate legacy Executor dispatch transitions: %w", err)
\t}
\tif err := legacyRows.Close(); err != nil {
\t\treturn 0, fmt.Errorf("close legacy Executor dispatch rows: %w", err)
\t}
\t// database.Open intentionally uses a single SQLite connection. Finish the
\t// transition query before looking up queue jobs to avoid self-deadlock.
\tfor _, legacy := range legacyDispatches {
\t\tvar jobType, status, message string
\t\terr := r.db.QueryRowContext(ctx, `
\t\t\tSELECT type, status,
\t\t\t\tCOALESCE(NULLIF(TRIM(last_error), ''), 'Executor dispatch exhausted all retries.')
\t\t\tFROM jobs WHERE id = ?
\t\t`, legacy.dispatchID).Scan(&jobType, &status, &message)
\t\tif errors.Is(err, sql.ErrNoRows) {
\t\t\tcontinue
\t\t}
\t\tif err != nil {
\t\t\treturn 0, fmt.Errorf("load legacy Executor dispatch %s: %w", legacy.dispatchID, err)
\t\t}
\t\tif jobType == "workflow.execute" && status == "DEAD" {
\t\t\tlegacy.message = message
\t\t\tcandidates[legacy.workflowID] = legacy
\t\t}
\t}
'''
if text.count(old) != 1:
    raise SystemExit(f"expected one legacy recovery block, found {text.count(old)}")
path.write_text(text.replace(old, new, 1))
print("recovery query sequencing fixed")
