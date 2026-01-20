.PHONY: dev run build test clean install frontend backend help air

# Default target
help:
	@echo "Time Resource Engine - Available Commands"
	@echo ""
	@echo "  Development:"
	@echo "    make dev          - Run both servers with hot-reload (if air installed)"
	@echo "    make dev-memory   - Run with in-memory database"
	@echo "    make dev-no-reload- Run without hot-reload"
	@echo "    make backend      - Run only the backend server"
	@echo "    make frontend     - Run only the frontend dev server"
	@echo "    make air          - Run backend with hot-reload only"
	@echo ""
	@echo "  Building:"
	@echo "    make build        - Build both backend and frontend for production"
	@echo "    make build-backend- Build only backend"
	@echo "    make build-frontend- Build only frontend"
	@echo ""
	@echo "  Testing:"
	@echo "    make test         - Run all tests (verbose)"
	@echo "    make test-short   - Run all tests (minimal output)"
	@echo "    make test-race    - Run tests with race detector"
	@echo ""
	@echo "  Setup:"
	@echo "    make install      - Install all dependencies"
	@echo "    make install-air  - Install air hot-reload tool"
	@echo "    make clean        - Remove build artifacts"
	@echo ""

# Development mode - both servers with hot-reload
dev:
	@./run.sh

dev-memory:
	@./run.sh --memory

dev-no-reload:
	@./run.sh --no-reload

# Run backend with air hot-reload only
air:
	@echo "Starting backend with hot-reload..."
	@mkdir -p data tmp
	@air

# Run only backend (no hot-reload)
backend:
	@echo "Starting backend server..."
	@mkdir -p data
	@go run ./cmd/server/... -db="./data/warp.db"

backend-memory:
	@echo "Starting backend server (in-memory)..."
	@go run ./cmd/server/... -db=":memory:"

# Run only frontend
frontend:
	@echo "Starting frontend dev server..."
	@cd web && npm run dev

# Build for production
build: build-backend build-frontend
	@echo "Build complete!"

build-backend:
	@echo "Building backend..."
	@mkdir -p bin
	@go build -o bin/server ./cmd/server
	@echo "Backend built: bin/server"

build-frontend:
	@echo "Building frontend..."
	@cd web && npm run build
	@echo "Frontend built: web/dist/"

# Run tests
test:
	@echo "Running Go tests..."
	@go test ./... -v -count=1

test-short:
	@go test ./... -count=1

test-race:
	@echo "Running tests with race detector..."
	@go test ./... -race -count=1

test-cover:
	@echo "Running tests with coverage..."
	@go test ./... -coverprofile=coverage.out
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Install dependencies
install: install-go install-frontend
	@echo "All dependencies installed!"

install-go:
	@echo "Installing Go dependencies..."
	@go mod download

install-frontend:
	@echo "Installing frontend dependencies..."
	@cd web && npm install

install-air:
	@echo "Installing air hot-reload tool..."
	@go install github.com/cosmtrek/air@latest || brew install air
	@echo "Air installed! Run 'make air' or 'make dev' to use it."

# Clean build artifacts
clean:
	@rm -rf bin/
	@rm -rf tmp/
	@rm -rf web/dist/
	@rm -f *.db
	@rm -f data/*.db
	@rm -f coverage.out coverage.html
	@rm -f build-errors.log
	@echo "Cleaned!"

clean-all: clean
	@rm -rf web/node_modules/
	@echo "Cleaned including node_modules!"

# Quick demo - load scenario and open browser
demo: dev
	@sleep 5
	@curl -s -X POST http://localhost:8080/api/scenarios/load -d '{"scenario_id":"new-parent"}' -H "Content-Type: application/json"
	@open http://localhost:5173 2>/dev/null || xdg-open http://localhost:5173 2>/dev/null || echo "Open http://localhost:5173 in your browser"
