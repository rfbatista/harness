---
title: Optimize RLS Policies for Performance
impact: HIGH
impactDescription: 5-10x faster RLS queries with proper patterns
tags: rls, performance, security, optimization
---

## Optimize RLS Policies for Performance

Poorly written RLS policies can cause severe performance issues. Use subqueries and indexes strategically.

**Incorrect (function called for every row):**

```sql
create policy orders_policy on orders
  using (current_setting('app.current_user_id')::bigint = user_id);  -- evaluated per row!

-- With 1M rows, current_setting() is re-evaluated 1M times
```

**Correct (wrap functions in SELECT):**

```sql
create policy orders_policy on orders
  using ((select current_setting('app.current_user_id')::bigint) = user_id);  -- evaluated once, cached

-- 100x+ faster on large tables
```

Use security definer functions for complex checks:

`SECURITY DEFINER` functions run with the creator's privileges and bypass RLS on any tables they touch — which is what makes them useful for internal lookups, but also what makes them dangerous if misused. Always include an explicit identity check inside the function body, keep them in a non-exposed schema, and revoke `EXECUTE` from any role that shouldn't call them directly.

```sql
-- Create helper function in a private schema
create or replace function private.is_team_member(team_id bigint)
returns boolean
language sql
security definer
set search_path = ''
as $$
  select exists (
    select 1 from public.team_members
    -- always check the calling user's identity inside the function
    where team_id = $1
      and user_id = (select current_setting('app.current_user_id')::bigint)
  );
$$;

-- Revoke direct execution from untrusted roles
revoke execute on function private.is_team_member(bigint) from public, application_user;

-- Use in policy (indexed lookup, not per-row check)
create policy team_orders_policy on orders
  using ((select private.is_team_member(team_id)));
```

Always add indexes on columns used in RLS policies:

```sql
create index orders_user_id_idx on orders (user_id);
```

Reference: [Row Security Policies](https://www.postgresql.org/docs/current/ddl-rowsecurity.html)
