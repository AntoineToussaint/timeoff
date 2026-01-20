#!/bin/bash
set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$PROJECT_DIR"

# Cleanup function
cleanup() {
    echo -e "\n${YELLOW}Shutting down...${NC}"
    kill $BACKEND_PID 2>/dev/null || true
    kill $FRONTEND_PID 2>/dev/null || true
    exit 0
}

trap cleanup SIGINT SIGTERM

echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
echo -e "${BLUE}║      Time Resource Engine - Development Mode       ║${NC}"
echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
echo ""

# Check dependencies
echo -e "${YELLOW}Checking dependencies...${NC}"

if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed${NC}"
    exit 1
fi

if ! command -v node &> /dev/null; then
    echo -e "${RED}Error: Node.js is not installed${NC}"
    exit 1
fi

# Check for air (hot-reload)
HAS_AIR=false
if command -v air &> /dev/null; then
    HAS_AIR=true
    echo -e "${GREEN}✓ Air (hot-reload) available${NC}"
fi

echo -e "${GREEN}✓ Go $(go version | awk '{print $3}')${NC}"
echo -e "${GREEN}✓ Node $(node --version)${NC}"
echo ""

# Install frontend dependencies if needed
if [ ! -d "web/node_modules" ]; then
    echo -e "${YELLOW}Installing frontend dependencies...${NC}"
    cd web && npm install && cd ..
    echo ""
fi

# Create data directory
mkdir -p ./data ./tmp

# Database option
DB_PATH="${DB_PATH:-./data/warp.db}"
if [ "$1" = "--memory" ] || [ "$1" = "-m" ]; then
    DB_PATH=":memory:"
    echo -e "${YELLOW}Using in-memory database (data will not persist)${NC}"
else
    echo -e "${YELLOW}Using persistent database: ${DB_PATH}${NC}"
fi
echo ""

# Start backend (with or without hot-reload)
if [ "$HAS_AIR" = true ] && [ "$1" != "--no-reload" ]; then
    echo -e "${CYAN}Starting backend with hot-reload (air)...${NC}"
    echo -e "${CYAN}Changes to .go files will auto-rebuild!${NC}"
    air &
    BACKEND_PID=$!
else
    echo -e "${YELLOW}Building backend...${NC}"
    go build -o ./bin/server ./cmd/server
    echo -e "${GREEN}✓ Backend built${NC}"
    echo -e "${YELLOW}Starting backend server...${NC}"
    ./bin/server -db="$DB_PATH" &
    BACKEND_PID=$!
fi

sleep 2

if ! kill -0 $BACKEND_PID 2>/dev/null; then
    echo -e "${RED}Failed to start backend server${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Backend running on http://localhost:8080${NC}"

# Start frontend
echo -e "${YELLOW}Starting frontend dev server...${NC}"
cd web && npm run dev &
FRONTEND_PID=$!
cd ..
sleep 3

if ! kill -0 $FRONTEND_PID 2>/dev/null; then
    echo -e "${RED}Failed to start frontend server${NC}"
    kill $BACKEND_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✓ Frontend running on http://localhost:5173${NC}"
echo ""

if [ "$HAS_AIR" = true ] && [ "$1" != "--no-reload" ]; then
    echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║              ${CYAN}🔥 HOT-RELOAD ENABLED 🔥${BLUE}              ║${NC}"
    echo -e "${BLUE}╠════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║  Frontend:  ${GREEN}http://localhost:5173${BLUE}                 ║${NC}"
    echo -e "${BLUE}║  Backend:   ${GREEN}http://localhost:8080${BLUE}                 ║${NC}"
    echo -e "${BLUE}║  API:       ${GREEN}http://localhost:8080/api${BLUE}             ║${NC}"
    echo -e "${BLUE}╠════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║  ${CYAN}Go changes auto-rebuild! React changes hot-reload!${BLUE} ║${NC}"
    echo -e "${BLUE}║  Press ${YELLOW}Ctrl+C${BLUE} to stop all servers               ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
else
    echo -e "${BLUE}╔════════════════════════════════════════════════════╗${NC}"
    echo -e "${BLUE}║                   Ready to use!                    ║${NC}"
    echo -e "${BLUE}╠════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║  Frontend:  ${GREEN}http://localhost:5173${BLUE}                 ║${NC}"
    echo -e "${BLUE}║  Backend:   ${GREEN}http://localhost:8080${BLUE}                 ║${NC}"
    echo -e "${BLUE}║  API:       ${GREEN}http://localhost:8080/api${BLUE}             ║${NC}"
    echo -e "${BLUE}╠════════════════════════════════════════════════════╣${NC}"
    echo -e "${BLUE}║  Press ${YELLOW}Ctrl+C${BLUE} to stop all servers               ║${NC}"
    echo -e "${BLUE}╚════════════════════════════════════════════════════╝${NC}"
fi
echo ""

# Wait for processes
wait
