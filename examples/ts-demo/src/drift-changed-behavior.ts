import { ok, err } from "./result"

// DRIFT: the comparison was flipped (< became >). The structured whenExpr no
// longer matches the declared behavior — a behavior-content divergence.
export function validateDonation(amount: Money, campaign: Campaign): ValidationResult<Money> {
  if (amount.cents > campaign.minimumCents) {
    return err("amount_below_minimum")
  }
  return ok(amount)
}
