import { ok, err } from "./result"
import { logger } from "./logger"

// DRIFT: someone added logging. The intent declares no side effects, so the
// engine flags an undeclared, high-confidence effect (logger.* → log).
export function validateDonation(amount: Money, campaign: Campaign): ValidationResult<Money> {
  logger.info("validating donation")
  if (amount.cents < campaign.minimumCents) {
    return err("amount_below_minimum")
  }
  return ok(amount)
}
