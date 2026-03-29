# Contributing to Campus Cloud Print Queue

Thank you for your interest in contributing! This document covers the development workflow and conventions for this project.

## Prerequisites

- **AWS CLI** configured with valid credentials
- **Terraform** >= 1.0
- **Docker** running
- **Python** 3.11+
- **Go** 1.25+ (only if working on the Gin API variant)
- **Make**

## Local Development

### Python API (`services/api/`)

```bash
cd services/api
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

# Set required environment variables (see services/api/.env.example)
uvicorn app.main:app --host 0.0.0.0 --port 8000 --reload
```

### Printer Worker (`services/printer-worker/`)

```bash
cd services/printer-worker
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt

# Set required environment variables (see services/printer-worker/.env.example)
python -m app.main
```

### Go API (`services/api-gin/`)

```bash
cd services/api-gin
go run .
```

### Running Tests

```bash
# Install test/experiment dependencies
pip install -r requirements-dev.txt

# Run unit tests
pytest tests/ -v

# Run experiments (requires a deployed stack)
make exp1-load
make exp2-contention
make exp3-saturation
make exp4-fault
```

## Code Style

- **Python**: Follow PEP 8. Use type hints where practical. Keep functions short and focused.
- **Go**: Run `gofmt` and `go vet` before committing.
- **Terraform**: Run `terraform fmt` before committing.

## Pull Request Process

1. Create a feature branch from `main`: `git checkout -b feature/your-feature`
2. Make your changes with clear, descriptive commits.
3. Ensure Docker images build successfully: `make build-api && make build-worker`
4. Run `terraform fmt -check` on any Terraform changes.
5. Open a PR against `main` with a description of what changed and why.
6. At least one team member should review before merging.

## Project Conventions

- **Environment configuration**: All runtime config comes from environment variables (never hardcoded secrets).
- **State machine transitions**: Always use DynamoDB conditional expressions to enforce valid state transitions.
- **Error handling**: Log errors with structured JSON; never silently swallow exceptions.
- **Idempotency**: Workers must handle SQS redelivery gracefully.
