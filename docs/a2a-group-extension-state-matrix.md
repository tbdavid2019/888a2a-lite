# A2A Group Coordination Extension state matrix

The extension uses only A2A core Task states. `PARTIAL` is never emitted; a
partial member result is represented by Parent Task artifacts and metadata.

| Parent condition | Parent state | Admission rule | Result handling |
| --- | --- | --- | --- |
| Fan-out committed, no member ACK | `TASK_STATE_SUBMITTED` | Members may ACK or the requester may cancel | No partial write is allowed |
| At least one member ACK, work remains | `TASK_STATE_WORKING` | No Parent cancellation | ACK is receipt only |
| Every member completed, including empty results | `TASK_STATE_COMPLETED` | No new turn | Aggregate artifacts in deterministic member order |
| Any member failed, rejected, or deadline expired | `TASK_STATE_FAILED` | No new update can revive it | Preserve successful and failed member summaries |
| Requester cancels before every member ACK | `TASK_STATE_CANCELED` | All unacknowledged deliveries are canceled | Late ACK/result is rejected |
| No eligible recipient | no Parent Task | No execution admission | Return bounded error with no database or audit write |

Member tasks use the same core states. Their `turnId`, `revision`, and
`updateId` are independent, while `parentTaskId` and `memberTaskId` provide
the durable correlation. The Parent revision increases after each accepted
member ACK or outcome update. Results are sorted by the transaction snapshot's
ordinal and then target Agent ID, so reconnects reproduce the same order.

Execution deadlines are separate from HTTP wait deadlines. The reaper marks
expired work failed and persists the event; a request timeout only returns 504
and does not cancel the durable work. Retention is conservative: Parent,
Member, delivery, result, and event records are not removed by Agent pruning.
