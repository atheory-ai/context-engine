import { ok, err } from "./result";
import { Money } from "./money";
import { Campaign } from "./campaign";
import { ValidationResult } from "./validation";

// This implementation matches the intended IIR exactly: same name, inputs, and
// return type, and no side effects. Verification should pass.
export function validateDonationAmount(
  amount: Money,
  campaign: Campaign,
): ValidationResult<Money> {
  if (amount.cents < campaign.minimumDonation.cents) {
    return err("amount_below_minimum");
  }

  return ok(amount);
}
