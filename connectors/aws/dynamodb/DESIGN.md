# DynamoDB pricing connector

## Source and scope

The connector reads the [public AWS Price List offer for Amazon DynamoDB in
us-east-1](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonDynamoDB/current/us-east-1/index.json).
AWS publishes this file directly. Fetching it requires no credentials.
The default endpoint covers US East (N. Virginia).

## Pricing and normalization

- Join AWS products and pricing terms by SKU. Read the region from the
  product's `regionCode`.
- Map AWS `OnDemand` and `Reserved` terms to `on_demand` and `reserved`.
  An AWS `OnDemand` term can also price provisioned capacity. The
  original `usagetype` is retained in `PricingDimension` to preserve
  distinctions such as request units, provisioned capacity, and
  Standard-Infrequent Access usage.
- Emit one record per price dimension and currency. Keep the AWS rate
  code, original billing unit, and exact decimal price string.
- Preserve tier boundaries. Map `Inf` to an open upper bound.
- Preserve the reserved contract duration. Hourly charges and upfront
  fees are separate records. Upfront fees without range bounds have no tier.

## Limitations

The connector loads a single regional offer per fetch. It preserves the
AWS usage type as supplied and leaves it to consumers to interpret
provider-specific usage codes. Effective dates and reserved term
attributes other than contract duration are currently omitted.

## Tests

Run `go test -v ./connectors/aws/dynamodb -count=1` from the repository
root. The local AWS fixture contains 46 price records. The tests check
the connector contract, an on-demand read price, free and paid write
capacity tiers, and upfront fees for one- and three-year reservations.