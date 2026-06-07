# strava-claude

A Claude Code skill that acts as your personal running coach, backed by an AWS Lambda proxy that handles Strava OAuth so you never have to.

example [here](examples/long-run.md)

**Trigger phrases:** "analyse my last run", "how was my run today", "check my training", etc.

## Architecture

```
Claude Code  →  curl  →  Lambda Function URL  →  Secrets Manager (OAuth creds)
                                              →  Strava API
```

The Lambda refreshes your Strava access token on every invocation — no token rotation to manage.

## Prerequisites

- AWS CLI configured (`aws sts get-caller-identity` should work)
- CDK bootstrapped in your target account/region (`cdk bootstrap`)
- A Strava app with `client_id`, `client_secret`, and a `refresh_token` with `activity:read_all` scope

## Setup

### 1. Create the AWS secret

The CDK stack references an existing secret by name, so it must exist before deployment. Open [Makefile](Makefile) and edit the placeholder values in the `create-secret` target, then run:

```bash
make create-secret
```

The `skill_auth_key` field is a simple shared secret that prevents your Function URL from being abused — pick anything hard to guess. You can verify the secret looks right with:

```bash
make check-secret
```

### 2. Deploy the Lambda + CDK stack

```bash
make deploy
```

This compiles the Go Lambda for `linux/arm64`, zips it, and runs `cdk deploy`. At the end you'll see output like:

```
Outputs:
StravaSkillStack.StravaSkillStackApiURL = https://abc123.execute-api.us-east-1.amazonaws.com/
```

Copy that URL.

### 3. Install the skill

```bash
make install-skill
```

Then open `~/.claude/skills/strava-analyzer/SKILL.md` and replace the two placeholders:

| Placeholder | Replace with |
|---|---|
| `REPLACE_WITH_YOUR_LAMBDA_URL` | The `LambdaURL` output from step 2 |
| `REPLACE_WITH_YOUR_SKILL_AUTH_KEY` | The `skill_auth_key` value from your secret |

## Usage

In Claude Code, just ask naturally:

```
analyse my last run
how was my workout today
check my cardiac drift from yesterday's long run
```

Claude will fetch your activity, streams, and laps, then give you a structured coaching breakdown covering pacing, cardiac drift (aerobic decoupling %), elevation impact, and next-session recommendations.

## Getting a Strava refresh token

The Lambda uses a long-lived `refresh_token` (not an access token) to mint fresh short-lived tokens on every call. You need to do a one-time OAuth flow to get it.

**Step 1** — Authorise the app in your browser. Build the authorization URL with `scope=activity:read_all` using your Strava app settings. After approving, Strava redirects to `http://localhost/?code=XXXX&scope=...` — copy the `code` value.

> The `code` is single-use and expires in minutes. Do not put it in Secrets Manager — exchange it first.

**Step 2** — Exchange the code and write directly to Secrets Manager in one shot (avoids copy-paste truncation):

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
  --secret-string "{\"client_id\":\"YOUR_CLIENT_ID\",\"client_secret\":\"YOUR_CLIENT_SECRET\",\"refresh_token\":\"$REFRESH\",\"skill_auth_key\":\"YOUR_SKILL_AUTH_KEY\"}"
```

The command prints the token length before writing — it should be `40`. If shorter, check the full `$RESPONSE` for the error.

**Step 3** — Verify scope:

```bash
curl -s -X POST https://www.strava.com/oauth/token \
  -d client_id=YOUR_CLIENT_ID \
  -d client_secret=YOUR_CLIENT_SECRET \
  -d refresh_token=$(aws secretsmanager get-secret-value \
      --secret-id StravaSkillStack/strava/oauth \
      --query SecretString --output text | grep -o '"refresh_token":"[^"]*"' | cut -d'"' -f4) \
  -d grant_type=refresh_token | grep -o '"scope":"[^"]*"'
```

Should return `"scope":"activity:read_all,read"`. If it only shows `"scope":"read"`, the OAuth flow was missing `activity:read_all` — repeat from Step 1.

## Integration tests

Run the full suite against the live deployed endpoint:

```bash
make test
```

Overrides for non-default endpoints:

```bash
make test BASE_URL=https://your-url.execute-api.region.amazonaws.com SKILL_SECRET=your-key
```

Covers: activity list, activity detail, streams, laps, and auth rejection.

## Makefile targets

| Target | Description |
|---|---|
| `make build-lambda` | Cross-compile Go for linux/arm64 and zip |
| `make deploy` | Build + `cdk deploy` |
| `make install-skill` | Copy skill to `~/.claude/skills/strava-analyzer/` |
| `make test` | Run integration tests against the live endpoint |
| `make clean` | Remove build artifacts |
