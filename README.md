# Piglala Discord Bot

A small Go Discord bot that watches Final Fantasy XIV characters on FFLogs and sends a Discord DM when a watched player improves a recorded parse.

The bot stores its local watch list, notification subscribers, recorded best parses, tracked-player poll history, and received Discord messages in a SQLite database. The default database file is `store.sqlite3`, which is runtime state and is intentionally ignored by git.

## Commands

Commands are accepted from any DM or server channel the bot can access.

- `!status` lists watched players and their recorded best parses for the latest Savage tier plus any Ultimates they have done.
- `!help` shows all supported commands.
- `!play <YouTube URL>` joins the command author's voice channel and plays that video's audio. A new request replaces the current track.
- `!price <item name>` compares the cheapest listing in Oceania, Japan, North America, and Europe, then adds a short llama-server observation based on recent completed sales.
- `!stop` stops playback and leaves the voice channel.
- `!subscribe` subscribes the DM user, or the server channel where it is typed, to parse improvement notifications.
- `!unsubscribe` removes the DM user, or the server channel where it is typed, from parse improvement notifications.
- `!watch <name> <server> <region>` starts tracking a player.
- `!unwatch <name> <server> <region>` stops tracking a player.

Examples:

```text
!watch <Yuyuri Yuri> <Ravana> <OC>
!unwatch <Yuyuri Yuri> <Ravana> <OC>
!help
!subscribe
!unsubscribe
!status
!price antique wall shelf
!price dancing wings
!play https://www.youtube.com/watch?v=XFgpi7vynko
!stop
```

## Requirements

- Go 1.25 or newer
- Docker with Compose (for the local Lavalink voice node)
- A Discord application with a bot token
- FFLogs API OAuth client credentials
- Network access to the public XIVAPI and Universalis APIs
- An OpenAI-compatible llama-server (optional; prices still display without its commentary)

## Discord Setup

1. Open the Discord Developer Portal.
2. Create an application.
3. Add a bot to the application.
4. Copy the bot token.
5. Invite the bot somewhere you can share a server with it, so you can open a DM with the bot.

The implementation requests direct-message, guild-message, and message-content gateway intents so it can read DMs and server channels the bot can access.

For a basic bot invite URL, replace `YOUR_CLIENT_ID` with your application's client ID:

```text
https://discord.com/oauth2/authorize?client_id=YOUR_CLIENT_ID&permissions=3214336&integration_type=0&scope=bot
```

The bot needs View Channel, Send Messages, Read Message History, Connect, and
Speak permissions. It also requests the non-privileged Guild Voice States
gateway intent so it can find the command author's current voice channel.

## Environment

Create a `.env` file from the example:

```sh
cp .env.example .env
```

Put this in `.env`:

```env
DISCORD_BOT_TOKEN=your_real_discord_bot_token
FFLOGS_CLIENT_ID=your_fflogs_client_id
FFLOGS_CLIENT_SECRET=your_fflogs_client_secret
MESSAGE_TEMPLATE_DIR=templates
STORE_DB_PATH=store.sqlite3
POLL_INTERVAL_MINUTES=30
XIVAPI_BASE_URL=https://v2.xivapi.com/api
UNIVERSALIS_BASE_URL=https://universalis.app/api/v2
ITEM_CATALOG_REFRESH_HOURS=168
PRICE_HISTORY_ENTRIES=20
LLAMA_SERVER_URL=http://127.0.0.1:8080/v1/chat/completions
LLAMA_MAX_TOKENS=512
LLAMA_TEMPERATURE=0.7
LAVALINK_ADDRESS=127.0.0.1:2333
LAVALINK_PASSWORD=youshallnotpass
LAVALINK_SECURE=false
```

`POLL_INTERVAL_MINUTES` is optional and defaults to `30`.
`MESSAGE_TEMPLATE_DIR` is required and defaults to the committed `templates` directory in `.env.example`. The bot loads all required Discord message templates from that directory and fails startup if any file is missing, invalid, or references unsupported placeholders.
`STORE_DB_PATH` is optional and defaults to `store.sqlite3`.
`ITEM_CATALOG_REFRESH_HOURS` defaults to `168` (seven days). The bot downloads
English item names from XIVAPI, keeps only IDs reported as marketable by
Universalis, and caches the resulting fuzzy-search catalog in `STORE_DB_PATH`.
`PRICE_HISTORY_ENTRIES` controls how many recent completed sales per region are
sent to llama-server and defaults to `20`.
`XIVAPI_BASE_URL` and `UNIVERSALIS_BASE_URL` normally do not need to be changed.
`LLAMA_SERVER_URL` points to an OpenAI-compatible chat-completions endpoint. If
it is unavailable or returns unusable output, the deterministic price comparison
is sent without an appended note.
The Lavalink values are optional and default to the local node supplied by
`compose.yaml`.

Do not commit `.env`; it contains secrets.

## Polling

On startup, the bot fetches relevant FFLogs zones and records current bests without sending improvement notifications. After that, it polls every `POLL_INTERVAL_MINUTES`.

The poller tracks FFLogs zone/difficulty pairs whose difficulty name is:

- `Savage`
- `Extreme`
- `Ultimate`

When a watched character's `rankPercent` improves for an encounter, the bot sends a DM to each user who subscribed in DMs and posts to each subscribed channel.

## Market Price Lookup

The first `!price` lookup populates the local marketable-item catalog if the
background startup refresh has not completed. Item matching checks normalized
exact names first, then applies deterministic edit-distance heuristics. A
high-confidence typo such as `dancing wings` resolves to `Dancing Wing`;
ambiguous input returns up to three suggestions instead of guessing.

For a resolved item, the bot requests one cheapest listing and recent completed
sales from each global Universalis region in parallel. Current listings are
always rendered deterministically. Structured per-unit sale history, quality,
world, timestamp, and sale velocity are then passed to llama-server, which is
instructed to produce one cautious buy/sell observation without predicting
future prices. Universalis data is crowdsourced, so the displayed listing age
should be considered before acting.

## Message Templates

Discord messages are managed as Go `text/template` files under `MESSAGE_TEMPLATE_DIR`. The app expects these fixed filenames:

- `parse-improvement.tmpl`
- `help.tmpl`
- `status-empty.tmpl`
- `status-player.tmpl`
- `status-no-parses.tmpl`
- `status-no-displayed-parses.tmpl`
- `status-tier.tmpl`
- `status-parse.tmpl`
- `subscribe-added.tmpl`
- `subscribe-already.tmpl`
- `subscribe-save-failed.tmpl`
- `unsubscribe-missing.tmpl`
- `unsubscribe-removed.tmpl`
- `unsubscribe-save-failed.tmpl`
- `watch-usage.tmpl`
- `watch-save-failed.tmpl`
- `watch-already.tmpl`
- `watch-added.tmpl`
- `unwatch-usage.tmpl`
- `unwatch-save-failed.tmpl`
- `unwatch-missing.tmpl`
- `unwatch-removed.tmpl`
- `price-usage.tmpl`
- `price-not-found.tmpl`
- `price-ambiguous.tmpl`
- `price-fetch-failed.tmpl`
- `price-result.tmpl`

Parse improvement templates support:

- `{{.PlayerName}}`
- `{{.Server}}`
- `{{.Region}}`
- `{{.EncounterName}}`
- `{{.OldPercent}}`
- `{{.NewPercent}}`

Player command/status templates support:

- `{{.PlayerKey}}`
- `{{.Name}}`
- `{{.Server}}`
- `{{.Region}}`

Status tier templates support `{{.TierName}}`.
Status parse row templates support `{{.EncounterName}}` and `{{.Percent}}`.
Price result templates receive `{{.ItemName}}`, `{{.UniversalisURL}}`,
`{{.MatchedQuery}}`, and regional rows containing listing availability, price,
quality, world, and age. Price resolution templates receive `{{.Query}}` and,
for ambiguous input, `{{.Suggestions}}`.

## Local Files

These files are expected locally but are not committed:

- `.env`: Discord and FFLogs credentials plus local channel/user IDs.
- `store.sqlite3`: watched players, notification subscribers, parse data, message logs, generated bot responses, and the cached marketable-item catalog.
- `piglala` or `bot`: local build output.
- `*.log`, `coverage.out`, `*.prof`: local diagnostics and test artifacts.

## Run

Start the DAVE-compatible voice node:

```sh
docker compose up -d lavalink
```

Lavalink handles Discord voice encryption and Opus audio delivery. The
committed configuration installs the maintained YouTube source plugin and
binds its API only to `127.0.0.1`.

Install Go dependencies and start the bot:

```sh
go mod tidy
go run .
```

Build a small production binary:

```sh
go build -ldflags="-s -w" -o piglala .
./piglala
```
