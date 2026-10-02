# Changelog

## [2.0.0] (2026-06-24)

### Changed

* Refocused the skill on vanilla Postgres: removed platform-specific
  branding, documentation links, auth helpers, and roles.
* Rewrote Row-Level Security examples to use standard Postgres patterns
  (`current_setting()` and explicit database roles) instead of
  platform-managed auth functions.
* Repointed all reference links to official Postgres documentation.

## [1.x]

### Features

* Add schema-constraints reference for safe migration patterns.
* Cover `SECURITY DEFINER`, role checks, and broken-object-level-authorization
  in the security checklist.

### Bug Fixes

* Correct broken reference links in the Postgres best-practices skill.
