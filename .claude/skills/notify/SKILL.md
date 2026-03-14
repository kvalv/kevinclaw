---
name: notify
description: Send push notifications to the user's phone/watch via Home Assistant. Use when the user asks to be notified, reminded, or alerted about something.
allowed-tools: Bash(notify:*)
---

# Notify

Send notifications to Mikael's phone and watch via Home Assistant.

## Usage

```bash
notify "Title" "Message"
notify --url "https://example.com" "Title" "Message with link"
notify "Short message"
```

## Rules

- Use two-argument form (title + message) when you have both. Single argument for short messages.
- Add `--url` when there's a relevant link (e.g., a PR URL, a page you found).
- Keep messages concise -- they show on a watch screen.
