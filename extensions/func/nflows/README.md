# Practical `@func` examples

These small `.nflow` files demonstrate pure, typed helpers used in everyday business calculations. Each flow accepts the input fields listed below; omitted fields use the representative sample defaults shown here, so every example can also run with an empty input. Explicit input values override the defaults. The files are examples, not production policy: adjust thresholds, rates, and legal/business rules for your own context.

| Example | Input fields (defaults when omitted) | What it demonstrates |
|---|---|---|
| [`order_pricing.nflow`](order_pricing.nflow) | `quantity` (3), `unit_price_minor` (1299), `customer_tier` (`gold`), `tax_rate_percent` (8) | Reused subtotal, tier-discount, tax, and total calculations; amounts are in minor currency units such as cents. |
| [`shipping_quote.nflow`](shipping_quote.nflow) | `subtotal_minor` (7000), `destination` (`domestic`) | A boolean eligibility helper composed into a fee calculation. Uses `domestic` or another destination label. |
| [`refund_eligibility.nflow`](refund_eligibility.nflow) | `days_since_purchase` (20), `item_returned` (true), `final_sale` (false) | A multi-condition policy predicate that returns a boolean. |
| [`invoice_status.nflow`](invoice_status.nflow) | `is_paid` (false), `days_until_due` (-2) | A readable nested decision that produces `paid`, `open`, `due_soon`, or `overdue`. |
| [`support_priority.nflow`](support_priority.nflow) | `impact` (5), `production_down` (true) | Function composition for a priority label and response target. |
| [`reusable_shipping_fee.nflow`](reusable_shipping_fee.nflow) | `subtotal_minor`, `destination_zone` | The compact one-line `= expression` form, typed schemas, and reuse across pipelines. |

The `@func` bodies are pure expr-lang expressions. The top-level projections use local `let` bindings to apply defaults and avoid repeating intermediate calculations; they perform no I/O or Flow actions. The compact equals form remains supported, as shown in `reusable_shipping_fee.nflow`.
