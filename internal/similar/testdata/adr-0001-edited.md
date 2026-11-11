# 1. Store orders in PostgreSQL

## Status

Accepted

## Context

The order service keeps its data in a single SQLite file on the host it runs on.
That worked while the service ran as one instance, but we now need to run at least
three instances behind the load balancer, and they cannot share the file safely.
Orders are relational: an order has lines, each line names a product, and refunds
refer back to both. Reports join across all of these every night.

## Decision

We will move the order service to PostgreSQL, managed by the platform team's
operator. Each environment gets its own database, and the service connects through
the connection pooler the operator provides. Schema changes are applied with the
migration tool the billing service already uses.

Backups are taken by the operator every hour and kept for thirty days.

## Consequences

The service can run as many instances as it needs. We take on a dependency on the
platform team's operator, and on-call needs access to the database dashboards.
Local development uses a container instead of a file.
