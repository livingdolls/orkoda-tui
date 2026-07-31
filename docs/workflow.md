# Workflow and State Machine

## 1. Main Workflow

```text
DRAFT
  ↓
PLAN_GENERATING
  ↓
PLAN_REVIEW
  ↓
READY
  ↓
WORKSPACE_PREPARING
  ↓
EXECUTING
  ↓
CHECKING
  ↓
REVIEWING
  ↓
WAITING_FOR_APPROVAL
  ├── APPROVED -> PUBLISHING -> COMPLETED
  ├── REVISION_REQUIRED -> EXECUTING
  ├── REJECTED
  └── CANCELLED
```

## 2. Job Status

- `DRAFT`: requirement masih dapat diubah bebas.
- `PLAN_GENERATING`: Planning Agent sedang bekerja.
- `PLAN_REVIEW`: user memeriksa structured plan.
- `READY`: plan telah disetujui untuk execution.
- `WORKSPACE_PREPARING`: isolated workspace sedang dibuat.
- `QUEUED`: command menunggu worker.
- `EXECUTING`: Executor mengubah repository.
- `CHECKING`: required checks sedang dijalankan.
- `REVIEWING`: Reviewer memeriksa diff dan evidence.
- `WAITING_FOR_APPROVAL`: menunggu keputusan developer.
- `REVISION_REQUIRED`: feedback siap dikirim ke Executor.
- `APPROVED`: execution version telah disetujui.
- `PUBLISHING`: commit/push/PR sedang dibuat.
- `COMPLETED`: publication atau local commit selesai.
- `FAILED`: tahap gagal dan membutuhkan retry atau intervensi.
- `REJECTED`: hasil ditolak.
- `CANCELLED`: job dihentikan.

## 3. Valid Transitions

| From | Action | To |
|---|---|---|
| `DRAFT` | generate plan | `PLAN_GENERATING` |
| `PLAN_GENERATING` | plan generated | `PLAN_REVIEW` |
| `PLAN_REVIEW` | accept plan | `READY` |
| `READY` | start | `WORKSPACE_PREPARING` |
| `WORKSPACE_PREPARING` | workspace ready | `QUEUED` |
| `QUEUED` | worker claimed | `EXECUTING` |
| `EXECUTING` | execution completed | `CHECKING` |
| `CHECKING` | checks completed | `REVIEWING` |
| `REVIEWING` | review completed | `WAITING_FOR_APPROVAL` |
| `WAITING_FOR_APPROVAL` | approve | `APPROVED` |
| `WAITING_FOR_APPROVAL` | request revision | `REVISION_REQUIRED` |
| `REVISION_REQUIRED` | queue revision | `EXECUTING` |
| `APPROVED` | publish | `PUBLISHING` |
| `PUBLISHING` | publication completed | `COMPLETED` |
| active status | cancel | `CANCELLED` |
| retryable status | failure | `FAILED` |

## 4. Planning Lifecycle

Structured plan minimal:

```json
{
  "goal": "Add employee import endpoint",
  "requirements": [],
  "constraints": [],
  "affected_areas": [],
  "implementation_steps": [],
  "test_strategy": [],
  "acceptance_criteria": [],
  "risks": [],
  "open_questions": []
}
```

User harus menyelesaikan open question yang blocking sebelum plan dapat menjadi `READY`.

## 5. Workspace Lifecycle

```text
REQUESTED -> PREPARING -> READY -> WRITE_LOCKED -> ARCHIVED -> DELETED
                         └────────> FAILED
```

Workspace menyimpan:

- Repository ID.
- Base branch dan base commit SHA.
- Workspace path atau storage reference.
- Current execution version.
- Dirty state.
- Patch checksum.
- Lease owner dan expiry.

## 6. Execution Lifecycle

1. Worker memperoleh write lease.
2. Context builder memilih file relevan.
3. Executor menjalankan model/tool loop.
4. Setiap tool run dicatat sebelum dan sesudah eksekusi.
5. Patch checkpoint dibuat setelah perubahan penting.
6. Executor mengembalikan summary, changed files, assumptions, dan unresolved items.
7. Workspace write lease dilepas sebelum review.

Execution status:

- `PENDING`.
- `RUNNING`.
- `SUCCEEDED`.
- `FAILED`.
- `TIMED_OUT`.
- `CANCELLED`.

## 7. Automated Checks

Check dapat berstatus:

- `PENDING`.
- `RUNNING`.
- `PASSED`.
- `FAILED`.
- `SKIPPED`.
- `TIMED_OUT`.

Required check yang gagal tidak menghentikan review, karena Reviewer membutuhkan evidence kegagalan. Namun approval diblokir kecuali user melakukan override eksplisit.

## 8. Review Lifecycle

Reviewer menerima snapshot immutable:

- Requirement dan accepted plan.
- Base commit SHA.
- Final diff dan changed files.
- Executor summary dan tool run summary.
- Check results.
- Previous review dan revision feedback bila ada.

Decision:

- `PASS`.
- `PASS_WITH_NOTES`.
- `REVISION_REQUIRED`.
- `REJECTED`.

Severity:

- `CRITICAL`.
- `HIGH`.
- `MEDIUM`.
- `LOW`.
- `INFO`.

## 9. Human Decision

### Approve

Syarat default:

- Execution dan review merupakan versi terbaru.
- Tidak ada unresolved `CRITICAL` issue.
- Required checks lulus.
- Diff checksum masih sama.
- Base commit belum berubah secara incompatible.

### Approve With Override

User harus memberikan alasan bila:

- Required check gagal.
- Ada blocking issue yang diterima sebagai risiko.
- Reviewer decision bukan `PASS`.

Override disimpan sebagai security audit event.

### Request Revision

Input:

- Free-form feedback.
- Selected reviewer issues.
- Acceptance criteria yang harus diperbaiki.
- File atau area yang tidak boleh diubah.

### Reject

Job menjadi terminal state dan tidak dapat dipublish. User dapat membuat job baru dari plan atau execution sebelumnya.

## 10. Revision Context

Revision input berisi hanya context yang relevan:

- Accepted plan.
- Previous diff.
- Previous execution summary.
- Selected issues.
- User feedback.
- Failed checks.
- Workspace current state.

Versi lama tidak diubah.

## 11. Retry Policy

Retry otomatis hanya untuk error transient:

- Provider rate limit.
- Network timeout.
- Worker interruption sebelum side effect selesai.
- Temporary Git provider failure.

Tidak auto-retry untuk:

- Invalid plan.
- Sandbox policy violation.
- Deterministic build failure.
- Authentication failure.
- Repository conflict yang membutuhkan keputusan user.

## 12. Cancellation

Cancellation bersifat cooperative:

- Tandai cancellation requested.
- Batalkan model request.
- Hentikan process tree sandbox.
- Simpan partial patch dan log.
- Lepaskan workspace lease.

## 13. Publication

Publication memverifikasi:

- Approval ID.
- Execution version.
- Diff checksum.
- Base and head SHA.
- Target branch policy.

Urutan:

```text
Create commit -> verify tree -> push branch -> create/update draft PR -> COMPLETED
```
