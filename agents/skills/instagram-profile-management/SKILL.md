---
name: instagram-profile-management
description: Use when asked to publish posts, reels, stories or carousels to Instagram, read or moderate comments, check account or post insights, reply to Instagram DMs, or inspect an Instagram professional (business/creator) profile through the Instagram Graph API.
---

# Instagram Profile Management

Manage an Instagram professional account through the **Instagram API with Instagram Login**
(host `graph.instagram.com`, API `v25.0`, no Facebook Page required). Every call goes through
`scripts/ig.sh`, which reads the token from the environment and sends it as a Bearer header.
Paths here are relative to this skill's directory: run it as `bash <skill-dir>/scripts/ig.sh`.

## Token protocol

The user owns the token. You ask for it; you never store it.

1. Before the first API call, check: `[ -n "$IG_ACCESS_TOKEN" ] && echo set || echo missing`.
   Never print the value.
2. If missing, stop and ask the user to run `! export IG_ACCESS_TOKEN=<their token>` in the
   prompt. Do not ask them to paste it into chat. Do not write it to any file, `.env`, commit,
   or literal command argument.
3. Resolve the account once: `bash scripts/ig.sh GET "/me?fields=user_id,username,account_type"`.
   Use `user_id` as `<IG_ID>` for the rest of the session.
4. Error code `190` means the token is expired or invalid: ask for a new one. Long-lived tokens
   last 60 days; if it is still valid and older than 24h, offer `bash scripts/ig.sh refresh-token`
   and let the user re-export the result.
5. Error `10`/`200` (permission): name the missing scope from the table below.

## Writes are outward-facing

Publishing, replying, hiding/deleting comments, toggling comments and sending DMs are public
and hard to undo. Before each one, show the user the exact payload (caption, media URLs,
comment text, recipient) and get an explicit yes. Reads need no confirmation.

## Quick reference

| Task | Call | Scope (`instagram_business_*`) |
|---|---|---|
| Profile | `GET /me?fields=user_id,username,followers_count,media_count` | `basic` |
| List posts | `GET /<IG_ID>/media?fields=id,caption,media_type,permalink,timestamp` | `basic` |
| Publish | `POST /<IG_ID>/media` → `wait-container` → `POST /<IG_ID>/media_publish` | `content_publish` |
| Quota | `GET /<IG_ID>/content_publishing_limit` | `content_publish` |
| Comments | `GET /<MEDIA_ID>/comments`, `POST /<COMMENT_ID>/replies` | `manage_comments` |
| Hide / delete | `POST /<COMMENT_ID>` `{"hide":true}`, `DELETE /<COMMENT_ID>` | `manage_comments` |
| Insights | `GET /<IG_ID>/insights`, `GET /<MEDIA_ID>/insights` | `manage_insights` |
| DM | `POST /<IG_ID>/messages` | `manage_messages` |

Full parameters, metric names and examples: `reference.md` in this directory.

## Common mistakes

- Media must already be at a **public HTTPS URL**. Local files won't work.
- Images must be **JPEG**. A carousel holds at most 10 items, and every item is cropped to the
  first item's aspect ratio.
- Calling `media_publish` before the container's `status_code` is `FINISHED`. Containers
  expire after 24h.
- Hitting the limit of **100 published posts per rolling 24h** (a carousel counts as one).
  Check the quota before a batch.
- Requesting `impressions`, which is deprecated. Use `views`. Account metrics also need
  `metric_type=total_value`.
- Reading an empty insights `data` as zero. It means no data. Metrics can lag up to 48h.
  Story insights only exist for 24h.
- Sending a DM outside the **24h window** after the user's last message.
