# Maintainer-only evaluation

This directory documents the intended maintainer evaluation hook.

Do **not** store hidden test fixtures, expected candidate outputs, or other secret evaluation inputs in the public repository.

The public repository can run visible CI checks. Maintainers can run additional evaluation against private inputs supplied outside the repository.

Recommended future shape:

```text
candidate PR
    ↓
visible CI
    ↓
maintainer checkout
    ↓
private evaluation inputs
    ↓
hidden contract/edge-case checks
    ↓
technical walkthrough
```

A simple future command can be added once the hiring workflow is finalized, for example:

```bash
make evaluate PRIVATE_EVAL_DIR=/path/to/private-fixtures
```

The exact mechanism should be decided after the connector contract is stable.


Repository rules should require maintainer/Code Owner review for framework changes.
Do not commit hidden fixtures, expected candidate outputs, provider-specific grading
data, or other secret evaluation inputs to the public repository.
