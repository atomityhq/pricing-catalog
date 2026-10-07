# Maintainer-only evaluation

This directory documents the intended maintainer evaluation hook.

Do **not** store hidden test fixtures, expected candidate outputs, or other secret evaluation inputs in the public repository.

The public repository can run visible CI checks. Maintainers can run additional evaluation against private inputs supplied outside the repository.


## Private evaluation input

Private evaluation data is supplied at runtime through:

    PRICING_CATALOG_EVAL_DIR

The directory is never committed to this repository.

Its structure is task-specific:

```text
<private-eval-dir>/
└── <task-id>/
    ├── cases.yaml
    ├── fixtures/
    └── expected/

The evaluator must fail if the directory is missing or incomplete. It must never silently fall back to public fixtures for private correctness checks.

## Evaluation flow

```text
candidate PR
  ↓
visible CI
  ↓
maintainer-only evaluation workflow
  ↓
private evaluation inputs
  ↓
candidate connector / pipeline
  ↓
private correctness checks
  ↓
automated score
  ↓
technical walkthrough
```
+The evaluator checks the output of the candidate's real connector and existing
+pipeline. It must not become a second implementation of the provider.

## Security

Never commit:

- hidden fixtures;
- expected candidate outputs;
- provider-specific grading data;
- private scoring inputs;
- credentials or access tokens.

GitHub Actions should inject private evaluation data only into a maintainer-only
workflow.

Repository rules should require maintainer/Code Owner review for framework
changes.
