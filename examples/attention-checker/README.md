# Axon startup attention checker

A tiny unattended Axon app that checks three read-only sources when your machine starts:

- LinkedIn direct-message activity
- LinkedIn notifications / post engagement
- recent unread Gmail

The model sees only three MCP tools. The browser bridge exposes no arbitrary navigation, click, type, send, delete, react, archive, label, or compose operation.

## Important LinkedIn caveat

LinkedIn's User Agreement prohibits unauthorized browser automation and scraping. This example deliberately stays low-volume and read-only and includes no stealth, CAPTCHA bypass, rate-limit bypass, or write automation, but using it can still create account-restriction risk. If LinkedIn presents a challenge, stop the checker and complete the challenge manually.

## 1. Install the browser bridge

Requirements: Node.js, Google Chrome (or another Chromium executable), and npm.

```bash
cd examples/attention-checker/browser-mcp
npm install
npm run login
```

A dedicated Chrome profile opens with LinkedIn and Gmail tabs. Log into both manually, complete MFA if needed, then return to the terminal and press Enter.

The profile defaults to:

```text
~/.local/share/axon/attention-browser
```

A dedicated profile is intentional. Recent Chrome versions do not support automating the normal default profile safely. Override the location with `AXON_ATTENTION_PROFILE`.

If Chrome is not available by channel name, point directly at a Chromium executable:

```bash
export ATTENTION_BROWSER_EXECUTABLE=/path/to/chromium
npm run login
```

## 2. Run the checker

From the Axon repository root:

```bash
export ATTENTION_MODEL='your-openrouter-model'
export OPENROUTER_API_KEY='...'
go run ./examples/attention-checker
```

Optional model endpoint overrides:

```bash
export ATTENTION_MODEL_BASE_URL='https://openrouter.ai/api'
export ATTENTION_MODEL_API_KEY='...'
```

Optional browser controls:

```bash
export AXON_ATTENTION_PROFILE="$HOME/.local/share/axon/attention-browser"
export ATTENTION_HEADLESS=1
export ATTENTION_BROWSER_CHANNEL=chrome
```

Expected output is either exactly:

```text
NOTHING IMPORTANT
```

or a maximum-five-bullet attention brief. On Linux, important output is also sent through `notify-send` when available.

The last successful scan time is stored at:

```text
~/.local/state/axon/attention-last-check
```

If any source is logged out, challenged, or unreadable, the checkpoint does not advance.

## 3. Run once at desktop startup (Linux/systemd user)

Create `~/.config/axon/attention.env`:

```bash
ATTENTION_MODEL=your-openrouter-model
OPENROUTER_API_KEY=...
ATTENTION_HEADLESS=1
```

Protect it:

```bash
chmod 600 ~/.config/axon/attention.env
```

Create `~/.config/systemd/user/axon-attention.service` and adjust `WorkingDirectory` to your Axon checkout:

```ini
[Unit]
Description=Axon startup attention checker
After=network-online.target graphical-session.target
Wants=network-online.target

[Service]
Type=oneshot
WorkingDirectory=%h/src/axon
EnvironmentFile=%h/.config/axon/attention.env
ExecStart=/usr/bin/go run ./examples/attention-checker

[Install]
WantedBy=default.target
```

Enable it:

```bash
systemctl --user daemon-reload
systemctl --user enable axon-attention.service
systemctl --user start axon-attention.service
```

Inspect the latest run with:

```bash
journalctl --user -u axon-attention.service -n 100 --no-pager
```

## Architecture

```text
systemd user service
        |
        v
 attention-checker (Go / Axon)
        |
        | stdio MCP
        v
 browser-mcp (Node + Playwright)
      /     |      \
LinkedIn  LinkedIn  Gmail
 inbox    activity  unread
        |
        v
      LLM triage
        |
        +--> NOTHING IMPORTANT
        `--> desktop notification + brief
```

The Axon runtime itself stays provider-agnostic: this is just an application built on its existing `MCPServers` support.
