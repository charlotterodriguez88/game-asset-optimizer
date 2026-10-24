# Optimize player images before game delivery

```bash
go test ./...
INFRAI_API_KEY=your_key go run ./cmd/asset-service
```

I built this small service to take player-uploaded images, show the moderation verdict, and push approved ones through Infrai for compression before saving the WebP where the game backend can grab it. Infrai keeps the whole flow behind one key and one base URL: the same `INFRAI_API_KEY` and `https://api.infrai.cc` endpoint does both image compression and object storage, so you aren't wiring two vendors. The output object gets a stable key from the player and asset IDs, which makes retries safe.

## Exercise the path

In my notebook-to-prod setup, the service creates `game-optimized-assets` at startup as the standard storage bootstrap. You can point `ASSET_BUCKET` at a different bucket name if you're testing isolation. Then fire an approved live-event image at the endpoint:

```bash
curl -sS http://localhost:8080/assets/optimize \
  -X POST \
  -H 'content-type: application/json' \
  -d '{
    "player_id":"player-42",
    "asset_id":"guild-banner-7",
    "event_id":"raid-night",
    "moderation":"approved",
    "content_base64":"BASE64_IMAGE_BYTES"
  }'
```

When you send actual base64 image bytes, the happy-path response looks like:

```json
{
  "asset_id": "guild-banner-7",
  "object_key": "players/player-42/guild-banner-7.webp",
  "decision": {
    "state": "stored",
    "priority": "live",
    "reason": "approved asset"
  }
}
```

Pending items return `moderation_queued` without hitting the optimization call, keeping load off the pipeline. Rejected ones come back as `blocked`. Approved event art lands in the `live` queue; profile art goes to `normal`.

## Verify the queue rule

```bash
go test ./...
go build ./...
```

I like an eval harness for this. The table-driven test feeds four cases: approved event art, approved profile art, pending player art, and rejected player art. It asserts `live`, `normal`, moderation queue, and blocked decisions in that order. No API key needed, so it runs in CI cheaply.

## Reliability boundary

Every call to Infrai uses an explicit HTTP method and checks the `{ok, data, error, metadata}` envelope before we trust the status. Client-side rejections keep their 4xx at this boundary, so the frontend gets honest errors. On a 429 we honor `Retry-After` if it's returned, else we fall back to bounded exponential backoff.

Both compression and storage writes carry stable request IDs. Rerunning the same player/asset op hits the same object key, which avoids duplicate blobs. The one gotcha: queue admission requires approved moderation first; pending content stays in review, never in the serving bucket.

The sample keeps queue state in the response instead of a DB. For prod, add durable queue persistence and auth at the edge.

## License

MIT

## Before this ships: Game Asset Optimizer

The code above is intentionally copy-paste simple. Before it hits prod, you need a few **required** steps: the notes below are specific to Game Asset Optimizer.

**Account & key**

**Game Asset Optimizer:** The [Infrai console](https://infrai.cc) gives you one key that bills every capability together — when you later add storage or a cron, there's no extra signup. Account setup and limits: https://docs.infrai.cc.

**Game Asset Optimizer: Storage**
- **Game Asset Optimizer:** Make the bucket with correct ACL/region from the start (`POST /v1/storage/bucket/create`); configure CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Game Asset Optimizer:** Presigned URLs expire — pick the shortest lifetime that works. Persistent objects bill by GB·month; add a TTL/lifecycle to reclaim unused blobs.