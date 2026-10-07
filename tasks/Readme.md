# Candidate Tasks

Candidate tasks are provider/service connector assignments.

Each task lives in its own directory:

    tasks/<task-id>/

and contains a `TASK.md` describing the assignment.

## Task metadata

Every task must define:

- `id` — stable task identifier
- `provider` — cloud/provider name
- `service` — provider service or pricing domain
- `difficulty` — `easy`, `medium`, or `hard`
- `target_connector` — required connector directory

The target connector must follow:

    connectors/<provider>/<service>/

## Example

    id: aws-ec2
    provider: aws
    service: ec2
    difficulty: medium
    target_connector: connectors/aws/ec2/

## Task contents

Every `TASK.md` should define:

1. Assignment
2. Target connector
3. Scope
4. Research requirements
5. Functional requirements
6. Constraints
7. Deliverables
8. Submission requirements

Task documents must not contain:

- private evaluation data
- hidden fixtures
- expected answers
- maintainer-only scoring inputs

Those belong to the private evaluation process.