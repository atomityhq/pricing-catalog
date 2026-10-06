# Contributing

Thanks for contributing to `pricing-catalog`.

## General expectations

- Keep provider-specific behavior inside the provider connector.
- Normalize data into the canonical model instead of adding provider-specific fields to the generic API unless the distinction is genuinely universal.
- Preserve commercially meaningful pricing dimensions.
- Prefer authoritative provider sources over rendered website scraping when a structured source exists.
- Do not use floating-point numbers for catalog prices.
- Add deterministic fixtures for non-trivial provider data.
- Add regression tests for every parsing or normalization edge case you fix.
- Document important assumptions and known limitations.
- Treat `internal/pipeline` as the canonical path from connector output to a catalog snapshot.
- Do not add provider-specific snapshot/update/persistence mechanisms.
- Do not modify the canonical schema or consumer API just to fit one provider.

## Local checks

```bash
make check
make validate-example
```

## Git hooks

Optional local hooks catch common mistakes before you commit and push. They are a developer convenience; CI is the authoritative check. Turn them on once after cloning:

```bash
make hooks
```

| Hook | When | What it checks |
|------|------|----------------|
| `pre-commit` | `git commit` | No `.env` files, private keys, conflict markers or files over 5 MB. When Go code, `go.mod`/`go.sum`, `testdata/` fixtures or snapshots are staged, it also runs `gofmt` on those Go files, `go vet ./...` and `go test ./...` — against the working tree, not the staged snapshot, so treat it as a quick sanity check. |
| `pre-push` | `git push` | Light, about a second: refuses direct pushes to `main`, and re-checks the outgoing commits for `.env` files, private keys and conflict markers (in case a commit skipped its hook). No builds or tests. |

The hooks live in `.githooks/`. To skip them once, in an emergency, use `--no-verify` (or `SKIP_HOOKS=1`).

## Connector structure

A connector will normally contain:

```text
connectors/<provider>/
├── connector.go
├── parser.go            # optional
├── normalizer.go        # optional
├── connector_test.go
└── testdata/
```

The split between files is up to the contributor. Keep the architecture understandable rather than creating abstractions for their own sake.

## Pull requests

The pull request should explain:

1. which source was used;
2. how the provider pricing model works;
3. how it maps to the canonical schema;
4. what information is intentionally not represented;
5. what tests and fixtures were added;
6. any known limitations.

The pull request template asks for these, plus a short checklist; fill in what applies.

## Repository protection

The repository is public, but the canonical framework is maintainer-owned.
Configure the default branch so that pull requests require Code Owner approval
and passing CI before merge. Replace the placeholder owner in `.github/CODEOWNERS`
with the actual maintainer account/team.

## Reporting bugs and ideas

Open an issue and pick the form that fits — bug report (including wrong or missing prices), feature request (including new providers), documentation issue, or question/discussion.
For security problems, follow [SECURITY.md](SECURITY.md) instead.
