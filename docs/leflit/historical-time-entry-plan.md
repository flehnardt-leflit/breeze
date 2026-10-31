# LEFLIT / Breeze – Historical manual time entry (draft)

> Status: design draft only — no application code or production deployment.
>
> Base: Breeze v0.125.0, `LanternOps/breeze` forked into `flehnardt-leflit/breeze`.
> Goal: contribute reusable improvements upstream rather than maintain a long-lived divergent application.

## Background and problem

An MSP technician often documents remote support, on-site visits or phone calls hours or days after the actual work. In Breeze, the ticket's **Time & Billing → Log time** quick-add accepts a number of minutes and stamps the entry as **ending now**. This records historical work under the wrong day.

The Breeze API already accepts explicit `startedAt` and `endedAt`, so the first deliverable should be a focused web UI improvement, not a new service-management subsystem.

## Scope — milestone 1: create historical entries

- Preserve the existing Quick log (minutes ending now) and Start timer flows.
- Add a clearly labelled **Detailed entry** mode in ticket Time & Billing.
- Inputs: work date, start time, end time, optional description and existing work-type, billable, and rate inputs as permitted.
- Default date/time to a sensible current local value, but never silently override a date chosen by the technician.
- Build `startedAt` and `endedAt` as explicit instants, sending ISO-8601 timestamps through the existing `POST /time-entries` endpoint.
- Preserve ticket ID and its organization, the technician identity, ticket-linked device context, rate and coverage calculations.
- Do not introduce an independent device reference to time entries without separate design: Breeze currently links time entries primarily through tickets.
- Handle invalid or future start dates, end-before-start and zero-duration entries. Clarify whether overnight spans belong in scope before implementing.
- Correctly handle timezone and daylight-saving transitions: a local wall time may be nonexistent or ambiguous during DST changes. Avoid silent corrections.
- After success, refresh ticket summary and entries; show entry on the correct historical timesheet date.
- Responsive and accessible controls, German and English labels.

## Scope — milestone 2: edit the time of unbilled entries

- Extend the existing weekly `/timesheet` row editor with date, start and end controls.
- Use `PATCH /time-entries/:id` and preserve all server-side approval, billing and permission enforcement.
- Billed/invoiced entries must not have historical timestamps rewritten.
- If the date changes weeks, refresh and navigate sensibly; make the changed date visible and confirm it persisted.
- Shared date/time form utilities between creation and editing are preferable to duplicated logic.

## Future work, intentionally separate

- **Pause/breaks**: design how non-working minutes affect stored worked time, billable minutes, approvals and invoice calculations before coding.
- **Technician appointments/calendar**: a scheduled service slot is distinct from a ticket due date and from actual recorded time; link to customer, contact, ticket, site, optional device and technician.
- **Convert appointment into suggested time entry**, with explicit human confirmation and idempotency.
- Calendar interoperability (e.g. CalDAV) and ERPNext integration.
- Ticketless entries and organization/site attribution using existing Breeze API features where supported.

## Technical references to verify on each development branch

- `apps/web/src/components/tickets/TicketTimeBilling.tsx`: quick-add constructs the timestamps by subtracting minutes from `new Date()`.
- `apps/web/src/components/time/TimesheetPage.tsx`: edit form currently omits `startedAt`/`endedAt`.
- `packages/shared/src/validators/timeEntries.ts`: create/update schemas support explicit timestamps.
- `apps/api/src/routes/timeEntries/timeEntries.ts`: create/update endpoints.

## Acceptance tests

1. Technician creates a 60-minute entry for a historical local date; it appears on the right ticket and timesheet day, with correct duration and financial treatment.
2. Quick log and running timer behavior are unchanged.
3. Unauthorized edits are refused and cannot be forced through UI.
4. Billed entries remain locked; approved/approval-pending entries respect existing rules.
5. DST and timezone tests include a normal day, a spring-forward gap and an autumn repeated hour.
6. Existing billing-profile defaults and rate overrides keep working.
7. Tests cover mobile-width layout and keyboard operation.

## Development and deployment rules

- Develop only on feature branches based on upstream-compatible Breeze code.
- Read upstream contribution guidance and request upstream approval of a nontrivial feature plan before proposing implementation there.
- No direct writes to the production database or modifications to running agents during development.
- Test against an isolated stack with test customer data before any LEFLIT production rollout.
- Preserve AGPL-3.0 copyright/license notices and never commit secrets or customer data.
- Prefer an upstream PR for generally useful functionality. If LEFLIT-only integrations are later needed, isolate them in separately maintained code wherever possible.

## Working issues (to create when GitHub Issues is enabled)

1. feat(time tracking): allow historical work logs with editable date and time.
2. feat(timesheet): correct start/end timestamps of unbilled entries.
3. enhancement(service desk): technician appointments linked to tickets and devices.
