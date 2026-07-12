import { ok, err } from "./result"

// validateDonation checks a donation against a campaign's minimum. It is pure —
// no I/O, no logging — and reports a below-minimum donation as a Result error
// rather than throwing.
export function validateDonation(amount: Money, campaign: Campaign): ValidationResult<Money> {
  if (amount.cents < campaign.minimumCents) {
    return err("amount_below_minimum")
  }
  return ok(amount)
}
