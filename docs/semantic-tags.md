# Controlled semantic tags

Semantic tags are a small controlled vocabulary between a natural-language
request, graph-backed target resolution, and host-evaluated plugin policies.
They are carried as provenance-bearing `SemanticPlan` claims, never as source
text or an authorization to guess framework behavior.

## Tag families

| Family | Examples | Meaning |
| --- | --- | --- |
| operation | `operation.mutate`, `operation.cart.modify`, `operation.block.register` | The requested domain operation. |
| surface | `surface.wordpress.request`, `surface.woocommerce.store_api`, `surface.gutenberg.editor` | The invocation/runtime boundary. |
| input/output | `input.user_controlled`, `output.html`, `output.user_facing_text` | Data crossing a trust or presentation boundary. |
| effect | `effect.database.query`, `effect.cart.mutation`, `effect.persisted_content` | An intended persistent or externally visible effect. |
| context | `context.wordpress.plugin`, `context.woocommerce.cart`, `context.gutenberg.block` | A framework or architectural context. |

The complete v1 allow-list belongs to `internal/semantic/vocabulary`. An
unknown model-proposed or caller-declared tag is rejected. A model-produced tag
is an **inferred** claim; a `--tag`/`--context` value is **declared**; a future
graph/plugin resolver must emit it as **observed** or **resolved**. The state
is retained in packet evidence.

## Policy selection

`when.claimKinds` remains an any-of selector for compatibility. New policies
use `when.allClaimKinds` for conjunctive conditions, especially for security
rules. For example, a WordPress nonce policy requires both
`surface.wordpress.request` and `operation.mutate`; a request mentioning an
unrelated mutation does not activate it.

```ts
when: {
  allClaimKinds: ["context.woocommerce.cart", "operation.cart.modify"],
}
```

Framework packs add implementation obligations only after selection. They do
not assert that a tag is true, and they cannot execute arbitrary code or write
to the substrate.

## Current CLI seam

`ce iir prepare` asks a direct/harness shaper to return only tags from this
allow-list. Agents with established context can use `--tag`; `--context` is the
compatibility shorthand for a `context.*` tag.

```sh
ce iir prepare "Add a customer-controlled cart update" \
  --target StoreApi.CartController.update_item \
  --language php \
  --tag context.woocommerce.cart \
  --tag operation.cart.modify \
  --tag input.user_controlled
```

This explicit seam is temporary. The target resolver and framework extractors
should produce the same tags from graph/source evidence before a packet can be
considered fully evidence-backed.
