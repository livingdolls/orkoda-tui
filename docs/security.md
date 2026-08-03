# Security Design

## 1. Threat Model

Aset utama:

- Source code dan Git history.
- Repository credential.
- Environment secret.
- Developer identity.
- AI provider key.
- Workspace dan generated patch.
- Approval dan publication integrity.

Ancaman utama:

- Prompt injection dari source code, README, issue, dependency, atau test output.
- Command injection.
- Path traversal dan symlink escape.
- Secret exfiltration melalui model, log, artifact, atau network.
- Malicious dependency script.
- Unauthorized Git push atau pull request.
- Approval terhadap diff yang telah berubah.
- Resource exhaustion dan cost abuse.

## 2. Repository Trust

Trust level:

- `RESTRICTED`: read-only inspection; network dan command disabled.
- `TRUSTED_LOCAL`: tool execution di sandbox dengan policy project.
- `TRUSTED_REMOTE`: remote clone dan provider integration diizinkan.

Trust harus diberikan secara eksplisit dan dapat dicabut.

## 3. Authentication and Authorization

- Local HTTP mode binds to loopback by default and requires a mode-0600 bearer token in `.orkoda/api.token` for every `/api/v1` route. Health liveness remains public so the TUI can detect daemon availability.
- The token may be supplied through `ORKODA_API_TOKEN`; the daemon never logs it.
- Remote mode menggunakan short-lived access token dan refresh rotation.
- Token disimpan di OS keychain bila tersedia.
- Seluruh query harus project-scoped.
- Permission terpisah: view, execute, approve, manage credentials, dan publish.

Cookie browser dan CSRF bukan mekanisme utama OpenTUI.

## 4. Workspace Isolation

- Repository sumber tidak menjadi writable mount.
- Job memperoleh workspace terpisah.
- Canonicalize seluruh path sebelum access.
- Tolak absolute path, `..`, device path, dan symlink yang keluar root.
- Batasi file count, total bytes, dan patch size.
- Workspace write lease mencegah dua Executor menulis bersamaan.

## 5. Sandbox

Minimum controls:

- Non-root user.
- Read-only base filesystem.
- Writable workspace mount saja.
- CPU, memory, PID, disk, dan timeout limit.
- Network disabled by default.
- Capability drop.
- Seccomp/AppArmor profile bila tersedia.
- Process tree termination saat cancel atau timeout.
- Temporary home tanpa host credential.

The default check runner is Docker. Host execution is an explicit development escape hatch and requires both `ORKODA_SANDBOX_MODE=host` and `ORKODA_ALLOW_UNSANDBOXED_CHECKS=true`.

## 6. Command Policy

- Command dipilih dari project command profile bila memungkinkan.
- Executable dan argument divalidasi terpisah.
- Block shell metacharacter untuk command yang tidak membutuhkan shell.
- `sudo`, mount, package manager global, Docker socket, SSH agent, dan host process access diblokir secara default.
- Install dependency membutuhkan policy dan network scope khusus.
- Output dibatasi dan di-redact.

## 7. Prompt Injection Defense

Instruction hierarchy:

1. Platform security policy.
2. Agent role policy.
3. User-approved plan.
4. Repository content dan tool output sebagai untrusted data.

Controls:

- Delimiter dan source labeling.
- Secret redaction sebelum context assembly.
- Tool permission enforcement di runtime, bukan hanya prompt.
- Reviewer menerima diff dan evidence, bukan reasoning rahasia Executor.
- Instruksi dalam repository tidak dapat mengaktifkan tool baru.

## 8. Secret Management

- Provider dan Git credentials disimpan terenkripsi.
- Agent menerima temporary scoped credential hanya ketika tool memerlukan.
- `.env`, key files, cloud credentials, dan known secret patterns dikecualikan dari prompt secara default.
- Secret scan dijalankan sebelum artifact dan publication.
- Log menyimpan command redacted dan tidak menyimpan environment lengkap.

## 9. Git Safety

- Direct push ke protected branch ditolak secara default.
- Force push disabled.
- Publication mengikat approval, execution version, base SHA, dan patch checksum.
- Publication rechecks the current workspace snapshot, uses a local commit marker for idempotent retry, and refuses a changed or stale workspace.
- Perubahan diff setelah approval membatalkan approval.
- Commit signing dapat diaktifkan melalui credential broker.
- Pull request dibuat draft pada MVP.

## 10. Approval Integrity

Approval menyimpan:

- User ID.
- Timestamp.
- Execution ID dan version.
- Review ID.
- Base commit SHA.
- Patch checksum.
- Check result snapshot.
- Override reason bila ada. A failed-check override requires the same explicit human acknowledgement and non-empty reason; publication reuses that persisted acknowledgement.

## 11. Queue and Event Security

- Queue tidak diekspos publik.
- Message memiliki schema version dan message ID.
- Consumer idempotent.
- Sensitive payload direferensikan melalui ID, bukan disalin ke message.
- Event stream memeriksa project authorization.

## 12. Security Checklist Before Release

- Sandbox escape test.
- Path traversal dan symlink test.
- Secret leak regression test.
- Malicious repository prompt test.
- Dependency script test.
- Git publication race test.
- Approval checksum mismatch test.
- Resource exhaustion test.
- Audit log completeness test.
