# Configuration and storage

## Server

The daemon listens on `127.0.0.1:8787` and writes to `./data`. Put `--data-dir DIR` before the command to choose another data directory. Put `--config FILE` before `serve` to load another configuration file.

| Variable | Purpose |
| --- | --- |
| `CONVOMEOW_DATA_DIR` | Data directory for the daemon and CLI |
| `CONVOMEOW_LISTEN` | Daemon listen address |
| `CONVOMEOW_URL` | Daemon URL used by the CLI |

Run one daemon per data directory. The process lock prevents a second daemon from opening the same files. On startup, the daemon loads existing paired sessions from SQLite.

## Access

On first start, the daemon creates `data/control.token`. This key grants access to all accounts through the API and web client. Browser sign-in lasts 30 days and survives daemon restarts. Sign out to clear the browser session; changing the key invalidates existing browser sessions.

Keep the API on loopback for local use. For remote access, use HTTPS and restrict access to the server.

## Local data

`data/app.sqlite` holds accounts, conversations, group details, messages, media metadata, and send jobs. `data/whatsmeow.sqlite` holds WhatsApp sessions and synced contact names. Protect both databases, stored media, and their backups. ConvoMeow creates the data directory, control token, and local media files with owner-only permissions.

Back up the data directory, including both databases and local media. Stop the daemon before copying SQLite files, or use SQLite's backup tools while it runs. Keep backups private.

## Media storage

Media defaults to `data/media`. Copy [config.example.yaml](../config.example.yaml) to `data/config.yaml` to set limits or storage profiles. `local` stores files in the data directory by default. For AWS S3, add a profile with `driver: s3`, `bucket`, and `region`; the AWS SDK loads credentials from its standard chain. For Cloudflare R2, also set `endpoint: https://ACCOUNT_ID.r2.cloudflarestorage.com`, `region: auto`, and environment variable names for the access key and secret key. Set `active_profile` to the profile ID used for new files. Leave old profiles configured while attachments or avatars still use them; changing the active profile does not move files. Buckets must remain private.

## Limits

`max_file_bytes` caps each upload and download, `max_total_bytes` caps stored attachments, avatars, and unused uploads, `max_temp_bytes` caps concurrent staging space, and `workers` controls background downloads and sends. The total limit counts ConvoMeow's recorded files, not other objects in a bucket. A blocked download stays `remote`; its content URL returns `413` or `507` until the relevant limit allows it. This release uses one S3 upload per file, so `max_file_bytes` must stay below 5 GiB.
