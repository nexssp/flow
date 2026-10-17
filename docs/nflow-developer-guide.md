# Nexss Flow (`.nflow`) — Developer Guide & Best Practices Specification

This specification defines the canonical style, architectural boundaries, and idioms for writing readable, maintainable, and verifiable `.nflow` programs.

---

## 1. Architectural Decisions & Syntax Clarifications

### 1.1 Modifier Separation: Spaces vs. Colons
**Rule:** Always separate inline modifiers with a space.
```nflow
# RECOMMENDED (clean, scannable)
fetch :timeout=5s :retry=3 :idempotent

# AVOID (cluttered, hard to read)
fetch:timeout=5s:retry=3:idempotent
```
The Lexer treats `:` as a distinct token (`TokColon`). Spaces between modifiers are token delimiters:
- **Single-line use:** `action :mod1 :mod2=val` is clean and does not wrap.
- **Multi-line use:** For definitions like `@pipeline` with long transport routes or multiple policies, place modifiers on subsequent indented lines.

---

### 1.2 The Field Name Question: `JobID` vs. `job_id`
In Go, field names start with an uppercase letter to be exported (`JobID`). However, in `.nflow` payloads, APIs, and JSON, wire keys are lowercased/snake_cased (`job_id`).

In `flow/extensions/schema/directive.go`:
```go
if jsonName, ok := tags["json"]; ok {
    field.JSONName = strings.Split(jsonName, ",")[0]
}
if field.JSONName == "" {
    field.JSONName = lowerFirst(name) // e.g., JobID -> jobID, Name -> name
}
```

#### The Official Rule for `.nflow` Schemas
1. **Schema Field Names:** Write fields in PascalCase matching Go conventions (`JobID`, `ArtifactBytes`, `CreatedAt`).
2. **`json:"..."` Tag:** Explicitly specify the wire/payload key (`json:"job_id"`). Downstream runtime payloads, projections, and `.` references will use this key.
3. **In the Pipeline:** Access the payload using the `json` tag key: `.job_id`, `.artifact_bytes`.

```nflow
@schema ReserveJobRequest {
  JobID      string `json:"job_id"      validate:"required"`
  TTL        string `json:"ttl"         validate:"required"`
  ReservedAt string `json:"reserved_at" validate:"required"`
}

# The incoming payload uses the JSON name:
nats.kv.create @{
  key: "jobs." + .job_id,
  ttl: .ttl
}
```
*Why this exists:* The struct tags allow seamless interop with Go engines, `thttp`, `tcli`, `tbus`, and `validator/v10` without requiring runtime reflection remapping.

---

### 1.3 `@{ ... }` (Action Args) vs. `{ ... }` (Projections)
Do not use `{ ... }` to feed arguments into an action.

- **`@{ key: value }` — Argument Block (Compile-Time Bound):**
  Supplies parameters directly into the action's execution signature. Supports static type checking via `compiler.ArgSchemas`.
- **`{ key: value }` — Runtime Projection:**
  Evaluated at runtime using `expr-lang` to reshape or transform state.

```nflow
# BAD: Extra map allocation and unvalidated stage
{
  bucket: artifact_buckets.jobs,
  key: "jobs." + .job_id
} -> nats.kv.get

# GOOD: Direct argument binding, zero allocation overhead
nats.kv.get @{
  bucket: artifact_buckets.jobs,
  key: "jobs." + .job_id
}
```

---

## 2. Standard Workflow Layout

Every `.nflow` file should follow this logical section ordering:

```
1. @description
2. @require (external bundles & transports)
3. @symbols (constants & namespaces)
4. @schema (input/output data contracts)
5. @pipeline (business logic fragments)
6. Listeners or Top-level Pipeline Execution
```

---

## 3. Best Practices by Component

### 3.1 Modules & Transports (`@require`)
Always pin versions for remote modules, and assign local namespaces using `as <name>`.

```nflow
# Local extension package
@require ./helpers as helpers {
  prefix: "PROD"
}

# Remote transport bundle
@require github.com/nexssp/transportnats/nexssflow as nats {
  url: "${NATS_URL ?? 'nats://127.0.0.1:4222'}"
}
```

---

### 3.2 Symbol Namespaces (`@symbols`)
Use symbols for constants, queue names, and status codes. Never hardcode the same magic string in multiple places.

```nflow
@symbols buckets {
  jobs:    "ARTIFACT_JOBS"
  objects: "ARTIFACT_OBJECTS"
}

@symbols status {
  pending:  "PENDING"
  reserved: "RESERVED"
  complete: "COMPLETE"
}
```
Reference them directly in arguments or expressions: `buckets.jobs`, `status.reserved`.

---

### 3.3 Schemas (`@schema`)
Every pipeline exposed to an external transport or HTTP route must be guarded by a `@schema`. Use `validate:` tags for assertions and `default:` tags to fill missing values.

```nflow
@schema CompleteJobRequest {
  JobID            string `json:"job_id"            validate:"required"`
  ExpectedRevision uint64 `json:"expected_revision" validate:"required,gt=0"`
  ArtifactName     string `json:"artifact_name"     validate:"required"`
  ArtifactBytes    int64  `json:"artifact_bytes"    validate:"required,gt=0"`
  SourceName       string `json:"source_name"       validate:"required"`
  SHA256           string `json:"sha256"            validate:"required"`
  UpdatedAt        string `json:"updated_at"        validate:"required"`
}
```

---

### 3.4 Reusable Pipelines (`@pipeline`)
- For short configurations, use inline space-separated modifiers:
  `@pipeline fast_ping :timeout=500ms :retry=1`
- For pipelines with schemas and transport triggers, use indented modifiers:

```nflow
@pipeline complete_job
  :schema=CompleteJobRequest
  :nats_request="artifact.jobs.complete"

  nats.kv.update @{
    bucket:   buckets.jobs,
    key:      "jobs." + .job_id,
    revision: .expected_revision,
    value: {
      job_id:         .job_id,
      status:         status.complete,
      artifact_name:  .artifact_name,
      artifact_bytes: .artifact_bytes,
      source_name:    .source_name,
      sha256:         .sha256,
      updated_at:     .updated_at
    }
  }
@end
```

---

### 3.5 Error Handling & Fallbacks
- Use `||` for fallback operations (catch-all).
- Use `? on_error { ... }` for error kind discrimination (`xerr.KindTimeout`, `xerr.KindUnavailable`).

```nflow
# Simple fallback floor
fetch_data :timeout=2s || const @{ value: "fallback_value" }

# Error kind branching
fetch_user ? on_error {
  xerr.KindNotFound -> { status: "missing", id: .input.id },
  xerr.KindTimeout  -> { status: "deferred", retry: true },
  else              -> error.reraise
}
```

---

### 3.6 Parallelism & Concurrency
- **Anonymous Gather:** `( branchA & branchB )` produces output keyed as `action#1`, `action#2`.
- **Named Gather (Recommended):** Use `parallel { ... }` for explicit, stable map keys:

```nflow
parallel {
  profile: user.fetch_profile,
  orders:  user.fetch_orders,
  metrics: analytics.fetch_metrics
}
```

---

## 4. Complete Reference Program

The artifact workflow with JetStream KV and ObjectStore, formatted according to the guidelines:

```nflow
@description "Cross-process report artifact workflow with JetStream KV and ObjectStore"

# ── 1. Transports & Dependencies ──────────────────────────────────────────────
@require github.com/nexssp/transportnats/nexssflow as nats {
  url: "${NATS_URL ?? 'nats://127.0.0.1:4222'}"
}

# ── 2. Constant Symbols ───────────────────────────────────────────────────────
@symbols buckets {
  jobs:    "ARTIFACT_JOBS"
  objects: "ARTIFACT_OBJECTS"
}

@symbols status {
  reserved: "reserved"
  complete: "complete"
}

# ── 3. Schemas ────────────────────────────────────────────────────────────────
@schema ReserveJobRequest {
  JobID      string `json:"job_id"      validate:"required"`
  TTL        string `json:"ttl"         validate:"required"`
  ReservedAt string `json:"reserved_at" validate:"required"`
}

@schema StoreArtifactRequest {
  JobID          string `json:"job_id"          validate:"required"`
  ArtifactBase64 string `json:"artifact_base64" validate:"required"`
}

@schema CompleteJobRequest {
  JobID            string `json:"job_id"            validate:"required"`
  ExpectedRevision uint64 `json:"expected_revision" validate:"required,gt=0"`
  ArtifactName     string `json:"artifact_name"     validate:"required"`
  ArtifactBytes    int64  `json:"artifact_bytes"    validate:"required,gt=0"`
  SourceName       string `json:"source_name"       validate:"required"`
  SHA256           string `json:"sha256"            validate:"required"`
  UpdatedAt        string `json:"updated_at"        validate:"required"`
}

@schema GetJobRequest {
  JobID string `json:"job_id" validate:"required"`
}

@schema GetArtifactRequest {
  Name string `json:"name" validate:"required"`
}

# ── 4. Pipelines ──────────────────────────────────────────────────────────────

# 1. Reserve job with expiring KV lease
@pipeline reserve_job
  :schema=ReserveJobRequest
  :nats_request="artifact.jobs.reserve"

  nats.kv.create @{
    bucket: buckets.jobs,
    key:    "jobs." + .job_id,
    ttl:    .ttl,
    value: {
      job_id:      .job_id,
      status:      status.reserved,
      reserved_at: .reserved_at
    }
  }
@end

# 2. Upload binary payload to ObjectStore (decoded from base64)
@pipeline store_artifact
  :schema=StoreArtifactRequest
  :nats_request="artifact.object.store"

  nats.object.put @{
    bucket:    buckets.objects,
    name:      "jobs/" + .job_id + "/artifact.bin",
    data:      .artifact_base64,
    encoding:  "base64",
    max_bytes: 1048576
  }
@end

# 3. Revision-checked update (optimistic concurrency control)
@pipeline complete_job
  :schema=CompleteJobRequest
  :nats_request="artifact.jobs.complete"

  nats.kv.update @{
    bucket:   buckets.jobs,
    key:      "jobs." + .job_id,
    revision: .expected_revision,
    value: {
      job_id:         .job_id,
      status:         status.complete,
      artifact_name:  .artifact_name,
      artifact_bytes: .artifact_bytes,
      source_name:    .source_name,
      sha256:         .sha256,
      updated_at:     .updated_at
    }
  }
@end

# 4. Fetch job state
@pipeline get_job
  :schema=GetJobRequest
  :nats_request="artifact.jobs.get"

  nats.kv.get @{
    bucket: buckets.jobs,
    key:    "jobs." + .job_id
  }
@end

# 5. Fetch raw object
@pipeline get_artifact
  :schema=GetArtifactRequest
  :nats_request="artifact.object.get"

  nats.object.get @{
    bucket:    buckets.objects,
    name:      .name,
    max_bytes: 1048576
  }
@end

# ── 5. Transport Daemon ───────────────────────────────────────────────────────
nats.listen
```

---

## 5. Summary Cheat Sheet for Code Reviews

| Check | Correct | Reject |
| :--- | :--- | :--- |
| **Modifier Spacing** | `action :timeout=5s :retry=3` | `action:timeout=5s:retry=3` |
| **Action Args** | `action @{ key: .field }` | `{ key: .field } -> action` |
| **Pipeline Modifiers** | Place on indented lines under `@pipeline` | Crammed into one long header line |
| **Field References** | Use the `json:"..."` tag name (`.job_id`) | Using struct field name (`.JobID`) |
| **Error Handling** | Catch typed `xerr.Kind` via `? on_error` | Raw string matches or silent drops |
| **Magic Strings** | Declare in `@symbols` namespace | Hardcoded strings scattered across actions |