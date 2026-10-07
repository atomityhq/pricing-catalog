---
id: hetzner-cloud-servers
provider: hetzner
service: cloud-servers
target_connector: connectors/hetzner/cloud-servers/
---

# Hetzner Cloud Server Pricing Connector

## Assignment

Implement a pricing connector for Hetzner Cloud server pricing.

The connector must integrate with the existing connector and catalog pipeline
and produce canonical pricing records for the supported pricing scope.

## Target connector

`connectors/hetzner/cloud-servers/`

## Authoritative source

Use Hetzner's official Cloud pricing/API documentation as the authoritative
source for the pricing model and response structure.

The connector should model the `server_types` pricing returned by Hetzner's
Cloud pricing API.

## Scope

Cover Hetzner Cloud server types and preserve the meaningful distinctions
present in the provider data.

At minimum, consider:

- server type
- provider server-type identifier
- location
- hourly price
- monthly price
- currency

## Functional requirements

- Normalize provider pricing into the canonical pricing model.
- Preserve server type identity.
- Preserve provider identifiers.
- Preserve location distinctions.
- Represent hourly and monthly pricing without silently collapsing them.
- Preserve the provider currency.
- Produce deterministic output.
- Satisfy the generic connector contract.

## Constraints

- Use the existing connector and catalog pipeline.
- Do not bypass the canonical model.
- Do not introduce provider-specific behavior into the public catalog API.
- Do not require private credentials for tests.
- Do not use private evaluation data.
- If a provider distinction cannot be represented cleanly, document the
  limitation instead of silently discarding it.

## Deliverables

- Connector implementation.
- Realistic deterministic provider-response fixture.
- Unit tests.
- Connector contract tests.
- Documentation of the authoritative source and mapping decisions.

## Submission

Submit a pull request describing:

- authoritative source
- provider response structure
- pricing model
- normalization decisions
- assumptions and limitations
- test coverage