---
name: database-normalization
description: Normalize (or deliberately denormalize) relational schemas — functional dependencies, 1NF through BCNF with worked examples, update/insert/delete anomalies, and when to break normal form on purpose for read performance. Use when designing a new schema, reviewing one for redundancy/anomaly risk, or deciding whether a denormalized/derived table is justified.
---

# Database Normalization

Organize relational schemas so each fact is stored in exactly one place, eliminating the
update/insert/delete anomalies that come from storing the same fact in multiple rows —
while knowing when to deliberately trade that off for read performance.

## When to Use

- Designing a new schema and deciding how to split data across tables
- Reviewing an existing schema for redundancy, update anomalies, or NULL-heavy tables
- Deciding whether a proposed denormalized/derived/cached table is justified, or is just
  redundancy nobody thought through
- Triggers: normalize, normal form, 1NF, 2NF, 3NF, BCNF, functional dependency, schema
  redundancy, update anomaly

## Functional Dependencies

Everything below reduces to one idea: attribute `Y` is **functionally dependent** on
attribute `X` (written `X → Y`) if, for every value of `X`, there's exactly one value of
`Y`. Normalization is the process of arranging tables so that every non-key column depends
on **the whole key, nothing but the key, and so help you Codd**.

## The Anomalies Normalization Prevents

An unnormalized table mixing facts about different things causes three problems:
- **Update anomaly** — the same fact is stored in multiple rows; update one and miss
  another, and the data contradicts itself
- **Insertion anomaly** — can't record a fact (e.g. a new product) until an unrelated fact
  exists (e.g. an order for it), because they're jammed into one table
- **Deletion anomaly** — deleting one fact (the last order for a customer) accidentally
  deletes another (the customer's contact info), because there was nowhere else it lived

## Normal Forms, Worked Example

Start from one denormalized table and fix one problem per step:

```
Orders(order_id, customer_id, customer_name, customer_email,
       product_id, product_name, product_price, qty, order_date)
```

### 1NF — Atomic values, no repeating groups

Every column holds a single, atomic value — no comma-separated lists, no repeated column
groups (`product_id_1, product_id_2, ...`). If an order could hold multiple products in one
row as a list, that's a 1NF violation: split it into one row per (order, product) line
item instead.

### 2NF — No partial dependency on a composite key

Applies when the key is composite. If `Orders`' key is `(order_id, product_id)`, then
`customer_name` and `customer_email` depend only on `order_id` (via `customer_id`), not on
the full key — that's a **partial dependency**. Split it out:

```
Orders(order_id, customer_id, order_date)
OrderLines(order_id, product_id, product_name, product_price, qty)
```

### 3NF — No transitive dependency

`product_name` and `product_price` depend on `product_id`, not directly on the
`(order_id, product_id)` key — that's a **transitive dependency** (`key → product_id →
product_name`). Split it out again:

```
Orders(order_id, customer_id, order_date)
OrderLines(order_id, product_id, qty, unit_price_at_sale)
Products(product_id, product_name, product_price)
Customers(customer_id, customer_name, customer_email)
```

Note `unit_price_at_sale` staying on `OrderLines`: that's not a normalization violation —
it's a genuinely different fact (what the customer paid at purchase time) from
`Products.product_price` (the catalog's current price). Don't conflate "looks redundant"
with "is redundant" — check whether the two columns can independently be true.

### BCNF — Every determinant is a candidate key

Stricter than 3NF: for every functional dependency `X → Y` in a table, `X` must be a
candidate key (a superkey), not just something non-key columns happen to depend on. 3NF
allows a narrow exception BCNF doesn't; BCNF violations mostly show up when a table has
multiple overlapping composite candidate keys. Rare in practice, but worth checking on
tables with more than one plausible unique constraint.

### 4NF / 5NF — Multivalued and join dependencies

Beyond BCNF, and rarely reached in practice: **4NF** removes independent multivalued
dependencies (e.g. a table pairing "employee → skill" and "employee → language" in one
table when skills and languages are unrelated to each other — split into two tables).
**5NF** ensures a table can't be losslessly split into smaller tables that, when
rejoined, would produce spurious rows. Reach for these only when a real anomaly shows up
at that level — most schemas never need to go past 3NF/BCNF.

## Denormalization: Breaking This on Purpose

Normalization optimizes for write correctness and storage efficiency, not read speed.
Deliberately denormalizing is legitimate when:

- **Read-heavy reporting/analytics** — a star schema (fact table + denormalized dimension
  tables) trades write complexity and redundancy for query simplicity and join-free
  aggregation
- **Derived/cached columns** — storing a computed value (`order_total`, `follower_count`)
  to avoid recomputing it on every read, as long as there's a clear invalidation/recompute
  path when the source data changes
- **Hot-path performance** — a specific, measured query is join-bound and a targeted
  denormalized table/materialized view fixes it

The rule: **normalize by default, denormalize with a name and a reason** — a specific
query pattern and measured cost, not "joins feel slow." An undocumented denormalized
column is a future update anomaly waiting to happen.

## Checklist

1. Identify the functional dependencies actually present, not assumed
2. Walk 1NF → 2NF → 3NF, splitting on partial and transitive dependencies
3. Check BCNF on any table with more than one candidate key
4. For anything that still looks redundant, verify it's actually the same fact (not two
   independently-true facts that happen to look similar, like a price-at-purchase vs. a
   catalog price)
5. If proposing a denormalized table/column, name the query it serves and how it stays
   in sync with the source of truth

## Notes

- This is schema-design guidance, independent of any specific database or language —
  pairs with `postgres-best-practices` (Postgres-specific schema/index/performance rules)
  once the normalized shape is decided.
- `golang-database` explicitly declines to design or modify schemas — this skill is what
  fills that gap before any Go (or other) data-access code gets written.

---

### Further Reading

- [Database Normalization: 1NF, 2NF, 3NF & BCNF Examples](https://www.digitalocean.com/community/tutorials/database-normalization)
- [Database Normalization – Normal Forms Table Examples](https://www.freecodecamp.org/news/database-normalization-1nf-2nf-3nf-table-examples/)
