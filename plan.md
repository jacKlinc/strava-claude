Building a custom Strava analysis skill for Claude Code using an AWS Lambda proxy in Go is an excellent architectural choice. By offloading the OAuth refresh logic and API abstraction to Lambda, you keep your `SKILL.md` clean and prevent Claude from burning context tokens trying to wrangle Strava's complex raw responses.

Here is the blueprint for your AWS CDK (Go) backend and your Claude Code skill definition.

---

### **1. Architecture & Flow**

Instead of Claude Code directly talking to Strava and managing token rotations via shell scripts, the flow will look like this:

1. **Claude Code** triggers the `strava-analyzer` skill based on your prompt (e.g., "analyse my last run").
2. **Claude Code** uses `curl` to hit your **AWS Lambda Function URL** (e.g., `GET /activities`).
3. **AWS Lambda (Go)** retrieves your Strava `client_id`, `client_secret`, and `refresh_token` from **AWS Secrets Manager**.
4. **Lambda** silently exchanges the refresh token for a fresh 6-hour access token (no need to write the new access token back to Secrets Manager; just exchange it on the fly during the Lambda execution to keep state management at zero).
5. **Lambda** proxies the request to the Strava API, retrieves the data, and returns clean JSON to Claude.
6. **Claude Code** analyzes the time-series streams and delivers your coaching insights.

---

### **2. AWS CDK (Go) Infrastructure Plan**

You will deploy a single AWS Stack containing your Secret and your Go Lambda, exposing it via a Function URL.

**Key CDK Components:**

* **Secrets Manager:** Create a secret named `strava/oauth` manually in the AWS Console (or via AWS CLI) containing your credentials: `{"client_id": "...", "client_secret": "...", "refresh_token": "..."}`.
* **Lambda Function:** Compiled Go binary (`bootstrap`) using the `aws-lambda-go` SDK.
* **Function URL:** Set `AuthType` to `NONE`. To secure it from public abuse without complex AWS IAM signing, pass a custom header (e.g., `X-Claude-Secret`) that your Lambda validates.

**CDK Go Implementation Sketch:**

```go
// ... standard CDK imports ...

func NewStravaSkillStack(scope constructs.Construct, id string, props *StravaSkillStackProps) awscdk.Stack {
	stack := awscdk.NewStack(scope, &id, &props.StackProps)

	// 1. Reference the existing secret
	stravaSecret := awssecretsmanager.Secret_FromSecretNameV2(stack, jsii.String("StravaSecret"), jsii.String("strava/oauth"))

	// 2. Define the Go Lambda Function
	stravaLambda := awslambda.NewFunction(stack, jsii.String("StravaProxyHandler"), &awslambda.FunctionProps{
		Runtime:      awslambda.Runtime_PROVIDED_AL2023(), // Custom runtime for Go
		Architecture: awslambda.Architecture_ARM_64(),
		Handler:      jsii.String("bootstrap"),
		Code:         awslambda.Code_FromAsset(jsii.String("./lambda/main.zip"), nil),
		Environment: &map[string]*string{
			"SECRET_ARN":     stravaSecret.SecretArn(),
			"SKILL_AUTH_KEY": jsii.String("your-super-secret-header-value"), // Simple auth
		},
	})

	// 3. Grant Lambda read access to the Secret
	stravaSecret.GrantRead(stravaLambda)

	// 4. Expose via Function URL
	fnUrl := stravaLambda.AddFunctionUrl(&awslambda.FunctionUrlOptions{
		AuthType: awslambda.FunctionUrlAuthType_NONE,
	})

	// Output the URL so you can copy it into SKILL.md
	awscdk.NewCfnOutput(stack, jsii.String("LambdaUrl"), &awscdk.CfnOutputProps{
		Value: fnUrl.Url(),
	})

	return stack
}

```

---

### **3. The Go Lambda Backend Plan**

Your Go Lambda will use the `events.APIGatewayV2HTTPRequest` struct (which Function URLs use).

**The Routing Logic:**

1. **Validate Authorization:** Check `req.Headers["x-claude-secret"]`.
2. **Route Parsing:** Switch on `req.RequestContext.HTTP.Path`.
* `/activities` -> Fetch recent activities.
* `/activities/{id}` -> Fetch detail.
* `/activities/{id}/streams` -> Fetch streams.
* `/activities/{id}/laps` -> Fetch laps.


3. **Token Refresh:** Before routing to Strava, fetch the secret from Secrets Manager, POST to `https://www.strava.com/oauth/token`, and grab the short-lived `access_token`.
4. **Strava Request:** Execute the actual Strava API call and return the payload.

> **Pro-Tip:** For the `/streams` endpoint, Strava returns a massive array of data. Have your Go backend format this into a condensed format before returning it to Claude. This saves context tokens and makes the data easier for the LLM to parse for your cardiac drift analysis.

---

### **4. Drafting the `SKILL.md**`

Place this in `~/.claude/skills/strava-analyzer/SKILL.md`. Claude Code will match the trigger phrases in the YAML description.

```yaml
---
name: strava-analyzer
description: Use when the user says "analyse my last run", "how was my run today", or asks to compare recent training. Fetches their Garmin/Strava physiological data.
---
# Strava Running Coach & Data Analyst

You are an elite running coach and physiological data analyst. Your job is to analyze the user's Strava `.fit` data via the AWS backend and provide actionable training feedback, specifically keeping their 50k ultra training block in mind.

## Endpoints
You have access to a secure backend API. Always include the header `-H "x-claude-secret: your-super-secret-header-value"`.
Base URL: `https://<YOUR_LAMBDA_URL>.lambda-url.<REGION>.on.aws`

1. **List Recent Activities:**
   `curl -s -H "x-claude-secret: ..." <BASE_URL>/activities`
2. **Full Activity Detail:**
   `curl -s -H "x-claude-secret: ..." <BASE_URL>/activities/<ID>`
3. **Time-Series Streams (Pace, HR, Altitude, Cadence):**
   `curl -s -H "x-claude-secret: ..." <BASE_URL>/activities/<ID>/streams`
4. **Lap Splits:**
   `curl -s -H "x-claude-secret: ..." <BASE_URL>/activities/<ID>/laps`

## Analysis Directives
When executing an analysis, you must explicitly look for and report on the following:

* **Pacing Strategy:** Did they go out too hard? Compare the pace of the first kilometer to the overall average. 
* **Aerobic Efficiency / Cardiac Drift:** Analyze the pace-to-heart-rate ratio (decoupling) in the first half of the run versus the second half. Note if their HR spiked significantly while holding the same pace later in the run.
* **Elevation-Adjusted Pace:** Assess how climbs impacted their heart rate and pacing.
* **RPE vs. HR Consistency:** If the user mentions their Perceived Exertion (RPE), compare it against the objective cardiac load.

## Execution
1. Fetch the latest activity ID.
2. Fetch the streams and laps for that ID.
3. Perform the analysis.
4. End with practical, bulleted next steps for their next 50k training session.

```

---

This architecture gives you the best of both worlds: a highly intelligent AI coach right in your terminal, and a secure, low-maintenance AWS backend that handles the OAuth headaches.

To refine the Go implementation, how do you want to handle the time-series data for the streams endpoint—would you like the Go Lambda to pre-process and downsample the arrays to save Claude's context window, or just pass the raw JSON straight through?