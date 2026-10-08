# Compile-Time Constants vs. Runtime Data

Nexss Flow enforces a strict architectural boundary between **Infrastructure (Compile-Time)** and **Data (Runtime)**. Mixing these two causes memory leaks, race conditions, and spaghetti code.

By separating them visually and functionally, Flow guarantees zero-cost abstractions and rock-solid stability.

## 1. Compile-Time: The `$` Syntax

Compile-time constants define the *shape* and *infrastructure* of your application. They are evaluated by the **C-style Preprocessor** before the script is compiled into an AST.

Once compiled, these values become hardcoded strings in memory. **They cost zero CPU cycles at runtime.**

**Syntax:** `${namespace.key}`

**When to use them:**
* Modifiers that bind to ports, streams, or queues (e.g., `:route`, `:nats_durable`).
* `@require` URLs or configuration.
* Magic numbers or threshold configurations loaded from a JSON file.

**Loading Constants:**
```nflow
# Load a JSON file and flatten it under the 'cfg' prefix
@const.load "prod.json" as cfg

# Define a manual constant
@const MAX_RETRIES = "3"

@pipeline process :nats_durable="stream=${cfg.stream} subject=data.* durable=worker" :retry=${MAX_RETRIES}
```

## 2. Runtime: The `.` Syntax

Runtime data is evaluated **in-flight** for every single message or request. It represents state, payloads, environment variables, and business logic.

**Syntax:** `.field` or `runtime.env`

**When to use them:**
* Evaluating JSON payloads (`.user_id`, `.amount`).
* Branching logic (`match(.amount > 100)`).
* Reading live secrets or environment variables (`runtime.env`).
* Generating dynamic output structures.

## 3. The Golden Rule

> **Never try to use Runtime data (`.field`) in a Compile-Time modifier (`:route`, `:durable`).**

If you try to write `@pipeline get_user :route="GET /users/" + .id`, the compiler will reject it. The HTTP router must know the exact path structure *before* the server starts listening. Dynamic data belongs inside the pipeline body, not in its architectural bindings.

### Example of the Boundary
```nflow
# BAD: Trying to use a runtime payload to define an infrastructure queue
@pipeline process :nats_durable="stream=" + .stream_name # INVALID

# GOOD: Infrastructure is fixed (Compile-Time), Data is dynamic (Runtime)
@pipeline process :nats_durable="stream=${cfg.streams.inbound} subject=in.* durable=worker"
  match(.amount > ${cfg.limits.max_amount}) {
     true -> { subject: "audit." + .tenant_id, payload: . } -> tnats.publish
  }
@end
```

### 4. `@include` for Shared Configuration

Because `@const` directives are evaluated by the preprocessor, you can extract them into a `constants.nflow` file and include them everywhere. The preprocessor parses top-down, meaning included constants instantly "spill over" into the importing file.

```nflow
@include "constants.nflow"
@require github.com/nexssp/transportnats/nexssflow { url: "${env.nats_url}" }
```
