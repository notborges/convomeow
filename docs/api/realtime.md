# Live updates

Connect to `/api/v1/events` with WebSocket. Browser clients use the existing session cookie and must connect from the same origin. API clients can send the normal bearer authorization header. Credentials do not belong in the URL.

The server subscribes the connection before sending:

```json
{"type":"ready"}
```

On every `ready` frame, refresh active HTTP queries and mark inactive cached data stale. This covers changes missed while disconnected. Notifications are not persisted or replayed.

## Notifications

| Type | Fields | Refresh |
| --- | --- | --- |
| `accounts.changed` | `account_id` | Account state, login attempts and account contacts |
| `conversations.changed` | `account_id`, optional `conversation_id` | Conversation list and affected conversation/messages |
| `attachment.changed` | `account_id`, `attachment_id` | Attachment metadata |
| `contacts.changed` | `account_id` | Contacts and conversation names |
| `avatars.changed` | `account_id` | Account, contact and conversation pictures |

An omitted `conversation_id` means a change may affect any conversation in the account, such as a history import or alias merge. Refresh loaded queries for that account; fetching unseen history is unnecessary.

Notifications follow saved resource changes or completed account/pairing state changes. They contain ConvoMeow IDs, not provider payloads, message contents, QR values or media bytes. For example:

```json
{"type":"attachment.changed","account_id":"account-id","attachment_id":"attachment-id"}
```

Receipt changes emit `conversations.changed`. Refresh message pages and any open message receipt details. Receipt notifications do not send read receipts back to the provider.

Coalesce notifications before fetching. If a notification arrives during an older HTTP request, fetch again after that request finishes. Use the HTTP responses for message ordering and pagination.

## Chat presence

To receive WhatsApp typing and recording activity, connect to `/api/v1/events?presence_account_id=<account-id>`. This opts the selected account into online presence for the lifetime of the connection. Multiple connections share presence: the last disconnect marks the account unavailable. The server restores online presence after provider reconnects while viewers remain. Without the parameter, the event stream does not change account presence.

The web client connects while its tab is visible and selects the account shown in the inbox. Hiding the tab closes the stream; returning reconnects and refreshes saved data. WhatsApp's presence privacy settings still apply.

Presence events carry temporary state, not cache invalidations:

```json
{"type":"presence.changed","account_id":"account-id","conversation_id":"conversation-id","presence":{"participant_id":"provider-contact-id","display_name":"Maya","activity":"typing","ttl_ms":10000}}
```

Track participants separately within a conversation. `activity` is `typing`, `recording`, or `paused`. A paused event removes that participant; active events expire after `ttl_ms` from receipt. Clear presence on disconnect, `ready`, and `accounts.changed` for the affected account. Do not fetch message lists in response to presence. The server neither saves nor replays it, and ignores activity for unknown conversations.

Send activity through `POST /api/v1/conversations/{id}/presence` with `{"client_id":"composer-instance-id","activity":"typing"}`. Use a unique `client_id` per composer instance and reuse it for refreshes and pause commands. The server combines active composers, expires leases after ten seconds, and limits unchanged activity refreshes to once per three seconds.

The web composer sends at most one typing refresh every three seconds and sends paused after four seconds of inactivity, on blur, on send, or on leaving the conversation. It does not advertise recording because it has no audio recorder; the API supports recording activity for other clients.

## Connection lifecycle

The connection only sends notifications; commands and file transfers use HTTP. Client application messages close the connection. The server sends a ping every 20 seconds, with a five-second response deadline. Browser authorization is rechecked before each notification and heartbeat.

Each connection has a bounded queue. Queue overflow closes the connection with code `1013`; reconnect and refresh after `ready`. Service shutdown closes the stream. Reconnect with bounded backoff and jitter after network failures. Stop reconnecting when the browser session is no longer valid, and close the stream on sign-out.

Reaction changes use `conversations.changed`; read message summaries or the paginated reactions endpoint for current state. Reactions do not create timeline messages or advance their timestamps.

Edits and deletions also use `conversations.changed`. Refresh messages, revision history and conversation previews. Saved content remains available after provider deletion.
