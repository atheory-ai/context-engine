import { ok, err } from "./result";
import { db } from "./db";
import { Money } from "./money";
import { Campaign } from "./campaign";
import { ValidationResult } from "./validation";

// This implementation matches the declared name, inputs, and return type, but
// it performs an UNDECLARED side effect: `db.query`, which the classifier
// resolves to a database effect. The intended IIR declares `sideEffects: []`,
// so verification should fail with an undeclared_side_effect mismatch.
export function validateDonationAmount(
  amount: Money,
  campaign: Campaign,
): ValidationResult<Money> {
  if (amount.cents < campaign.minimumDonation.cents) {
    return err("amount_below_minimum");
  }

  db.query("insert into audit (event) values ('donation_validated')");

  return ok(amount);
}
