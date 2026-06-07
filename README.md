# strava-claude

A personal running coach powered by Claude, backed by an AWS Lambda proxy that handles Strava OAuth so you never have to. Works in **Claude Code** (via a skill) and **Claude.ai web/mobile** (via MCP).

## Architecture

```
Claude Code / Claude.ai  →  Lambda Function URL  →  Secrets Manager (OAuth creds)
                                                 →  Strava API
```

The Lambda refreshes your Strava access token on every invocation — no token rotation to manage.

## Prerequisites

- AWS CLI configured (`aws sts get-caller-identity` should work)
- CDK bootstrapped in your target account/region (`cdk bootstrap`)
- A Strava API app — create one at [strava.com/settings/api](https://www.strava.com/settings/api)
- Go 1.21+ (for building the Lambda)

## Setup

### 1. Configure your `.env`

Copy the example and fill in your values:

```bash
cp .env.example .env
```

| Variable | Description |
|---|---|
| `CLIENT_ID` | Strava app client ID |
| `CLIENT_SECRET` | Strava app client secret |
| `REFRESH_TOKEN` | Long-lived Strava refresh token with `activity:read_all` scope — see [Getting a refresh token](#getting-a-strava-refresh-token) |
| `SKILL_SECRET` | A secret string you choose — used to protect your Lambda URL |
| `BASE_URL` | Set this after the first deploy (copied from CDK output) |
| `TRAINING_GOAL` | Appears in the coaching persona, e.g. `training for a marathon` |

### 2. Create the AWS secret

The CDK stack reads credentials from Secrets Manager. Create it from your `.env`:

```bash
make create-secret
```

Verify it looks right:

```bash
make check-secret
```

### 3. Deploy the Lambda

```bash
make deploy
```

This compiles the Go Lambda for `linux/arm64`, zips it, and runs `cdk deploy`. Copy the URL from the output and add it to your `.env` as `BASE_URL`:

```
Outputs:
StravaSkillStack.StravaSkillStackApiURL = https://xxxx.execute-api.region.amazonaws.com
```

### 4. Install the Claude Code skill

```bash
make install-skill
```

This substitutes `BASE_URL`, `SKILL_SECRET`, and `TRAINING_GOAL` into the skill template and writes it to `~/.claude/skills/strava-analyzer/SKILL.md`.

### 5. Connect Claude.ai (web/mobile) via MCP

In Claude.ai → **Settings → Integrations → Add MCP Server**, enter:

- **URL:** `https://<BASE_URL>/mcp?key=<SKILL_SECRET>`

No other fields needed. Claude.ai's connector will discover the four tools automatically (`list_activities`, `get_activity`, `get_streams`, `get_laps`).

To give the web/mobile Claude the same coaching persona, paste the contents of `skill/SKILL.md` (after running `make install-skill` so the variables are filled in) as a **Project instruction** in Claude.ai.

## Usage

**Claude Code** — just ask naturally:

```
analyse my last run
how was my workout today
check my cardiac drift from yesterday's long run
```

**Claude.ai** — same phrases work once the MCP server is connected and the Project instruction is set.

Claude fetches your activity, streams, and laps, then delivers a structured coaching breakdown: pacing strategy, cardiac drift (aerobic decoupling %), elevation impact, and next-session recommendations.

## Getting a Strava refresh token

The Lambda uses a long-lived `refresh_token` to mint fresh short-lived tokens on every call. You need a one-time OAuth exchange to get it.

**Step 1** — Authorise your app in the browser. Build the authorization URL from your Strava app settings page, making sure `scope=activity:read_all` is included. After approving, Strava redirects to something like `http://localhost/?code=XXXX` — copy the `code`.

> The code is single-use and expires in minutes. Exchange it immediately — do not paste it into `.env` or Secrets Manager.

**Step 2** — Exchange the code and write to Secrets Manager in one shot (avoids copy-paste truncation):

```bash
RESPONSE=$(curl -s -X POST https://www.strava.com/oauth/token \
  -d client_id=YOUR_CLIENT_ID \
  -d client_secret=YOUR_CLIENT_SECRET \
  -d code=PASTE_CODE_HERE \
  -d grant_type=authorization_code) && \
REFRESH=$(echo "$RESPONSE" | grep -o '"refresh_token":"[^"]*"' | cut -d'"' -f4) && \
echo "refresh_token length: ${#REFRESH}" && \
aws secretsmanager put-secret-value \
  --secret-id StravaSkillStack/strava/oauth \
  --secret-string "{\"client_id\":\"YOUR_CLIENT_ID\",\"client_secret\":\"YOUR_CLIENT_SECRET\",\"refresh_token\":\"$REFRESH\",\"skill_auth_key\":\"YOUR_SKILL_SECRET\"}"
```

The command prints the token length — it should be `40`. If shorter, print `$RESPONSE` to see the error.

**Step 3** — Verify the token has the right scope:

```bash
curl -s -X POST https://www.strava.com/oauth/token \
  -d client_id=YOUR_CLIENT_ID \
  -d client_secret=YOUR_CLIENT_SECRET \
  -d refresh_token=$(aws secretsmanager get-secret-value \
      --secret-id StravaSkillStack/strava/oauth \
      --query SecretString --output text | grep -o '"refresh_token":"[^"]*"' | cut -d'"' -f4) \
  -d grant_type=refresh_token | grep -o '"scope":"[^"]*"'
```

Should return `"scope":"activity:read_all,read"`. If it only shows `"scope":"read"`, the OAuth flow was done without `activity:read_all` — repeat from Step 1.

Also update `REFRESH_TOKEN` in your `.env` so `make create-secret` stays in sync.

## Integration tests

Run the full suite against the live endpoint:

```bash
make test
```

Covers REST endpoints (activities, detail, streams, laps, auth) and all MCP methods (initialize, tools/list, each tool, error cases, notifications).

## Makefile targets

| Target | Description |
|---|---|
| `make create-secret` | Create the Secrets Manager secret from `.env` values |
| `make check-secret` | Print the current secret value |
| `make build-lambda` | Cross-compile Go for linux/arm64 and zip |
| `make deploy` | Build + `cdk deploy` |
| `make install-skill` | Render skill template and copy to `~/.claude/skills/strava-analyzer/` |
| `make test` | Run integration tests against the live endpoint |
| `make clean` | Remove build artifacts |
