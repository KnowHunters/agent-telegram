# Telegram gateway

The `gateway` command runs one Telegram client behind HTTP and WebSocket.
It is an alias of `serve-api` that enables the SQLite journal by default.
Existing CLI commands remain available. Do not run a separate `serve` daemon
and a gateway against the same session at the same time.

## Windows quick start

Build from this fork:

```powershell
go build -o agent-telegram-gateway.exe .
```

Sign in from a foreground terminal and open the printed localhost URL in your
browser. Scan with the Telegram account you intend to automate:

```powershell
.\agent-telegram-gateway.exe auth --session-provider dpapi --reload-server=false
```

`dpapi` saves the authorization encrypted for the current Windows user under
`%USERPROFILE%\.agent-telegram\sessions\<profile>.dpapi`. It is the Windows
default for new configurations. Existing configurations may still select
`memory`; use the explicit provider above to migrate by signing in once.
Use `--profile work` consistently to select another profile. Logout clears
that profile's authorization. Encryption does not make the session portable
to a different Windows user or computer. macOS retains the existing Keychain
provider; other builds retain the memory provider.

## Remote QR login

The auth page can listen beyond loopback when a browser must reach it from
another machine. Pass both the bind address and the public base URL. The
command creates a random one-time token and appends it to the printed link.

Stop the gateway for the selected profile before starting auth. This prevents
two Telegram clients from using the same account session at once.

```bash
agent-telegram auth \
  --listen 0.0.0.0 \
  --port 18082 \
  --public-url http://203.0.113.10:18082 \
  --session-provider file \
  --reload-server=false
```

Open the printed URL on the device with Telegram. The login page exits and
closes its listener after the QR flow finishes, fails, or times out. A
non-loopback listener requires `--public-url`; the URL may use HTTP or HTTPS
and must not contain credentials, a query, or a fragment. Prefer HTTPS when
the browser can reach a trusted certificate. When using bare HTTP, restrict
the port to the operator's address with a firewall and remove that rule after
login. Never expose the auth port while no login is running.

After the command writes its JSON result, start the gateway and check
authenticated readiness:

```bash
docker compose up -d
curl http://127.0.0.1:18080/health
curl -H "Authorization: Bearer $AGENT_TELEGRAM_API_SECRET" \
  -X POST http://127.0.0.1:18080/rpc/status
```

The file provider writes the session below `/data/.agent-telegram/sessions`
in the container volume with mode `0600`. Do not copy a session between
installations that use different Telegram API credentials. Sign in again
when the stored API ID/hash changes.

Start the gateway in a foreground terminal:

```powershell
$env:AGENT_TELEGRAM_API_SECRET = "replace-with-a-long-random-token"
.\agent-telegram-gateway.exe gateway --session-provider dpapi --listen 127.0.0.1 --port 8080
```

`--gateway-db <path>` overrides the default
`%USERPROFILE%\.agent-telegram\gateway.db`. Supplying this option to
`serve-api` also enables the gateway. Plain `serve-api` preserves the old HTTP
behavior. A gateway always requires a token, even with `--unsafe-no-auth`.
Keep the foreground process running. Ctrl+C disconnects without logging out.
Before switching accounts, stop the gateway, authenticate the new account,
and start it again. Session reload over the gateway is deliberately disabled.

## HTTP

The existing operations remain at `POST /rpc/{method}`. Discover method names
and parameter schemas using authenticated `GET /methods` and `GET /manifest`.
`GET /health` is public liveness; authenticated `GET /ready` returns 200 when
the Telegram account is ready and 503 otherwise.

```powershell
$headers = @{ Authorization = "Bearer $env:AGENT_TELEGRAM_API_SECRET" }
Invoke-RestMethod http://127.0.0.1:8080/rpc/status -Method Post -Headers $headers -ContentType application/json -Body '{}'
```

Writes require an `X-Request-Id` header. Generate a new ID for a new action;
reuse the ID and identical parameters when recovering its result:

```powershell
$headers['X-Request-Id'] = [guid]::NewGuid().ToString()
$body = @{ peer = '@SomeBot'; message = '/start' } | ConvertTo-Json
Invoke-RestMethod http://127.0.0.1:8080/rpc/send_message -Method Post -Headers $headers -ContentType application/json -Body $body
```

Gateway writes accept JSON only; they preserve existing file-root restrictions.
Use existing approved local-file parameters for attachments. The legacy
multipart upload API remains available in plain `serve-api` mode.
Destructive/paid methods retain the existing policy and explicit
`X-Confirm-Action: true` requirement. A token authorizes the gateway, while
the existing `policy.json` controls operation classes and peers.

## WebSocket protocol (schema 1)

Connect to `ws://127.0.0.1:8080/ws`. Within five seconds send:

```json
{"type":"auth","token":"your-gateway-token"}
```

The server responds with `{"type":"ready","schema":1,"account":"<Telegram user ID>"}`.
The Telegram account must be ready before connecting. Tokens in query strings
are not accepted. Browser origins must be listed exactly using `--cors`, e.g.
`--cors http://localhost:3000`; wildcard origins are rejected for WebSocket.
Non-browser clients can omit Origin. For access across machines terminate TLS
and use HTTPS/WSS; the default binding is loopback.

Operations use the same method names and parameters as HTTP:

```json
{"type":"request","id":"unique-action-1","method":"send_message","params":{"peer":"@SomeBot","message":"/start"}}
```

Responses contain `type: response`, the matching `id`, `ok`, and the existing
HTTP result/error envelope. Sending success only acknowledges the send.
Bot responses are separate events, may arrive before the response envelope,
and may include multiple new messages or edits. Correlate by account, peer,
message ID and reply metadata, rather than assuming the next event is a reply.
Pass `"confirm":true` for operations requiring confirmation. At most eight
requests per socket run concurrently; additional requests return `busy`.
Responses can arrive out of order. Maximum inbound frame size is 1 MiB.

### Subscribe and resume

```json
{"type":"request","id":"sub-1","method":"events.subscribe","params":{"cursor":123,"kinds":["new_message","edit_message"],"peers":["user:10001"]}}
```

Omit `cursor` to start from now; use `0` for all retained events. Omit `kinds`
or `peers` for all allowed values. Peer filters use the exact typed peer IDs
in events, e.g. `user:123`, `chat:123`, `channel:123`, not usernames. Policy is
checked before subscription and again for each event, including policy reloads.

```json
{"type":"event","cursor":124,"account":"123456789","event":"new_message","data":{"peer":"user:10001","id":321,"text":"Results"}}
```

Persist each `checkpoint.cursor` after processing all preceding events. A
checkpoint advances across filtered events too. On reconnect authenticate
again and subscribe with the saved cursor, scoped to this gateway database
and account. Delivery is at least once: deduplicate by cursor. Message
edits are separate events, so do not deduplicate all edits by message ID.
`events.unsubscribe` stops the subscription. One active subscription per socket.

An expired/future cursor returns `type: resync_required` and stops that
subscription. Reconcile with `get_messages`, then subscribe from now.
The journal retains at most 10,000 events and 24 hours. It survives gateway
restarts, but cannot recreate updates Telegram did not deliver while the
gateway was offline. `connection.state` events set `resync_required: true`:
check relevant chat history after startup or a connection transition.

### Failure and retry rules

Write receipts are scoped by Telegram user ID and shared across HTTP/WS.
Identical request IDs and parameters return the saved response. Reusing an ID
with different parameters returns `request_conflict`. Receipts are retained
for seven days; use globally unique IDs and do not retry old writes after
that retention window.

The gateway reserves the receipt before calling Telegram. If it crashes in
between, retry returns `unknown` and does not execute the write again. Inspect
chat history before issuing any replacement action. A timed-out request does
not prove a send failed. This is deduplication, not an exactly-once guarantee.

The server sends WebSocket ping frames every 20 seconds and waits up to ten
seconds for pong. Slow clients have a bounded 64-frame queue; an overflowing
or stalled socket is closed. Reconnect and resume from your last checkpoint.
Do not block your client's receive loop while waiting for command responses.

SQLite stores message events and receipts locally, including their content.
Keep its directory private. Session encryption is separate from the journal.
Logs use the existing redaction/audit pipeline; authentication frames are not
logged. Treat all Telegram message text as untrusted input to an AI agent.

## Example client

With Node.js 22 or newer:

```powershell
node examples/gateway-client.mjs
```

It reads `AGENT_TELEGRAM_API_SECRET`, subscribes to events and requests account
status. It never sends Telegram messages. Use `GATEWAY_URL` to override the
WebSocket address. The example prints events; production clients must persist
checkpoints and deduplicate them across reconnects.
