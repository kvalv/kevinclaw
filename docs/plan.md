# Home Assistant MCP — Plan

## Goal

Kevin can:

- Start/stop/dock vacuum
- Check if lights are on, turn on/off
- Read temperature sensors

## Architecture

```
internal/mcp/
└── ha.go           # HA REST API client + MCP server + toml config loader
scripts/
└── ha-export.sh    # exports entities from HA, outputs ha.toml skeleton
ha.toml             # gitignored — the actual config
ha.toml.example     # checked in — shows format
```

## ha.toml

Categories map to what Kevin can do with them. Entity IDs stay in the config, Kevin sees friendly names.

```toml
url = "https://homeassistant.example.com"
# token from env: HOMEASSISTANT_API_TOKEN

[[entities]]
id = "vacuum.roborock_s7"
name = "vacuum"
category = "vacuum"         # actions: start, stop, dock, status
description = "Living room robot vacuum"

[[entities]]
id = "light.living_room"
name = "living_room_lights"
category = "light"          # actions: turn_on, turn_off, status
description = "Main living room ceiling lights"

[[entities]]
id = "sensor.bedroom_temperature"
name = "bedroom_temp"
category = "sensor"         # actions: status (read-only)
description = "Bedroom temperature sensor (celsius)"
```

## Categories define available actions

| Category | Actions                   | HA Services                                                  |
| -------- | ------------------------- | ------------------------------------------------------------ |
| vacuum   | start, stop, dock, status | vacuum.start, vacuum.stop, vacuum.return_to_base, states API |
| light    | turn_on, turn_off, status | light.turn_on, light.turn_off, states API                    |
| sensor   | status                    | states API (read-only)                                       |

## MCP tools (2 generic tools)

- `ha_status(name?)` — get state of one entity or all. Returns friendly name + state + attributes.
- `ha_action(name, action)` — execute an action. Validates action is allowed for the category.

Example:

- `ha_status()` → "vacuum: docked, living_room_lights: on, bedroom_temp: 21.3°C"
- `ha_status("vacuum")` → "vacuum: cleaning, battery: 73%"
- `ha_action("vacuum", "start")` → "vacuum: started"
- `ha_action("living_room_lights", "turn_off")` → "living_room_lights: off"

## Export script

```bash
# ha-export.sh — fetches all entities, filters by domain, outputs toml skeleton
curl -s -H "Authorization: Bearer $TOKEN" "$URL/api/states" \
  | jq '[.[] | select(.entity_id | test("^(light|sensor|vacuum)\\."))]
        | .[] | {id: .entity_id, name: (.entity_id | split(".")[1]),
                 category: (.entity_id | split(".")[0])}'
```

User then picks which entities to keep and adds descriptions.

## Steps

1. `internal/mcp/ha.go` — API client + config loader + MCP tools
2. `ha.toml.example`
3. Wire into main.go (if ha.toml exists)
4. `scripts/ha-export.sh`
5. Block for non-owner in policy
