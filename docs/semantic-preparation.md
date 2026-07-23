# Semantic preparation

`ce iir prepare` compiles a request into the evidence-backed contract given to
an implementation LLM or harness agent. It is deliberately not a source
generator.

```text
natural language / declared IIR
  -> candidate intent
  -> target and context selection
  -> active plugin semantic policies
  -> immutable decorated SemanticPlan
  -> implementation packet
  -> LLM writes source
  -> active plugins lift source back to observed IIR
  -> verification and repair guidance
```

## Shaping executors

For local development, a natural-language request uses the configured direct
LLM provider. `--intent` bypasses model shaping entirely. The semantic shaping
package accepts an `IntentShaper` interface, so an agent harness can supply the
same validated candidate-IIR output without CE requiring a provider key.

Both paths produce the same candidate-IIR boundary: model or harness output is
validated before it becomes a plan; framework requirements are never delegated
to that model call.

## Plugin policies

An SDK plugin can declare a `semanticPolicies` pack:

```ts
semanticPolicies: {
  schemaVersion: "v1",
  languages: ["php"],
  policies: [{
    id: "example.checkout.validation-hook",
    version: "v1",
    phase: "constrain",
    severity: "error",
    when: { claimKinds: ["context.woocommerce.checkout"] },
    add: {
      kind: "hook",
      requirement: "invoke the checkout validation hook",
      mandatory: true,
    },
  }],
}
```

The host parses and validates this data. It records a resulting obligation with
the plugin ID/version as evidence. A non-PHP plan never selects this pack; a
PHP plan without the checkout context records no hook obligation. Conflicting
mandatory obligations block the plan rather than using load order as authority.

## CLI

```sh
# Direct provider shaping in development.
ce iir prepare "Validate a checkout key" \
  --target CheckoutValidator.validate_key \
  --language php \
  --context woocommerce.checkout

# No LLM/provider required.
ce iir prepare --intent ./intent.yaml --target CheckoutValidator.validate_key

# Emit only the prompt packet for an implementation agent.
ce iir prepare --intent ./intent.yaml --target CheckoutValidator.validate_key --prompt
```

`--context` is an explicit caller declaration, useful for an early integration
or an agent that already established the context. It is not a substitute for
graph-backed/plugin-observed resolution; its provenance stays `user`.

See [controlled semantic tags](semantic-tags.md) for the tag vocabulary used by
the shaper, resolver, and policy selectors. `--tag` accepts a validated tag
directly; `--context woocommerce.checkout` remains shorthand for
`context.woocommerce.checkout`.
