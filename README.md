# strava-claude

A personal running coach powered by Claude, backed by an AWS Lambda proxy that handles Strava OAuth so you never have to. Works in **Claude Code** (via a skill) and **Claude.ai web/mobile** (via MCP).


> [!NOTE]
> **AI Transparency Disclosure:** This project utilizes AI coding assistants to generate boilerplates, optimize benchmarks, and refine documentation. All critical logic and performance calculations are human-reviewed and verified.


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
| `INTERVALS_API_KEY` | intervals.icu API key — from intervals.icu Settings → Developer Settings (optional, only needed for the intervals.icu MCP integration below) |
| `INTERVALS_BASE_URL` | Set this after `make deploy-intervals` (copied from CDK output) |

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

### 6. (Optional) Connect intervals.icu for granular Garmin data

intervals.icu imports activities directly from Garmin, so it has finer-grained HR/power/pace
streams than Strava exposes. A second, independent Lambda + MCP server proxies it, so Claude can
pull that data without you manually uploading a `.fit` file into the chat.

```bash
make create-intervals-secret   # writes INTERVALS_API_KEY + SKILL_SECRET to Secrets Manager
make deploy-intervals           # builds + deploys the IntervalsSkillStack
```

Copy the printed URL into `.env` as `INTERVALS_BASE_URL`, then in Claude.ai → **Settings →
Integrations → Add MCP Server**, enter:

- **URL:** `https://<INTERVALS_BASE_URL>/mcp?key=<SKILL_SECRET>`

Claude.ai will discover four tools: `list_activities`, `get_activity`, `get_streams`,
`get_intervals` (intervals.icu's auto-detected interval segments — richer than Strava laps,
with per-interval average power/HR/pace).

#### Temperature provenance (`temp_source`)

intervals.icu returns two unrelated temperatures under confusingly similar names. `average_temp`
/ `min_temp` / `max_temp` are the **device sensor** reading — on a wrist-worn watch that is
mostly skin contact and body heat, not the air. Ambient temperature lives in
`average_weather_temp` / `min_weather_temp` / `max_weather_temp`, and is only populated when
`has_weather` is `true`.

The values look plausible either way, so this gets misread silently — a 30°C wrist on a 14°C
afternoon becomes a phantom heat explanation for cardiac drift. The intervals proxy therefore
adds a `temp_source` field to every activity it returns, on both the `/activities` REST routes
and the `list_activities` / `get_activity` MCP tools:

| `temp_source` | meaning |
|---|---|
| `weather_service` | `average_weather_temp` and friends are real ambient temperature — use those |
| `device_sensor` | only the sensor reading exists; a `temp_note` field spells out the caveat |
| `unavailable` | no temperature of either kind |

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

## Local debugging

`intervals/debug_test.go` runs the Lambda handler locally so you can step through it. It's behind a
`//go:build debug` tag, so it never reaches normal builds, tests, or the deployed binary.

Set a breakpoint in `intervals/main.go`, then click **debug test** above the route you want —
`TestDebugListActivities`, `TestDebugActivityDetail`, `TestDebugStreams`, `TestDebugIntervals`, or
`TestDebugMCP`. Nothing to configure: `.vscode/settings.json` supplies the build tag and your `.env`.
Breakpoints in the `shared` module work too. To inspect a different activity, edit the
`debugActivityID` constant at the top of the file.

No AWS credentials needed — `shared.LoadSecret` short-circuits when `SECRET_JSON` is set, and the
harness builds that from `INTERVALS_API_KEY` + `SKILL_SECRET` in `.env`. Set `SECRET_ARN` in `.env`
if you'd rather exercise the real Secrets Manager path. The upstream intervals.icu calls are real
either way.

From the terminal:

```bash
cd intervals
go test -tags debug -run TestDebug -v .
dlv test --build-flags='-tags=debug' -- -test.run TestDebugActivityDetail   # then: break main.go:55
```

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
| `make create-intervals-secret` | Create the intervals.icu Secrets Manager secret from `.env` values |
| `make check-intervals-secret` | Print the current intervals.icu secret value |
| `make build-intervals-lambda` | Cross-compile Go for linux/arm64 and zip the intervals.icu Lambda |
| `make deploy-intervals` | Build + `cdk deploy IntervalsSkillStack` |
