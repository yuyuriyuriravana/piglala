# Piglala Discord Bot

A small Go Discord bot that watches Final Fantasy XIV characters on FFLogs and sends a Discord DM when a watched player improves a recorded parse.

The bot stores its local watch list, notification subscribers, recorded best parses, tracked-player poll history, and received Discord messages in a SQLite database. The default database file is `store.sqlite3`, which is runtime state and is intentionally ignored by git.

## Commands

Commands are accepted from any DM or server channel the bot can access.

- `!status` lists watched players and their recorded best parses for tracked Savage, Extreme, and Ultimate fights.
- `!help` shows all supported commands.
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
```

## Requirements

- Go 1.22 or newer
- A Discord application with a bot token
- FFLogs API OAuth client credentials

## Discord Setup

1. Open the Discord Developer Portal.
2. Create an application.
3. Add a bot to the application.
4. Copy the bot token.
5. Invite the bot somewhere you can share a server with it, so you can open a DM with the bot.

The implementation requests direct-message, guild-message, and message-content gateway intents so it can read DMs and server channels the bot can access.

For a basic bot invite URL, replace `YOUR_CLIENT_ID` with your application's client ID:

```text
https://discord.com/oauth2/authorize?client_id=YOUR_CLIENT_ID&permissions=68608&integration_type=0&scope=bot
```

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
```

`POLL_INTERVAL_MINUTES` is optional and defaults to `30`.
`MESSAGE_TEMPLATE_DIR` is required and defaults to the committed `templates` directory in `.env.example`. The bot loads all required Discord message templates from that directory and fails startup if any file is missing, invalid, or references unsupported placeholders.
`STORE_DB_PATH` is optional and defaults to `store.sqlite3`.

Do not commit `.env`; it contains secrets.

## Polling

On startup, the bot fetches relevant FFLogs zones and records current bests without sending improvement notifications. After that, it polls every `POLL_INTERVAL_MINUTES`.

The poller tracks FFLogs zone/difficulty pairs whose difficulty name is:

- `Savage`
- `Extreme`
- `Ultimate`

When a watched character's `rankPercent` improves for an encounter, the bot sends a DM to each user who subscribed in DMs and posts to each subscribed channel.

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

## Local Files

These files are expected locally but are not committed:

- `.env`: Discord and FFLogs credentials plus local channel/user IDs.
- `store.sqlite3`: watched players, notification subscribers, recorded best parses, tracked-player poll history, and received Discord messages.
- `piglala` or `bot`: local build output.
- `*.log`, `coverage.out`, `*.prof`: local diagnostics and test artifacts.

## Run

Install dependencies and start the bot:

```sh
go mod tidy
go run .
```

Build a small production binary:

```sh
go build -ldflags="-s -w" -o piglala .
./piglala
```
