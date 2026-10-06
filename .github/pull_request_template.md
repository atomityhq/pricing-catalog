<!--
Title: use Conventional Commits, e.g. "feat(connectors): add hetzner connector" or "fix(catalog): tier ordering in validation".
Keep the PR small and focused on one change. Delete any section that doesn't apply.
-->

## Description

<!-- What does this change, and why? Link the issue it closes. -->

Closes #

## Type of change

- [ ] New provider connector
- [ ] Bug fix
- [ ] Pricing data fix (wrong, missing or outdated prices)
- [ ] New feature
- [ ] Refactor (no behaviour change)
- [ ] Documentation
- [ ] Build, CI or tooling
- [ ] Breaking change (canonical schema or public Go API changes that need action from consumers)

## Connector details

<!-- For connector PRs (see CONTRIBUTING.md → Pull requests). Delete otherwise. -->

- **Source used** (and why it is authoritative):
- **Provider pricing model** (products, SKUs, regions, purchase models, units, tiers, currencies):
- **Mapping to the canonical schema:**
- **Intentionally not represented:**
- **Known limitations:**

## How to test

<!-- How a reviewer can see the change working (and, for a fix, see the bug before it). -->

1.
2.
3.

**Expected result:**

## Checklist

**Title**
- [ ] The PR title follows `type: short summary` (`feat`, `fix`, `docs`, `chore`, `refactor`, …)

**Tests**
- [ ] `make check` passes (gofmt, `go vet`, `go test ./...`) — or the pre-commit hook ran it
- [ ] `make validate-example` passes
- [ ] I added or updated tests that cover this change
- [ ] Every parsing or normalization edge case I fixed has a regression test
- [ ] Non-trivial provider data has deterministic fixtures under `testdata/`

**Code**
- [ ] The git hooks ran on my commits (no `--no-verify`)
- [ ] No floating-point numbers for catalog prices
- [ ] Provider-specific behaviour stays inside the connector; the canonical schema and consumer API are unchanged (or the change is proposed in `DESIGN.md`)
- [ ] No secrets, API keys or personal data in code, logs or fixtures

**Docs**
- [ ] The connector has a `DESIGN.md` (sources, mapping decisions, assumptions, limitations, how to run its tests)
- [ ] README / CONTRIBUTING / `docs/` updated if behaviour or setup changed
- [ ] Meaningful AI assistance is disclosed
