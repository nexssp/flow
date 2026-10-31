# Nexss Flow practical examples

## 1. Literal values and core actions

### Return a scalar

```exs
const @{ value: "ready" }
```

### Return an object

```exs
const @{ order_id: "ORD-1042", status: "queued" }
```

### Return an array

```exs
const @{ value: ["orders.read", "orders.write"] }
```

### Pass the current value through

```exs
noop
```

### Merge fields into the current object

```exs
with @{ status: "approved", reviewed_by: "Julia" }
```

### Select a field

```exs
pick @{ field: "customer.email" }
```

### Wrap the current value

```exs
wrap @{ key: "payload" }
```

### Stop with an error

```exs
fail @{ kind: "Validation", message: "order total is required" }
```

### Wait before polling again

```exs
sleep @{ duration_ms: 250 }
```

### Read an environment value

```exs
const @{ value: "PAYMENTS_REGION" } -> env
```

### Generate an identifier

```exs
uuid
```

### Clean a JSON response

```exs
const @{ value: .model_response } -> json
```

### Print a diagnostic value

```exs
debug
```

### Flatten a nested result

```exs
flatten
```

## 2. Arguments and modifiers

### Send structured input

```exs
orders.get @{ order_id: .order_id, include_items: true }
```

### Pass the whole input

```exs
orders.update @{ payload: . }
```

### Pass a nested input path

```exs
orders.get @{ order_id: .customer.latest_order_id }
```

### Use an array and nested object

```exs
orders.create @{ customer: { id: .customer_id }, items: [.item_id], priority: "high" }
```

### Combine modifiers and arguments

```exs
payments.charge :timeout=5s :retry=2 @{ amount: .amount, currency: "EUR" }
```

### Compact modifier spelling

```exs
payments.charge:timeout=5s:retry=2 @{ amount: .amount }
```

### Attach metadata

```exs
orders.create :name="create-order" :tag="orders" :status=201 @{ customer_id: .customer_id }
```

### Restrict a feature

```exs
payments.refund :feature="refunds" :role="finance" :permission="payments.refund" @{ payment_id: .payment_id }
```

### Enable strict request validation

```exs
orders.create :strict @{ customer_id: .customer_id, items: .items }
```

### Disable inherited strict validation

```exs
orders.create :lenient @{ customer_id: .customer_id }
```

### Use a read-only action

```exs
customers.lookup :read_only @{ customer_id: .customer_id }
```

## 3. Data flow and operators

### Pipe with `->`

```exs
orders.get @{ order_id: .order_id }
-> { id: .id, total: .total, status: .status }
```

### Pipe with `|`

```exs
orders.get @{ order_id: .order_id }
| pick @{ field: "customer.email" }
```

### Recover with `||`

```exs
payments.primary @{ payment_id: .payment_id }
|| payments.backup @{ payment_id: .payment_id }
```

### Run concurrent branches with `&`

```exs
(
  customers.get @{ id: .customer_id }
  & orders.list @{ customer_id: .customer_id }
)
```

### Conditional result

```exs
.amount > 1000 ? approvals.required : approvals.auto
```

### Conditional action without an else branch

```exs
.status == "ready" ? notifications.send @{ channel: "email", to: .email }
```

### Use a parenthesized expression as an action input

```exs
(
  { customer_id: .customer_id, total: .total }
) -> orders.validate
```

## 4. Projections and expressions

### Rename fields

```exs
{ order_id: .id, customer_name: .customer.name, total: .amount }
```

### Copy fields with shorthand

```exs
{ id, email, status }
```

### Spread and replace

```exs
{ ..., status: "normalized", source: "api" }
```

### Calculate a field

```exs
{ subtotal: .price * .quantity, currency: "EUR" }
```

### Bind a local value

```exs
{
  let total = .price * .quantity;
  ..., total: total, tax: total * 0.2
}
```

### Filter and sort a collection

```exs
{ high_value: .orders | filter(#.total > 1000) | sortBy(#.total, "desc") }
```

### Build a nested response

```exs
{
  customer: { id: .customer_id, name: .customer_name },
  payment: { amount: .amount, currency: .currency },
  audit: { request_id: .request_id }
}
```

### Project input into a transport request

```exs
{
  method: "POST",
  path: "/orders",
  body: { customer_id: .customer_id, items: .items }
} -> http.request
```

## 5. Stable parallel and race control

### Label parallel results

```exs
parallel {
  customer: customers.get @{ id: .customer_id },
  balance: billing.balance @{ customer_id: .customer_id },
}
```

### Use direct literal parallel values

```exs
parallel {
  region: "eu-west-1",
  mode: "read-only",
}
```

### Use actions inside labeled branches

```exs
parallel {
  profile: const :tag="profile" @{ value: "profile-ready" },
  metrics: metrics.collect :timeout=2s @{ service: "orders" },
}
```

### Race equivalent providers

```exs
race {
  catalog.primary @{ sku: .sku },
  catalog.replica @{ sku: .sku },
}
```

### Race regional endpoints

```exs
race {
  transport.eu @{ path: "/health" },
  transport.us @{ path: "/health" },
}
```

### Combine parallel results with a projection

```exs
parallel {
  customer: customers.get @{ id: .customer_id },
  orders: orders.list @{ customer_id: .customer_id },
}
-> { customer: .customer, order_count: len(.orders) }
```

### Nested control values in a projection

```exs
{
  customer: customers.get @{ id: .customer_id },
  payment: race {
    payments.primary @{ customer_id: .customer_id },
    payments.replica @{ customer_id: .customer_id },
  }
}
```

## 6. Match and bounded loops

### Match a request condition

```exs
match {
  .risk == "high" && .amount > 1000 -> approvals.required,
  .risk == "medium" -> approvals.review,
  _ -> approvals.auto,
}
```

### Match a subject value

```exs
match(.score) {
  > 90 -> const @{ value: "gold" },
  >= 60 -> const @{ value: "silver" },
  _ -> const @{ value: "bronze" },
}
```

### Route by status

```exs
match(.status) {
  "paid" -> fulfillment.start,
  "cancelled" -> refunds.start,
  _ -> support.review,
}
```

### Poll until a job completes

```exs
jobs.start @{ type: "report" }
-> loop(jobs.status @{ job_id: .job_id }) until(.status == "completed")
```

### Increment a bounded retry state

```exs
{ attempt: 0, status: "pending" }
-> loop({ ..., attempt: .attempt + 1, status: jobs.status }) until(.status == "complete")
```

## 7. Error handling and assertions

### Fallback to a backup service

```exs
inventory.primary @{ sku: .sku }
|| inventory.backup @{ sku: .sku }
```

### Handle one action error

```exs
payments.charge @{ payment_id: .payment_id }
? on_error payments.recover @{ payment_id: .payment_id }
```

### Route typed errors

```exs
payments.charge @{ payment_id: .payment_id }
? on_error {
  when .kind == "Unavailable" -> payments.retry,
  when .kind == "Forbidden" -> support.escalate,
  else -> error.reraise,
}
```

### Return error details

```exs
payments.charge
? on_error {
  when .kind == "Validation" -> error.info,
  else -> error.reraise,
}
```

### Assert during execution

```exs
orders.get @{ order_id: .order_id }
-> assert(.status != "deleted", "deleted order")
```

### Assert the final result

```exs
orders.get @{ order_id: .order_id }
@assert: result.id != ""
```

### Assert multiple final conditions

```exs
orders.get @{ order_id: .order_id }
@assert {
  result.id != ""
  result.status != "deleted"
  duration_ms < 2000
}
```

### Use a compact assertion list

```exs
orders.get @{ order_id: .order_id }
@assert: { result.id != "", result.status != "deleted" }
```

### Test a flow result

```exs
const @{ status: "ready" }
@test: result.status == "ready"
```

## 8. Reusable pipelines

### Declare a pipeline

```exs
@pipeline get_customer
  customers.get @{ id: .customer_id }
@end

pipeline.get_customer
```

### Call a named pipeline

```exs
@pipeline get_customer
  customers.get @{ id: .customer_id }
@end

pipeline.get_customer @{ customer_id: "CUS-42" }
```

### Pass named pipeline input

```exs
@pipeline create_order
  orders.create @{ customer_id: .customer_id, items: .items }
@end

pipeline.create_order @{ customer_id: .customer_id, items: .items }
```

### Use a pipeline request schema

```exs
@schema CustomerRequest {
  CustomerID string `json:"customer_id" validate:"required"`
}

@pipeline get_customer
  :request-schema=CustomerRequest
  customers.get @{ id: .customer_id }
@end
```

### Use request and response schemas

```exs
@schema OrderRequest {
  CustomerID string `json:"customer_id" validate:"required"`
}

@schema OrderResponse {
  OrderID string `json:"order_id" validate:"required"`
  Status string `json:"status" validate:"required"`
}

@pipeline create_order
  :request-schema=OrderRequest
  :response-schema=OrderResponse
  orders.create
@end
```

### Use schema aliases

```exs
@pipeline create_order
  :in-schema=OrderRequest
  :out-schema=OrderResponse
  orders.create
@end
```

### Use strict boundaries

```exs
@pipeline create_order
  :request-schema=!OrderRequest
  :response-schema=!OrderResponse
  orders.create
@end
```

### Validate in the middle of a pipeline

```exs
orders.create
-> OrderResponse.validate
-> fulfillment.start
```

## 9. Modules, includes, and macros

### Include a local module

```exs
@include "billing.nflow" AS billing

billing.pipeline.charge @{ payment_id: .payment_id }
```

### Include a bundle module

```exs
@require github.com/nexssp/si-ai v1.2.3 as ai
@include ai:nflows/lib/critic.nflow AS critic

critic.pipeline.review @{ text: .text }
```

### Declare a macro

```exs
@macro audit_label(value) {
  wrap @{ key: "audit_label", value: value }
}

const @{ value: "approved" } -> @audit_label("approval")
```

### Use a macro for a repeated policy shape

```exs
@macro normalize_status(value) {
  { status: value, normalized_at: .timestamp }
}

orders.get -> @normalize_status(.status)
```

## 10. Metadata and whole-pipeline recovery

### Describe a flow

```exs
@description "Create and fulfill an order"

orders.create
-> fulfillment.start
```

### Route errors for a whole pipeline

```exs
@on_error {
  when .kind == "Unavailable" -> support.retry,
  when .kind == "Forbidden" -> support.escalate,
  else -> error.reraise,
}

orders.create
-> fulfillment.start
```

## 11. Compile-time constants and configuration

### Declare scalar constants

```exs
@const DEFAULT_REGION = "eu-west-1"
@const MAX_ITEMS = 100

orders.list @{ region: DEFAULT_REGION, limit: MAX_ITEMS }
```

### Load JSON constants

```exs
@const.load "settings.json" as settings

http.request @{ base_url: settings.api_url, timeout: settings.timeout }
```

### Validate loaded constants

```exs
@schema Settings {
  APIURL string `json:"api_url" validate:"required"`
}

@const.load "settings.json" as settings :schema=Settings

http.request @{ base_url: settings.api_url }
```

### Declare configuration

```exs
@config {
  environment: "production"
  region: "eu-west-1"
  strict: true
}

orders.list
```

### Load JSON configuration

```exs
@config.load:path="settings.json" :prefix="app" :required=true

http.request @{ base_url: app.api_url }
```

### Enable strict typed requests

```exs
@config:strict=true

orders.create @{ customer_id: .customer_id, items: .items }
```

### Constrain compiler version

```exs
@nflow_version ">=0.59.0 <0.60.0"

orders.get @{ order_id: .order_id }
```

## 12. Symbols

### Declare named values

```exs
@symbols status {
  pending: "PENDING"
  paid: "PAID"
  cancelled: "CANCELLED"
}

orders.update @{ status: status.paid }
```

### Declare identity-valued symbols

```exs
@symbols queues { orders payments notifications }

jobs.enqueue @{ queue: queues.payments }
```

### Declare one-line symbols

```exs
@symbols roles { reviewer operator auditor }
```

### Declare an explicit empty value

```exs
@symbols labels { pending, done: }

const @{ label: labels.done }
```

### Declare typed symbols

```exs
@symbols policy {
  timeout: duration = 5s
  attempts: int = 3
  enabled: bool = true
}

service.call :timeout=policy.timeout :retry=policy.attempts
```

### Use symbols in assertions

```exs
@symbols status { pending: "PENDING", paid: "PAID" }

orders.get
@assert: result.status == status.paid
```

### Use an included symbol namespace

```exs
@include "shared-symbols.nflow" AS shared

orders.update @{ status: shared.status.paid }
```

## 13. Profiles and scopes

### Define a reusable profile

```exs
@profile reliable
  :timeout=5s
  :retry=3
  :idempotent

@pipeline charge
  :profile=reliable
  payments.charge
@end
```

### Inherit a profile

```exs
@profile base_api
  :timeout=3s
  :retry=2

@profile reliable_api
  :parent=base_api
  :cache=30s
  :idempotent

http.request :profile=reliable_api
```

### Scope a timeout

```exs
@scope :timeout=10s {
  orders.get
  payments.authorize
}
```

### Override a nested policy

```exs
@scope :timeout=10s {
  orders.get
  @scope :timeout=2s {
    health.check
  }
}
```

### Combine flags and values

```exs
payments.charge :timeout=5s :retry=2 :idempotent :dedup :coalesce @{ payment_id: .payment_id }
```

## 14. Pools, dispatch, and distribution

### Define a round-robin pool

```exs
@pool order_workers [orders.worker_a, orders.worker_b] {
  strategy: "round_robin"
}

pool.order_workers
```

### Define a failover pool

```exs
@pool payment_workers [payments.primary, payments.secondary] {
  strategy: "failover"
}

pool.payment_workers
```

### Define a hash-routed pool

```exs
@pool tenant_workers [tenants.worker_a, tenants.worker_b] {
  strategy: "hash"
  key: ".tenant_id"
}

pool.tenant_workers
```

### Dispatch to pool members

```exs
@pool workers [orders.worker_a, orders.worker_b]

dispatch.run @{ members: [pool.workers], payload: . }
```

### Distribute work over items

```exs
distribute.map @{ action: orders.validate, items: .orders }
```

### Distribute with a native action

```exs
distribute.map @{ action: noop, items: [1, 2, 3] }
```

## 15. Streams and files

### Read standard input as a stream

```exs
io.stdin
-> stream.take @{ count: 100 }
-> collect
```

### Map stream items

```exs
io.stdin
-> stream.map @{ expression: "item.email" }
-> collect
```

### Filter stream items

```exs
io.stdin
-> stream.filter @{ expression: "item.status == 'ready'" }
-> collect
```

### Fold a stream

```exs
io.stdin
-> stream.fold @{ expression: "acc + item.amount", initial: 0 }
-> collect
```

### Walk and read files

```exs
fs.walk @{ root: "./incoming" }
-> fs.read
-> fs.filter @{ expression: ".size > 0" }
-> fs.sort @{ by: "name" }
-> out.file @{ path: "./out/records.jsonl" }
```

### Write a result file

```exs
orders.export
-> out.file @{ path: "./out/orders.json" }
```

### Render a text template

```exs
template.render @{
  template: "Hello {{.customer_name}}, your order is {{.order_id}}.",
  variables: .
}
```

## 16. External capabilities and requirements

### Require an external bundle

```exs
@require github.com/nexssp/transport v0.9.0

http.request @{ method: "GET", url: .url }
```

### Require a bundle with an alias

```exs
@require github.com/nexssp/transportai v0.11.0 as ai

ai.complete @{ prompt: .prompt }
```

### Pass bundle options

```exs
@require retry {
  default_max: "4"
  backoff: "exponential"
  min_backoff: "100ms"
  max_backoff: "3s"
  retry_all: "false"
}

service.call :retry
```

### Run a language adapter

```exs
@require github.com/nexssp/lang/nexssflow vX.Y.Z

{ "x": 2, "y": 3 }
-> py"import json, sys; v=json.load(sys.stdin); print(json.dumps({'value': v['x'] + v['y']}))"
```

### Run a language file

```exs
@require github.com/nexssp/lang/nexssflow vX.Y.Z

{ "orders": [] }
-> go.file "./normalize.go"
```

### Embed a static asset

```exs
@embed invoice_template "assets/invoice.html"

files.stage @{ file: embed.invoice_template }
```

### Pass a capability reference

```exs
distribute.map @{ action: orders.validate, items: .orders }
```

### Pass a list of capabilities

```exs
dispatch.run @{ members: [orders.worker_a, orders.worker_b], payload: . }
```

## 17. Approval and durable execution

### Declare an approval gate

```exs
@approval finance_review {
  role: "finance-reviewer"
  reason: "confirm transfer"
  expires: 1h
}

payments.prepare
-> approval.finance_review
-> payments.commit
```

### Resume after approval

```exs
@approval release_review {
  role: "release-manager"
  reason: "approve production release"
  expires: 30m
}

release.prepare
-> approval.release_review
-> release.execute
```

### Attach a journal-aware idempotent action

```exs
payments.commit :idempotent :dedup
```

### Use a durable pipeline boundary

```exs
@pipeline transfer
  :idempotent
  payments.prepare
  -> approval.finance_review
  -> payments.commit :idempotent
@end
```

## 18. Hooks and event metadata

### Attach verification and metrics hooks

```exs
@hook:hook.verify,observe.metrics

orders.create @{ customer_id: .customer_id, items: .items }
```

### Declare an event trigger

```exs
@on event "nats:orders.created"

orders.process
```

### Declare an event pipeline

```exs
@on event "nats:payments.authorized"

payments.reconcile
```

## 19. Native and extension actions together

### HTTP to validation to storage

```exs
@require github.com/nexssp/transport v0.9.0
@require github.com/nexssp/storage v0.5.0

http.request @{ method: "GET", url: .url }
-> OrderResponse.validate
-> storage.put @{ key: .order_id, value: . }
```

### AI to schema to business action

```exs
@require github.com/nexssp/ai v0.1.1

ai.complete @{ prompt: .prompt }
-> json
-> ParsedResponse.validate
-> support.route
```

### Language worker to Flow projection

```exs
@require github.com/nexssp/lang/nexssflow vX.Y.Z

py.file "./score.py"
-> { customer_id: .customer_id, score: .score, tier: .tier }
```

### Transport with resilience policies

```exs
@require github.com/nexssp/transport v0.9.0
@require resilience

http.request :timeout=5s :retry=3 :rate_limit=100 :circuit_breaker="payments" @{ method: "POST", url: .url, body: .body }
```

## 20. Complete practical flows

### Order intake

```exs
@nflow_version ">=0.59.0 <0.60.0"

@schema OrderRequest {
  CustomerID string `json:"customer_id" validate:"required"`
  ItemIDs []string `json:"item_ids" validate:"required,dive,required"`
}

@schema OrderResponse {
  OrderID string `json:"order_id" validate:"required"`
  Status string `json:"status" validate:"required"`
}

@profile reliable
  :timeout=5s
  :retry=2
  :idempotent

@pipeline create_order
  :request-schema=!OrderRequest
  :response-schema=!OrderResponse
  :profile=reliable
  orders.create
  -> inventory.reserve
  -> payments.authorize
  -> fulfillment.start
@end

pipeline.create_order @{ customer_id: .customer_id, item_ids: .item_ids }
@assert: result.order_id != ""
```

### Customer dashboard

```exs
@pipeline dashboard
  parallel {
    customer: customers.get @{ id: .customer_id },
    orders: orders.list @{ customer_id: .customer_id },
    balance: billing.balance @{ customer_id: .customer_id },
  }
  -> {
    customer: .customer,
    orders: .orders,
    balance: .balance,
    generated_at: .timestamp,
  }
@end

pipeline.dashboard @{ customer_id: "CUS-42" }
```

### Resilient payment

```exs
@profile payment_policy
  :timeout=5s
  :retry=3
  :idempotent
  :dedup
  :coalesce

@pipeline charge
  :profile=payment_policy
  payments.primary
  || payments.secondary
  ? on_error {
    when .kind == "Forbidden" -> support.escalate,
    else -> error.reraise,
  }
@end

pipeline.charge @{ payment_id: .payment_id, amount: .amount }
```

### Approval-controlled release

```exs
@approval production_release {
  role: "release-manager"
  reason: "approve deployment"
  expires: 1h
}

@pipeline release
  :idempotent
  release.prepare
  -> approval.production_release
  -> release.execute
  -> release.verify
@end

pipeline.release @{ version: .version, environment: "production" }
```

### Batch file processing

```exs
@profile batch_policy
  :timeout=30s
  :retry=2

@pipeline process_batch
  :profile=batch_policy
  fs.walk @{ root: "./incoming" }
  -> fs.read
  -> stream.filter @{ expression: ".size > 0" }
  -> distribute.map @{ action: records.normalize, items: . }
  -> out.file @{ path: "./out/normalized.jsonl" }
@end

pipeline.process_batch
```

### Event-driven reconciliation

```exs
@require github.com/nexssp/transportnats v0.5.0
@require github.com/nexssp/storage v0.5.0

@on event "nats:payments.authorized"

payments.fetch
-> payments.reconcile
-> storage.put @{ key: .payment_id, value: . }
```
