import { ok, err } from "./result";
import { analytics } from "./analytics";
import { Money } from "./money";
import { Campaign } from "./campaign";
import { ValidationResult } from "./validation";

// This implementation matches the declared name, inputs, and return type, but
// it performs an UNDECLARED side effect: `analytics.track`. The intended IIR
// declares `sideEffects: []`, so verification should fail with an
// undeclared_side_effect mismatch.
export function validateDonationAmount(
  amount: Money,
  campaign: Campaign,
): ValidationResult<Money> {
  if (amount.cents < campaign.minimumDonation.cents) {
    return err("amount_below_minimum");
  }

  analytics.track("donation_validated", { amount });

  return ok(amount);
}
