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
./convomeow chat messages sales '<chat-id>'
./convomeow message list sales
```

The daemon listens on `127.0.0.1:8787` and writes to `./data`. Put `--data-dir DIR` before the command to change its data directory. You can set these variables:

| Variable | Purpose |
| --- | --- |
| `CONVOMEOW_DATA_DIR` | Data directory for the daemon and CLI |
| `CONVOMEOW_LISTEN` | Daemon listen address |
| `CONVOMEOW_URL` | Daemon URL used by the CLI |

The CLI displays a QR code during pairing. Scan it from WhatsApp's **Linked devices** screen. The daemon loads paired sessions from SQLite when it starts. Run one daemon per data directory; the process lock prevents a second daemon from opening the same files.

## Native API

The CLI talks to ConvoMeow through the `/api/v1` routes below. I chose their URLs and JSON format for this project. Existing Evolution API clients cannot use them. I plan to add Evolution's WhatsApp routes as another HTTP layer over the same application code, using its [v2 API specification](https://github.com/evolution-foundation/docs-evolution/blob/main/openapi/openapi-v2.json).

On first start, the daemon creates `data/control.token`. Send its value as `Authorization: Bearer <token>` on every `/api/v1` request. The token grants access to all accounts. Keep the API on loopback, or place it behind HTTPS and access controls if you change the listen address.

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/healthz` | Process health; no token required |
| `POST`, `GET` | `/api/v1/accounts` | Create and list accounts |
| `GET` | `/api/v1/accounts/{id}` | Account connection state |
| `POST`, `GET` | `/api/v1/accounts/{id}/login` | Start pairing and poll QR or connection state |
| `POST`, `GET` | `/api/v1/accounts/{id}/messages` | Send text and read saved messages |
| `GET` | `/api/v1/accounts/{id}/chats` | List chats with saved messages |
| `GET` | `/api/v1/accounts/{id}/chats/{chat_id}/messages` | Read saved messages in one chat |

Create an account with `{"label":"sales"}`. Send text with `{"to":"+15551234567","text":"Hello"}`. Read messages with `?after=0&limit=100`; use the last returned `id` as the next cursor. API paths use account UUIDs, while the CLI accepts account labels.

Chat and per-chat message lists return the newest items first. Use `?before=0&limit=100` for the first page. For more chats, pass the last chat's `last_message.id` as `before`. For older messages in one chat, pass the last message's `id`.

URL-encode the chat ID in the path. A chat appears in the list after ConvoMeow saves its first message. To reply, send text to that chat ID through the existing message endpoint.

If a send returns `outcome_unknown`, check the chat before retrying. WhatsApp may have received the message.

## Data and limits

`data/app.sqlite` holds accounts and saved messages. `data/whatsmeow.sqlite` holds WhatsApp sessions. Stop the daemon before backing up both files. ConvoMeow creates the data directory and control token with owner-only permissions.

The connector saves messages it receives after startup. For images, videos, audio, documents, stickers, locations, and contacts, it stores the type and any caption. It does not download attachments or import older chats.

I plan to add the web inbox, team roles, Evolution API routes, automation, and Telegram.

I have not tested pairing, messaging, or reconnection with a WhatsApp number yet. If pairing stops after whatsmeow saves a device but before ConvoMeow records its identity, unlink that device in WhatsApp and pair again.

The connector uses an unofficial WhatsApp client. Review [WhatsApp's terms](https://www.whatsapp.com/legal/terms-of-service) before using an account.

## Code layout

- `cmd/convomeow`: CLI entry point and daemon lifecycle
- `internal/core`: account and message types, plus the connector contract
- `internal/app`: account sessions, pairing, and message handling
- `internal/providers/whatsapp`: whatsmeow adapter
- `internal/store/sqlite`: application database and migrations
- `internal/api/native`: authenticated HTTP API
- `internal/cli`: API client and QR display

## Development

I use AI tools to help write ConvoMeow.

Use `make fmt` to format Go files and `make fmt-check` to check them. Run `go build ./...` and `go vet ./...` before contributing a change.

I license ConvoMeow's code under [Apache-2.0](LICENSE). Whatsmeow remains a separate MPL-2.0 dependency.
