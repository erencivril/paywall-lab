APP_PORT ?= 8080
BASE_URL ?= http://localhost:$(APP_PORT)
VENV     ?= tests/.venv
PYTEST   := $(VENV)/bin/pytest --base-url $(BASE_URL)

.PHONY: up down logs unit lint venv api e2e test schemas

up: ## build the image and start app + postgres, wait until healthy
	docker compose up -d --build --wait

down: ## stop everything and delete the database volume
	docker compose down -v

logs:
	docker compose logs -f app

unit: ## Go unit tests (set DATABASE_URL to include the database-backed ones)
	go vet ./...
	go test -race -count=1 ./...

lint:
	golangci-lint run

venv: $(VENV)/.installed
$(VENV)/.installed: tests/requirements.txt
	python3.12 -m venv $(VENV)
	$(VENV)/bin/pip install -r tests/requirements.txt
	$(VENV)/bin/playwright install chromium
	touch $@

api: up venv ## API and contract tests against the running stack
	$(PYTEST) tests/api tests/contract

e2e: up venv ## Playwright tests against the running stack
	$(PYTEST) tests/e2e

test: unit api ## everything except the browser tests

schemas: venv ## write JSON Schemas generated from tracking-plan.yaml to reports/schemas
	$(VENV)/bin/python -m tests.support.tracking_plan reports/schemas
