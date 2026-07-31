# Product Specification

## 1. Nama Sementara

**AI Software Development Agent TUI**

## 2. Ringkasan

Aplikasi terminal-native yang membantu developer mengerjakan perubahan source code melalui beberapa agent dengan tanggung jawab terpisah.

```text
Requirement -> Plan -> Workspace -> Execute -> Check -> Review -> Human Decision -> Commit/PR
                                               ^                    |
                                               |------ Revision ----|
```

OpenTUI menjadi interface utama untuk memilih repository, menulis requirement, memantau proses, membaca diff dan review, serta memberikan approval.

## 3. Masalah yang Diselesaikan

- Coding agent biasa mencampur planning, implementasi, review, dan keputusan dalam satu percakapan.
- Perubahan file dan command tidak selalu memiliki audit trail.
- Agent dapat menganggap pekerjaannya benar tanpa pemeriksaan independen.
- Test result, diff, dan acceptance criteria sering tidak dihubungkan secara formal.
- Revisi sulit dibandingkan dengan versi sebelumnya.
- Agent berisiko menjalankan command berbahaya atau membaca secret repository.
- Hasil dapat dipublish sebelum developer benar-benar memeriksanya.

## 4. Target Pengguna

### Individu

- Backend developer.
- Frontend developer.
- Mobile developer.
- Full-stack developer.
- DevOps atau platform engineer.
- Maintainer open-source.

### Tim

- Software engineering team.
- QA automation team.
- Platform engineering team.
- Security engineering team.

## 5. Use Case

- Implementasi feature dari issue atau requirement.
- Bug fixing dengan reproduksi dan regression test.
- Refactoring tanpa mengubah behavior.
- Menambah unit, integration, atau end-to-end test.
- Memperbaiki lint, type, build, atau dependency issue.
- Membuat database migration dan perubahan API.
- Meninjau perubahan source code dan risiko keamanan.
- Membuat dokumentasi teknis yang menjadi bagian repository.
- Menghasilkan commit atau draft pull request setelah approval.

## 6. Peran Sistem

### Developer

- Memilih repository dan base branch.
- Memberikan requirement, constraint, dan acceptance criteria.
- Mengubah implementation plan.
- Mengatur tool policy dan budget.
- Mengawasi progress dan command.
- Memeriksa diff, check result, dan review.
- Approve, request revision, reject, atau mengambil alih secara manual.

### Planning Agent

- Membaca requirement dan repository context yang relevan.
- Mengidentifikasi file atau modul yang kemungkinan terdampak.
- Membuat implementation steps, test strategy, risiko, dan acceptance criteria.
- Tidak mengubah file.

### Executor Agent

- Bekerja hanya di workspace yang ditentukan.
- Membaca dan mengubah source code.
- Menjalankan command yang diizinkan.
- Menambah atau memperbaiki test.
- Menghasilkan diff, execution summary, dan evidence.
- Tidak menyetujui hasil sendiri dan tidak publish tanpa approval.

### Reviewer Agent

- Melakukan review independen terhadap plan, diff, dan check result.
- Memeriksa correctness, security, maintainability, test, dan scope.
- Memberikan issue dengan severity dan lokasi file.
- Tidak mengubah workspace pada MVP.

### Orchestrator

- Mengatur state machine.
- Menyiapkan workspace.
- Menjalankan agent dan tool secara aman.
- Menyimpan event, output, usage, dan audit.
- Menangani retry, cancellation, timeout, dan budget.

## 7. Functional Requirements

### FR-001 Terminal Interface

- Seluruh workflow utama dapat dilakukan melalui OpenTUI.
- TUI mendukung keyboard navigation, command palette, progress, log viewer, diff viewer, review panel, dan approval dialog.
- TUI dapat terhubung ke local daemon atau remote API.

### FR-002 Repository Management

- User dapat membuka repository lokal atau menghubungkan repository remote.
- Sistem membaca remote, branch, commit SHA, bahasa, dan build configuration.
- User memilih base branch dan target publication.
- Repository yang tidak dipercaya harus dibuka dalam restricted mode.

### FR-003 Planning

- User dapat menulis requirement dalam Markdown.
- Plan menyimpan goal, requirements, constraints, affected areas, implementation steps, test strategy, dan acceptance criteria.
- Planning Agent dapat menghasilkan structured plan.
- User dapat mengedit dan menyetujui plan sebelum execution.

### FR-004 Workspace

- Setiap job memiliki isolated Git worktree atau clone.
- Workspace memiliki base commit yang immutable.
- Perubahan antar-revisi disimpan sebagai execution version.
- Cleanup tidak boleh menghapus workspace yang masih dibutuhkan untuk audit atau recovery.

### FR-005 Agent Configuration

- User dapat memilih provider, model, instruction, token limit, iteration limit, timeout, dan tool policy.
- Executor dan Reviewer menggunakan konfigurasi terpisah.
- Reviewer secara default read-only.

### FR-006 Code Execution

- Executor dapat menggunakan file read, file search, file patch, Git diff, dan command runner.
- Semua write dibatasi pada workspace.
- Command memiliki timeout, output limit, environment policy, network policy, dan audit record.
- Agent tidak boleh mengakses credential host secara langsung.

### FR-007 Automated Checks

- Project dapat mendefinisikan formatter, linter, type check, unit test, integration test, dan build command.
- Hasil check menyimpan exit code, duration, output ringkas, dan artifact log.
- Required check yang gagal mencegah approval kecuali user melakukan explicit override dengan alasan.

### FR-008 Review

- Reviewer menerima requirement, plan, base commit, diff, changed files, tool summary, dan check result.
- Review memiliki decision, score, issue list, passed criteria, failed criteria, dan residual risks.
- Issue memiliki severity, category, file, line, evidence, dan recommendation.

### FR-009 Human Approval

- User dapat approve, approve with notes, request revision, reject, atau take over.
- Hanya execution version terbaru yang dapat disetujui.
- Approval mencatat actor, timestamp, review, base commit, diff checksum, dan override reason.

### FR-010 Revision

- Reviewer issue dan user feedback dapat dipilih sebagai revision input.
- Setiap revisi menghasilkan execution version baru dalam workspace yang sama atau workspace turunan.
- Revision loop dibatasi oleh count, token, cost, dan wall-clock budget.

### FR-011 Git Publication

- Setelah approval, sistem dapat membuat commit lokal.
- Integrasi remote dapat push branch dan membuat draft pull request.
- Publication harus memverifikasi base commit, current diff checksum, dan approval version.
- Force push dan direct push ke protected branch dinonaktifkan secara default.

### FR-012 Audit and Usage

- Semua state transition, command, file change summary, review, approval, dan publication dicatat.
- Usage menyimpan token, request count, model, duration, dan estimated cost.

## 8. Non-Functional Requirements

### Reliability

- Worker restart tidak menghilangkan job.
- Command dan event diproses idempotent.
- Workspace dapat direkonstruksi dari base commit dan patch artifact.

### Security

- Tool execution berjalan dalam sandbox.
- Path traversal, symlink escape, secret exposure, dan unrestricted network diblokir.
- Repository content dianggap untrusted input.
- Publish action selalu membutuhkan approval manusia.

### Performance

- Navigasi TUI tetap responsif saat job berjalan.
- Log besar dipaging atau di-stream tanpa menyimpan seluruh output di memory TUI.
- File indexing dilakukan incremental dan dapat dibatalkan.

### Portability

- Local mode mendukung Linux dan macOS terlebih dahulu.
- Windows dapat ditambahkan setelah workspace dan sandbox abstraction stabil.

### Maintainability

- Core domain tidak bergantung pada OpenTUI atau provider AI.
- Tool, provider, Git host, sandbox, dan storage menggunakan adapter interface.

## 9. MVP Scope

### Included

- OpenTUI terminal application.
- Local daemon atau API service.
- Repository lokal dan Git remote dasar.
- Planning Agent, Executor, dan Reviewer.
- Isolated workspace.
- File read/search/patch tools.
- Sandboxed command runner.
- Configurable checks.
- Diff viewer.
- Structured code review.
- Human approval dan revision loop.
- Local commit.
- Draft pull request untuk satu Git provider.
- Usage dan activity timeline.

### Excluded

- Autonomous production deployment.
- Direct merge tanpa approval.
- Multi-agent voting.
- Visual node-based workflow builder.
- IDE extension.
- Enterprise SSO dan billing.
- Automatic access ke cloud infrastructure user.

## 10. Acceptance Criteria MVP

- Developer dapat membuka repository dari OpenTUI.
- Sistem mencatat base branch dan commit SHA.
- Requirement dapat diubah menjadi structured implementation plan.
- Executor dapat mengubah file hanya dalam isolated workspace.
- Semua command tercatat dengan exit code dan duration.
- Required checks dijalankan sebelum review.
- Reviewer menghasilkan output schema yang valid dan menunjuk file atau evidence saat relevan.
- User dapat melihat diff per file dan hasil check dari TUI.
- Request revision menghasilkan execution version baru.
- Approval mengikat execution version dan diff checksum.
- Sistem dapat membuat commit lokal dari versi approved.
- Draft pull request tidak dapat dibuat dari versi yang belum approved.
- Duplicate message tidak menghasilkan execution atau publication ganda.
- Secret repository tidak muncul di prompt, log, atau artifact biasa.

## 11. Success Metrics

- Approval rate per job.
- Median revision count sebelum approval.
- Persentase job dengan seluruh required checks lulus.
- Persentase reviewer issue yang diperbaiki pada revisi berikutnya.
- Waktu dari requirement hingga approved diff.
- Rollback atau revert rate pada hasil agent.
- Command failure rate dan sandbox violation rate.
