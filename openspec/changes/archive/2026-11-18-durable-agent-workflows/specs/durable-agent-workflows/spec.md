# Spec Delta

## Purpose

This capability gives multi-Agent coordinators a durable, Circle-scoped record of workflow progress, attempts, deadlines, cancellation, failures, and join-policy outcomes that remains queryable after client or Hub restarts.

## ADDED Requirements

### Requirement: Agents can create and query durable workflows

The Hub SHALL expose `POST /hub/v1/workflows`, `GET /hub/v1/workflows`, and `GET /hub/v1/workflows/{workflowId}`. An authenticated Agent SHALL create a workflow with a caller-provided workflow ID, flow type, join policy, expected step count, minimum success quorum, deadline, retry limit, and idempotency key. Supported join policies SHALL be `ALL_SUCCESS`, `QUORUM`, `FIRST_SUCCESS`, and `PARTIAL_FAILURE`. Workflow IDs SHALL be unique within the Hub and Circle. Repeating a create request with the same idempotency key and content SHALL return the existing workflow; reusing the key with different content SHALL return a conflict. The owner SHALL be able to list and query only its own workflows, scoped to its Hub and Circle. The Hub SHALL cap each owner at 100 active workflows and SHALL rate-limit workflow writes using the existing per-Agent task limiter.

#### Scenario: Owner creates workflow
- **WHEN** an authenticated Agent creates a valid workflow
- **THEN** the Hub persists it as RUNNING and returns its policy, deadline, and timestamps

#### Scenario: Create retries are idempotent
- **WHEN** the owner repeats a workflow create request with the same idempotency key and body
- **THEN** the Hub returns the same workflow without creating duplicate state

#### Scenario: Another owner cannot read workflow
- **WHEN** another Agent in the same or a different Circle queries a workflow it does not own
- **THEN** the Hub returns not found without revealing the workflow's existence

### Requirement: Workflow steps track bounded delivery attempts and outcomes

The Hub SHALL expose `POST /hub/v1/workflows/{workflowId}/steps` for step/attempt registration and `POST /hub/v1/workflows/{workflowId}/steps/{stepId}/attempts/{attempt}/outcome` for target outcome reports. The workflow owner or an Agent already assigned to a workflow step SHALL register a step with a target Agent, task ID, step ID, attempt number, and idempotency key. The Hub SHALL confirm that the referenced task was sent by the authenticated registering Agent to the stated target in the same Circle. The assigned target Agent SHALL report attempt state and bounded result/error summary using its own credential. A non-owner mutation response SHALL expose only the affected step and attempt state, not the workflow's other steps or results. Each retry SHALL use the next monotonically increasing attempt number; the Hub SHALL reject retries beyond the workflow retry limit or after the workflow deadline. A failed step with no retry budget remaining SHALL be recorded as DEAD_LETTER and remain queryable by the owner.

#### Scenario: Target reports attempt outcome
- **WHEN** an assigned target Agent reports a valid terminal outcome for its current attempt
- **THEN** the Hub persists that outcome and recomputes the workflow state atomically

#### Scenario: Retry advances the attempt
- **WHEN** the workflow owner registers the next attempt after a failed attempt and before deadline
- **THEN** the Hub records the next attempt while preserving previous attempt history

#### Scenario: Retry budget is exhausted
- **WHEN** an attempt fails after the configured retry limit is consumed
- **THEN** the step is marked DEAD_LETTER and appears in the workflow query result

#### Scenario: Assigned participant delegates a child step
- **WHEN** an Agent assigned to a workflow step sends and registers a child task
- **THEN** the Hub adds the child step when the task sender, target, and Circle match the authenticated participant

### Requirement: Workflow deadlines, cancellation, and join policies have durable outcomes

The Hub SHALL expose `POST /hub/v1/workflows/{workflowId}/cancel`. Workflow reads and outcome writes SHALL enforce the deadline by durably transitioning overdue running workflows to TIMED_OUT and active steps to TIMED_OUT or DEAD_LETTER as appropriate. The owner SHALL be able to cancel a running workflow; cancellation SHALL mark remaining active steps CANCELED and cancel their linked inbox items that remain pending. Cancellation cannot interrupt work already running in another Agent process; late outcome reports SHALL be rejected. The Hub SHALL compute a stable terminal workflow state from step outcomes and the selected join policy, and preserve it across restart. Workflow queries SHALL return expected and minimum-success counts, per-state step counts, step identities, current attempts, and prior terminal attempt summaries.

#### Scenario: Quorum is reached
- **WHEN** successful steps reach the configured minimum success quorum
- **THEN** the workflow becomes COMPLETED and exposes completed, failed, canceled, and dead-letter counts

#### Scenario: Quorum becomes impossible
- **WHEN** all possible steps are terminal and the success quorum cannot be reached
- **THEN** the workflow becomes FAILED with its per-step outcomes queryable

#### Scenario: Owner cancels workflow
- **WHEN** the owner cancels a running workflow
- **THEN** the workflow and all active steps become CANCELED and later reports cannot overwrite them

#### Scenario: Deadline expires across restart
- **WHEN** a workflow deadline passes while the Hub is unavailable
- **THEN** the first subsequent query or update persists TIMED_OUT state and returns the same terminal outcome on later queries
