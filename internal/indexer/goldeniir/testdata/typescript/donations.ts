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

function internalHelper(x: number): number {
  return x * 2
}
