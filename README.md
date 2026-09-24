# ConvoMeow

ConvoMeow runs WhatsApp accounts on one server through [whatsmeow](https://github.com/tulir/whatsmeow). Its CLI uses a local HTTP API to manage accounts, send text, and read saved messages.

## Build and run

You need Go 1.27 or newer and a C compiler for SQLite (`CGO_ENABLED=1`). Pairing requires a WhatsApp number.

```sh
go build -o convomeow ./cmd/convomeow
./convomeow serve
```

In another terminal:

```sh
./convomeow account add sales
./convomeow account login sales
./convomeow account list
./convomeow message send sales +15551234567 'Hello'
./convomeow chat list sales
./convomeow chat messages sales '<conversation-id>'
./convomeow message list sales
```

The daemon listens on `127.0.0.1:8787` and writes to `./data`. Put `--data-dir DIR` before the command to change its data directory. Put `--config FILE` before `serve` to load a different configuration file. You can set these variables:

| Variable | Purpose |
| --- | --- |
| `CONVOMEOW_DATA_DIR` | Data directory for the daemon and CLI |
| `CONVOMEOW_LISTEN` | Daemon listen address |
| `CONVOMEOW_URL` | Daemon URL used by the CLI |

The CLI displays a QR code during pairing. Scan it from WhatsApp's **Linked devices** screen. The daemon loads paired sessions from SQLite when it starts. Run one daemon per data directory; the process lock prevents a second daemon from opening the same files.

## Native API

The CLI uses ConvoMeow's native API. The [OpenAPI file](docs/api/openapi-v1.yaml) defines the current routes and payloads.

On first start, the daemon creates `data/control.token`. Send its value as `Authorization: Bearer <token>` on every `/api/v1` request. The token grants access to all accounts. Keep the API on loopback, or place it behind HTTPS and access controls if you change the listen address.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz`, `/api/versions` | Process health and API versions; no token required |
| `POST`, `GET` | `/api/v1/accounts` | Create and list accounts |
| `GET` | `/api/v1/accounts/{id}` | Account connection state |
| `POST` | `/api/v1/accounts/{id}/login-attempts` | Start pairing |
| `GET` | `/api/v1/accounts/{id}/login-attempts/{attempt_id}` | Read the QR challenge or pairing state |
| `POST` | `/api/v1/accounts/{id}/conversations` | Find or create a conversation for a phone number |
| `GET` | `/api/v1/conversations`, `/api/v1/conversations/{id}` | List and read conversations |
| `GET`, `POST` | `/api/v1/conversations/{id}/messages` | Read a thread or send text |
| `GET` | `/api/v1/messages`, `/api/v1/messages/{id}` | Read an account's messages or one message |
| `GET` | `/api/v1/attachments/{id}` | Read attachment metadata and availability |
| `GET`, `HEAD` | `/api/v1/attachments/{id}/content` | Download stored media or request a remote file |

Create an account with `{"label":"sales","provider":"whatsapp","connection_kind":"linked_device"}`. To message a new number, create a conversation with `{"target":{"type":"phone_number","value":"+15551234567"}}`. Send to its ID with `{"kind":"text","content":{"text":"Hello"}}` and an `Idempotency-Key` header. Reuse the same key if you need to retry that request.

List responses contain `items` and, when more records exist, `next_cursor`. Pass that value as `?cursor=...` to load older records. Messages use WhatsApp timestamps, so imported history appears below newer messages even when it arrives later. Use `?account_id=...` to limit the conversation list to one account; the account message list requires it. The CLI accepts account labels and displays conversation IDs.

ConvoMeow assigns conversation and message IDs. WhatsApp chat IDs appear as read-only `provider_chat_id` values; API paths use ConvoMeow IDs.

The API saves an outgoing message before asking WhatsApp to send it. A failed request may leave its state as `outcome_unknown`; check that message before sending again. The CLI prints a retry key when a send request fails.

## Data and limits

`data/app.sqlite` holds accounts, messages, and private media download references. `data/whatsmeow.sqlite` holds WhatsApp sessions. Protect both databases, stored media, and their backups. ConvoMeow creates the data directory, control token, and local media files with owner-only permissions.

ConvoMeow saves live messages and any chat history WhatsApp supplies after pairing or reconnecting. It attempts to download new images, videos, audio, documents, and stickers in the background. Requesting a file from imported history starts its download. A queued file returns `202 Accepted` with `Retry-After`; poll the same URL until it returns the file. The content route supports one `Range: bytes=...` request. `HEAD` checks a stored file without starting a download. Locations and contacts include only their type.

Media defaults to `data/media`. Copy [config.example.yaml](config.example.yaml) to `data/config.yaml` to set limits or storage profiles. `local` stores files in the data directory by default. For AWS S3, add a profile with `driver: s3`, `bucket`, and `region`; the AWS SDK loads credentials from its standard chain. For Cloudflare R2, also set `endpoint: https://ACCOUNT_ID.r2.cloudflarestorage.com`, `region: auto`, and environment variable names for the access key and secret key. Set `active_profile` to the profile ID used for new files. Leave old profiles configured while attachments still use them; changing the active profile does not move files. Buckets must remain private.

`max_file_bytes` caps each download, `max_total_bytes` caps recorded files, `max_temp_bytes` caps concurrent staging space, and `workers` controls background downloads. The total limit counts ConvoMeow's recorded files, not other objects in a bucket. A blocked download stays `remote`; its content URL returns `413` or `507` until the relevant limit allows it. This release uses one S3 upload per file, so `max_file_bytes` must stay below 5 GiB. Media sending is not available yet.

The connector uses an unofficial WhatsApp client. Review [WhatsApp's terms](https://www.whatsapp.com/legal/terms-of-service) before using an account.

## Code layout

- `cmd/convomeow`: CLI entry point and daemon lifecycle
- `internal/core`: account and message types, plus the connector contract
- `internal/app`: account sessions, pairing, and message handling
- `internal/providers/whatsapp`: whatsmeow adapter
- `internal/store/sqlite`: application database and schema
- `internal/media`: local and S3-compatible file storage
- `internal/api/native/v1`: authenticated HTTP API and v1 response types
- `internal/cli`: API client and QR display

## Development

AI tools assist with code and documentation.

See [CONTRIBUTING.md](CONTRIBUTING.md) for local checks and pull request guidance.

ConvoMeow's code is licensed under [Apache-2.0](LICENSE). Whatsmeow remains a separate MPL-2.0 dependency.
