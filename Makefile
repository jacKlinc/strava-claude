LAMBDA_DIR   := ./lambda
CDK_DIR      := ./cdk
SKILL_DIR    := ./skill
SKILL_TARGET := $(HOME)/.claude/skills/strava-analyzer
SECRET_NAME  ?= StravaSkillStack/strava/oauth

# Load BASE_URL and SKILL_SECRET from .env if present, env vars take precedence
BASE_URL     ?= $(shell grep '^BASE_URL=' .env 2>/dev/null | cut -d= -f2-)
SKILL_SECRET ?= $(shell grep '^SKILL_SECRET=' .env 2>/dev/null | cut -d= -f2-)

.PHONY: all build-lambda deploy install-skill clean test

all: deploy install-skill

build-lambda:
	@echo "→ Building Lambda binary..."
	cd $(LAMBDA_DIR) && GOARCH=arm64 GOOS=linux go build -o bootstrap .
	cd $(LAMBDA_DIR) && zip -j bootstrap.zip bootstrap
	rm -f $(LAMBDA_DIR)/bootstrap

deploy: build-lambda
	@echo "→ Deploying CDK stack..."
	cd $(CDK_DIR) && cdk deploy --require-approval never

install-skill: _require-BASE_URL _require-SKILL_SECRET
	@echo "→ Installing skill..."
	mkdir -p $(SKILL_TARGET)
	BASE_URL=$(BASE_URL) SKILL_SECRET=$(SKILL_SECRET) envsubst '$$BASE_URL $$SKILL_SECRET' < $(SKILL_DIR)/SKILL.md > $(SKILL_TARGET)/SKILL.md
	@echo "  Installed: $(SKILL_TARGET)/SKILL.md"

test: _require-BASE_URL _require-SKILL_SECRET
	@echo "→ Running integration tests..."
	cd $(LAMBDA_DIR) && STRAVA_BASE_URL=$(BASE_URL) STRAVA_SECRET=$(SKILL_SECRET) \
		go test -v -tags integration ./...

clean:
	rm -f $(LAMBDA_DIR)/bootstrap $(LAMBDA_DIR)/bootstrap.zip

_require-%:
	@if [ -z "$($(*))" ]; then \
		echo "Error: $* is not set. Add it to .env or export it."; exit 1; \
	fi
