// DRIFT: two contract breaks at once — the function is no longer exported
// (public → private) and its return type changed (ValidationResult<Money> →
// string). The intent still declares the original public contract.
function validateDonation(amount: Money, campaign: Campaign): string {
  if (amount.cents < campaign.minimumCents) {
    return "amount_below_minimum"
  }
  return "ok"
}
