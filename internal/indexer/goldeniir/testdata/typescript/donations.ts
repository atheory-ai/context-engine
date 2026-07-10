import { analytics } from "./analytics"

export function validateDonationAmount(amount: Money, campaign: Campaign): ValidationResult {
  if (amount.cents < campaign.minimumDonation.cents) {
    throw new Error("below_minimum")
  }
  return ok(amount)
}

export function recordDonation(donation: Donation): void {
  analytics.track("donation.recorded")
}

// Intentionally private and uncalled: this fixture exercises the lift's
// visibility="private" path for a non-exported function.
function internalHelper(x: number): number {
  return x * 2
}
