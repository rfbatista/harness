# Instagram API with Instagram Login: reference

Host `https://graph.instagram.com`, version `v25.0` (override with `IG_API_VERSION`).
All examples use `bash scripts/ig.sh`, which adds the host, version and Bearer token.
Anything not covered here: https://developers.facebook.com/documentation/instagram-platform/llms.txt

## Contents
- Auth and scopes
- Profile and media
- Publishing
- Comments
- Insights
- Messaging

## Auth and scopes

- The account must be an Instagram **professional** account (Business or Creator).
- Tokens: the business-login flow issues short-lived tokens (1h). App-Dashboard and refreshed
  tokens are long-lived (60 days).
- Refresh: `GET /refresh_access_token?grant_type=ig_refresh_token&access_token=...`
  (unversioned). The token must be at least 24h old and not yet expired, and needs
  `instagram_business_basic`. Response: `{access_token, token_type: "bearer", expires_in}`.
  Use `bash scripts/ig.sh refresh-token`.
- Scopes: `instagram_business_basic`, `instagram_business_content_publish`,
  `instagram_business_manage_comments`, `instagram_business_manage_insights`,
  `instagram_business_manage_messages`. The legacy `business_*` names are deprecated.

## Profile and media

`/me` fields: `id` (app-scoped), `user_id` (the IG professional account ID, which is your `<IG_ID>`),
`username`, `name`, `account_type`, `profile_picture_url`, `followers_count`, `follows_count`,
`media_count`.

IG User edges: `media`, `stories`, `tags` (media you're tagged in), `mentioned_media`,
`content_publishing_limit`, `live_media`.

```bash
bash scripts/ig.sh GET "/<IG_ID>/media?fields=id,caption,media_type,media_product_type,permalink,timestamp,like_count,comments_count&limit=25"
bash scripts/ig.sh GET "/<IG_ID>/stories?fields=id,media_type,permalink,timestamp"
```

Pagination: follow `paging.cursors.after` with `&after=<cursor>`.

## Publishing

There are always three steps: **create container → wait for `FINISHED` → `media_publish`**.

```bash
# Image post
bash scripts/ig.sh POST "/<IG_ID>/media" '{"image_url":"https://example.com/a.jpg","caption":"Hello","alt_text":"A red bike"}'
# Video in feed
bash scripts/ig.sh POST "/<IG_ID>/media" '{"media_type":"VIDEO","video_url":"https://example.com/v.mp4","caption":"..."}'
# Reel
bash scripts/ig.sh POST "/<IG_ID>/media" '{"media_type":"REELS","video_url":"https://example.com/r.mp4","caption":"..."}'
# Story (image_url or video_url)
bash scripts/ig.sh POST "/<IG_ID>/media" '{"media_type":"STORIES","image_url":"https://example.com/s.jpg"}'

# -> {"id":"<IG_CONTAINER_ID>"}
bash scripts/ig.sh wait-container <IG_CONTAINER_ID>
bash scripts/ig.sh POST "/<IG_ID>/media_publish" '{"creation_id":"<IG_CONTAINER_ID>"}'
# -> {"id":"<IG_MEDIA_ID>"}
```

Carousel (2–10 items, which can mix images and videos):

```bash
# 1. one child container per item
bash scripts/ig.sh POST "/<IG_ID>/media" '{"image_url":"https://example.com/1.jpg","is_carousel_item":true}'
# (video child: {"media_type":"VIDEO","video_url":"...","is_carousel_item":true})
# 2. parent container
bash scripts/ig.sh POST "/<IG_ID>/media" '{"media_type":"CAROUSEL","caption":"...","children":"<C1>,<C2>,<C3>"}'
# 3. wait on the parent, then media_publish with the parent id
```

Optional container params: `caption`, `alt_text` (images only, not Reels or Stories), `user_tags`,
`location_id`, `is_ai_generated`.

`status_code` values: `IN_PROGRESS`, `FINISHED` (ready), `PUBLISHED`, `ERROR`, `EXPIRED`
(not published within 24h). Poll at most once a minute, for up to 5 minutes.

Quota: `GET /<IG_ID>/content_publishing_limit?fields=quota_usage,config` covers 100 posts per
rolling 24h.

Media requirements: JPEG only for images, served from a public URL at publish time. Carousel
items are cropped to the first item's aspect ratio (1:1 by default).

## Comments

```bash
bash scripts/ig.sh GET    "/<IG_MEDIA_ID>/comments?fields=id,text,username,timestamp,like_count,hidden"
bash scripts/ig.sh GET    "/<IG_COMMENT_ID>/replies?fields=id,text,username,timestamp"
bash scripts/ig.sh POST   "/<IG_COMMENT_ID>/replies" '{"message":"Thanks!"}'
bash scripts/ig.sh POST   "/<IG_COMMENT_ID>" '{"hide":true}'          # false to unhide
bash scripts/ig.sh DELETE "/<IG_COMMENT_ID>"
bash scripts/ig.sh POST   "/<IG_MEDIA_ID>" '{"comment_enabled":false}' # true to re-enable
```

For high volume, Meta recommends webhooks over polling to avoid rate limits.

## Insights

Account: `GET /<IG_ID>/insights?metric=...&period=day&metric_type=total_value[&since=&until=][&breakdown=...]`

- Metrics that need `metric_type=total_value`: `views`, `reach`, `accounts_engaged`,
  `total_interactions`, `likes`, `comments`, `shares`, `saves`, `replies`, `reposts`,
  `profile_links_taps`, `follows_and_unfollows`, `follower_demographics`,
  `engaged_audience_demographics`.
- Also available: `follower_count` and `online_followers` (both need 100+ followers;
  `online_followers` covers the last 30 days).
- Breakdowns: `media_product_type` (AD, STORY, REEL, CAROUSEL_CONTAINER, POST),
  `follow_type` (FOLLOWER, NON_FOLLOWER, UNKNOWN), `contact_button_type`. Demographics take
  `breakdown=age|city|country|gender`.
- Demographics use `period=lifetime` plus `timeframe=last_14_days|last_30_days|last_90_days|prev_month|this_month|this_week`.
- `impressions` is deprecated. Account data is kept for 90 days.

```bash
bash scripts/ig.sh GET "/<IG_ID>/insights?metric=reach,views,accounts_engaged&period=day&metric_type=total_value&since=<unix>&until=<unix>"
bash scripts/ig.sh GET "/<IG_ID>/insights?metric=follower_demographics&period=lifetime&metric_type=total_value&timeframe=last_30_days&breakdown=country"
```

Media: `GET /<IG_MEDIA_ID>/insights?metric=...`

| Type | Metrics |
|---|---|
| Common | `views`, `reach`, `likes`, `comments`, `shares`, `saved`, `total_interactions`, `reposts` |
| FEED | `profile_activity` (breakdown `action_type`), `profile_visits`, `follows` |
| REELS | `ig_reels_avg_watch_time`, `ig_reels_video_view_total_time`, `reels_skip_rate` |
| STORY | `navigation` (breakdown `story_navigation_action_type`), `replies`, `follows`, `profile_visits`, `link_clicks` |

Media insights are kept for up to 2 years, but story insights only exist for 24h. Insights are
not available for individual items inside a carousel. Missing data comes back as an empty `data`
array, not `0`. Data can lag up to 48h.

## Messaging

- You can only reply after the user messages you, and only **within 24h** of their last message.
  A `"tag":"HUMAN_AGENT"` on the message extends this when a human is answering.
- Recipient IDs are Instagram-scoped IDs (IGSIDs), taken from conversations or webhooks.

```bash
bash scripts/ig.sh GET  "/me/conversations?platform=instagram&fields=id,updated_time,participants"
bash scripts/ig.sh GET  "/<CONVERSATION_ID>?fields=messages{id,from,to,message,created_time}"
bash scripts/ig.sh POST "/<IG_ID>/messages" '{"recipient":{"id":"<IGSID>"},"message":{"text":"Hi!"}}'
# image: "message":{"attachment":{"type":"image","payload":{"url":"https://..."}}}
```

Supported message types: text, links, images, audio, video, PDF, stickers, reactions, and posts
the account owns.
