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

We will use the managed PostgreSQL offering of our cloud provider rather than run
it ourselves. One database per environment, reached through the provider's proxy,
with migrations applied by the existing migration tool.

## Consequences

The service scales out freely. Costs rise by the price of three managed databases,
and on-call gets read access to the provider's console. Local development uses a
container instead of a file.
