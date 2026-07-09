LAMBDA_DIR    := ./lambda
INTERVALS_DIR := ./intervals
CDK_DIR       := ./cdk
SKILL_DIR     := ./skill
SKILL_TARGET  := $(HOME)/.claude/skills/strava-analyzer
SECRET_NAME   ?= StravaSkillStack/strava/oauth
INTERVALS_SECRET_NAME ?= IntervalsSkillStack/intervals/api

# Load variables from .env if present; explicit env vars take precedence.
BASE_URL           ?= $(shell grep '^BASE_URL='           .env 2>/dev/null | cut -d= -f2-)
SKILL_SECRET       ?= $(shell grep '^SKILL_SECRET='       .env 2>/dev/null | cut -d= -f2-)
TRAINING_GOAL      ?= $(shell grep '^TRAINING_GOAL='      .env 2>/dev/null | cut -d= -f2-)
CLIENT_ID          ?= $(shell grep '^CLIENT_ID='          .env 2>/dev/null | cut -d= -f2-)
CLIENT_SECRET      ?= $(shell grep '^CLIENT_SECRET='      .env 2>/dev/null | cut -d= -f2-)
REFRESH_TOKEN      ?= $(shell grep '^REFRESH_TOKEN='      .env 2>/dev/null | cut -d= -f2-)
INTERVALS_API_KEY  ?= $(shell grep '^INTERVALS_API_KEY='  .env 2>/dev/null | cut -d= -f2-)
INTERVALS_BASE_URL ?= $(shell grep '^INTERVALS_BASE_URL=' .env 2>/dev/null | cut -d= -f2-)

.PHONY: all build-lambda deploy install-skill create-secret check-secret test clean \
        build-intervals-lambda deploy-intervals create-intervals-secret check-intervals-secret

all: deploy install-skill

build-lambda:
	@echo "→ Building Lambda binary..."
	cd $(LAMBDA_DIR) && GOARCH=arm64 GOOS=linux go build -o bootstrap .
	cd $(LAMBDA_DIR) && zip -j bootstrap.zip bootstrap
	rm -f $(LAMBDA_DIR)/bootstrap

deploy: build-lambda
	@echo "→ Deploying CDK stack..."
	cd $(CDK_DIR) && cdk deploy StravaSkillStack --require-approval never

build-intervals-lambda:
	@echo "→ Building intervals.icu Lambda binary..."
	cd $(INTERVALS_DIR) && GOARCH=arm64 GOOS=linux go build -o bootstrap .
	cd $(INTERVALS_DIR) && zip -j bootstrap.zip bootstrap
	rm -f $(INTERVALS_DIR)/bootstrap

deploy-intervals: build-intervals-lambda
	@echo "→ Deploying intervals.icu CDK stack..."
	cd $(CDK_DIR) && cdk deploy IntervalsSkillStack --require-approval never

create-intervals-secret: _require-INTERVALS_API_KEY _require-SKILL_SECRET
	@echo "→ Creating Secrets Manager secret $(INTERVALS_SECRET_NAME)..."
	aws secretsmanager create-secret \
		--name $(INTERVALS_SECRET_NAME) \
		--secret-string '{"api_key":"$(INTERVALS_API_KEY)","skill_auth_key":"$(SKILL_SECRET)"}'
	@echo "  Created."

check-intervals-secret:
	@aws secretsmanager get-secret-value --secret-id $(INTERVALS_SECRET_NAME) --query SecretString --output text

install-skill: _require-BASE_URL _require-SKILL_SECRET _require-TRAINING_GOAL
	@echo "→ Installing skill..."
	mkdir -p $(SKILL_TARGET)
	BASE_URL=$(BASE_URL) SKILL_SECRET=$(SKILL_SECRET) TRAINING_GOAL='$(TRAINING_GOAL)' \
		envsubst '$$BASE_URL $$SKILL_SECRET $$TRAINING_GOAL' < $(SKILL_DIR)/SKILL.md > $(SKILL_TARGET)/SKILL.md
	@echo "  Installed: $(SKILL_TARGET)/SKILL.md"

create-secret: _require-CLIENT_ID _require-CLIENT_SECRET _require-REFRESH_TOKEN _require-SKILL_SECRET
	@echo "→ Creating Secrets Manager secret $(SECRET_NAME)..."
	aws secretsmanager create-secret \
		--name $(SECRET_NAME) \
		--secret-string '{"client_id":"$(CLIENT_ID)","client_secret":"$(CLIENT_SECRET)","refresh_token":"$(REFRESH_TOKEN)","skill_auth_key":"$(SKILL_SECRET)"}'
	@echo "  Created."

check-secret:
	@aws secretsmanager get-secret-value --secret-id $(SECRET_NAME) --query SecretString --output text

test: _require-BASE_URL _require-SKILL_SECRET
	@echo "→ Running integration tests..."
	cd $(LAMBDA_DIR) && STRAVA_BASE_URL=$(BASE_URL) STRAVA_SECRET=$(SKILL_SECRET) \
		go test -v -tags integration ./...

clean:
	rm -f $(LAMBDA_DIR)/bootstrap $(LAMBDA_DIR)/bootstrap.zip
	rm -f $(INTERVALS_DIR)/bootstrap $(INTERVALS_DIR)/bootstrap.zip

_require-%:
	@if [ -z "$($(*))" ]; then \
		echo "Error: $* is not set. Add it to .env or export it."; exit 1; \
	fi
