# IIR verify demo (TypeScript)

A runnable walk-through of Context Engine's **IIR verification loop**: you declare
a function's *intent* once, and the engine checks that the *source* still upholds
it — catching drift a type-checker won't, like an undeclared side effect or a
flipped condition.

## The idea

[`intents/validateDonation.iir.yaml`](intents/validateDonation.iir.yaml) declares
the contract for a `validateDonation` function:

- its **inputs** (`amount: Money`, `campaign: Campaign`) and **return type**
  (`ValidationResult<Money>`),
- that it is **public**,
- its **behavior** — when `amount.cents < campaign.minimumCents`, it returns the
  `amount_below_minimum` error (declared as a structured `whenExpr`, so the shape
  of the condition is compared, not just its presence),
- and that it performs **no side effects**.

[`src/validateDonation.ts`](src/validateDonation.ts) is an implementation that
matches. The three `src/drift-*.ts` files are copies where something drifted away
from the intent.

## Run it

The demo needs a `ce` binary with the default language plugins embedded. From the
repo root:

```bash
make bundle-default-plugins   # build + embed the go/ts/python plugins
make build                    # produces ./ce
```

Then run the guided script (point `CE` at the binary you built):

```bash
cd examples/ts-demo
CE=../../ce ./demo.sh
```

Or verify a single file yourself:

```bash
ce iir verify intents/validateDonation.iir.yaml src/validateDonation.ts
```

`ce iir verify` exits non-zero when verification fails, so it drops straight into
CI or a pre-commit hook.

## What each case shows

| source | result | why |
|---|---|---|
| `src/validateDonation.ts` | **passes** | matches the intent on every axis |
| `src/drift-undeclared-effect.ts` | **error** | adds `logger.info(...)` — an undeclared, recognized (log) side effect |
| `src/drift-changed-behavior.ts` | **warning** | flips `<` to `>`; the structured `whenExpr` no longer matches the declared behavior |
| `src/drift-changed-contract.ts` | **error** | drops `export` (public → private) and changes the return type |

The intent never changes — only the code drifts, and each divergence is surfaced.
Contract breaks are **errors** (they fail verification); the behavior change is a
**warning** (flagged, but non-blocking) — the report separates the two so you
decide what gates a merge.

> Note: the report mentions a `rules:` file. `ce iir verify` auto-discovers an
> `iir.rules.yaml` walking up from the working directory (here it finds the repo's
> `examples/iir.rules.yaml`) and layers it over the built-in defaults.
