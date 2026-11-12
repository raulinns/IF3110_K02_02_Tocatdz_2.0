# Fixtures for consumers

All values are synthetic. These files describe wire payloads, not live database
contents, executable seed migrations or evidence of implemented routes.

| Fixture | Schema / purpose |
|---|---|
| migration-normal.json | Migration: draft, full SQL, real checksum test vector |
| migration-frozen.json | Same definition frozen after submission, before first execution |
| migration-batch-input.json | DefinitionInput: K4 assignments and separate identifiers |
| change-request-pending.json | ChangeRequest revision 1, two groups, writer and viewer leaders |
| change-request-one-approved.json | Revision 2, writer leader approved, viewer leader still pending |
| change-request-approved.json | Revision 3, both leaders approved |
| job-queued.json | Job before claim; attempt 0, nullable times/total/error |
| job-partial.json | Interrupted batch with four proven commits; resume/revert needs new CR |
| job-uncertain.json | Unknown commit, no automatic SQL retry, await reconciliation |
| job-partially-reverted.json | Compensation job with partial progress; preserve previous counters |
| history-hide-rollback.json | Full/filtered ExecutionsPage and PDF E1–E5 labels, newest first |
| error-conflict.json | ErrorEnvelope with stable reason revision_mismatch |
| checksum-v1.json | Exact UTF-8 checksum input and expected digest; not an HTTP response |

For a mock review journey, use pending -> one-approved -> approved. Two distinct
leader identities approve group 1 with revision 1, then group 2 with revision 2.
The requester is a third account, compatible with the provisional self-approval
policy. Execute with revision **3**, then serve job-queued. Read the frozen
migration for SQL review; summary list endpoints strip SQL fields per K1.

Partial/uncertain/compensation fixtures are independent scenarios, not sequential
state changes to replay blindly on the same job. For a live recovery mock, create
new CR/job IDs and source_job_id references as K2 requires. History's dummy SQL
and checksums illustrate ordering/identity only and do not serve as a seed chain.

`migration-batch-input` is safe to reverse only if the runner proves/persists the
original affected rows and their preimage (processed=false); the example predicate
must not be re-evaluated after UPDATE to locate compensation rows.
