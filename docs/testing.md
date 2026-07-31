# Testing Strategy

## 1. Tujuan

Memastikan OpenTUI, workflow, repository operations, sandbox, agent contracts, review, approval, dan Git publication bekerja aman dan deterministik.

## 2. Test Pyramid

### Unit Tests

- State transition.
- Plan and review schema validation.
- Path canonicalization.
- Tool policy decision.
- Diff checksum.
- Command argument validation.
- Budget and revision guards.
- OpenTUI reducers, key bindings, and view-model formatting.

### Integration Tests

- PostgreSQL repositories dan migrations.
- RabbitMQ outbox and idempotent consumer.
- Redis event fan-out.
- Git worktree/clone lifecycle.
- Sandbox command execution.
- LLM provider adapters dengan recorded fixtures.
- Git provider adapter sandbox account.

### End-to-End Tests

- Open repository -> plan -> execute -> check -> review -> approve -> commit.
- Request revision -> new execution -> re-review -> approve.
- Failed check -> override approval with reason.
- Cancel running command.
- Push branch dan create draft PR.

## 3. OpenTUI Tests

- Keyboard-only navigation.
- Focus tidak hilang setelah event update.
- Resize terminal.
- Small terminal fallback.
- Large log virtualization/pagination.
- Diff navigation per file dan hunk.
- Confirmation dialog untuk approval, reject, dan publication.
- Reconnect setelah daemon restart.

Gunakan renderer abstraction atau snapshot text representation agar test tidak bergantung pada terminal nyata.

## 4. State Machine Tests

Setiap transition diuji dalam table-driven tests:

- Valid transition berhasil.
- Invalid transition menghasilkan domain error.
- Aggregate version mismatch menghasilkan conflict.
- Duplicate command menghasilkan response yang sama.
- Terminal states tidak dapat kembali aktif.

## 5. Repository and Workspace Tests

- Dirty source repository tidak ikut berubah.
- Base commit SHA konsisten.
- Worktree cleanup aman.
- Symlink ke luar workspace ditolak.
- Submodule policy diterapkan.
- Workspace recovery dari patch berhasil.
- Concurrent write lease ditolak.

## 6. Tool and Sandbox Tests

- Allowed command berhasil.
- Unknown command ditolak.
- Timeout menghentikan seluruh process tree.
- CPU, memory, PID, dan output limit bekerja.
- Network disabled benar-benar tidak dapat keluar.
- Host environment dan credential tidak terlihat.
- Absolute path dan `../` ditolak.
- Malicious package script dibatasi sesuai policy.

## 7. Agent Contract Tests

- Structured output valid.
- Unknown enum ditolak.
- Invalid file path ditolak.
- Reviewer issue line berada dalam file yang relevan.
- Executor tidak dapat memanggil publication tool.
- Reviewer tool policy read-only.
- Repair attempt tidak mengubah semantic requirement.

## 8. Prompt Regression Tests

Fixture repository mencakup:

- Go API.
- TypeScript frontend repository sebagai target code.
- Kotlin project.
- Monorepo.
- Repository dengan malicious instruction di README.
- Repository dengan secret-like fixtures.

Evaluasi:

- Plan coverage.
- Scope discipline.
- Diff correctness.
- Test selection.
- Hallucinated file rate.
- Reviewer precision dan duplicate issue rate.

## 9. Git Publication Tests

- Commit hanya dari approved checksum.
- Diff berubah setelah approval menyebabkan conflict.
- Push duplicate tetap idempotent.
- Protected branch ditolak.
- Force push ditolak.
- Draft PR metadata benar.
- Provider outage dapat di-retry tanpa membuat PR ganda.

## 10. Performance Tests

- Repository index dengan puluhan ribu file.
- Diff besar dengan pagination.
- Log stream panjang tanpa memory growth TUI.
- Banyak concurrent jobs dan workers.
- Event delivery latency.
- Workspace preparation latency.

## 11. Acceptance Test MVP

- Developer dapat menyelesaikan satu feature kecil dari TUI.
- Source repository tetap bersih sampai publication.
- Executor hanya mengubah workspace.
- Semua tool run muncul di timeline.
- Required checks dijalankan.
- Reviewer issue dapat dipilih sebagai revision input.
- Approval mengikat checksum yang benar.
- Local commit berhasil.
- Draft PR hanya dibuat setelah approval.

## 12. CI Quality Gate

- Go format, lint, unit, integration, dan race test.
- TypeScript type check dan lint.
- OpenTUI component tests.
- JSON Schema validation.
- Migration test.
- Dependency and secret scan.
- Sandbox security suite pada runner yang mendukung.
- Build TUI package dan Go binaries.
