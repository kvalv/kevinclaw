---
name: app-development
description: Frontend development workflow for apps/. Covers dev server setup, browser testing, and screenshot-based validation.
allowed-tools: Bash, Read, Edit, Write, Glob, Grep
---

# App Development

Workflow for working on frontend apps in `apps/`.

## Dev server

Start the dev server in the worktree:

```bash
cd {worktree_path}/apps/{relevant-app}
npm install
REACT_APP_DEFAULT_USER="$REACT_APP_DEFAULT_USER" \
REACT_APP_DEFAULT_PASSWORD="$REACT_APP_DEFAULT_PASSWORD" \
npm run dev:proxy &
```

Wait for the server to be ready — poll `http://localhost:3000` until it responds.

When done with all work, shut down the server: `kill %1`

## Browser testing

Use the `browser` MCP tools to interact with the running app:

1. `navigate_page` to `http://localhost:3000/...`
2. `take_screenshot` to capture the current state
3. `fill`, `click`, `type_text` etc. to interact with the page

## Screenshot validation

For bug fixes, take before/after screenshots to prove the fix works:

1. **Before**: navigate to the page showing the bug, take screenshot, save to `/tmp/{issue-id}-before.png`
2. **After**: same page after the fix, save to `/tmp/{issue-id}-after.png`
3. Upload both:

```bash
BEFORE_URL=$(upload-screenshot /tmp/{issue-id}-before.png {issue-id}-before)
AFTER_URL=$(upload-screenshot /tmp/{issue-id}-after.png {issue-id}-after)
```

4. Comment on the PR with the screenshots

Screenshots are the reproduction proof for frontend bugs — no need to write unit tests.

## Notes

- The proxy connects to the staging backend, so you get real data
- Login happens automatically via the env vars
- If the server fails to start, check `npm install` output for errors
- Port 3000 is the default
