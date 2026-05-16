#!/bin/bash
set -e

# Clean up any existing server on port 8765
lsof -ti:8765 | xargs kill -9 2>/dev/null || true

# Start server in background with a timeout to check if it starts successfully
python3 server.py &
SERVER_PID=$!

# Give server time to start, check if it's still running
sleep 2
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "FAIL: Server failed to start"
    exit 1
fi

# Function to check response
check_response() {
    local url="$1"
    local expected_status="$2"
    local expected_body="$3"
    local test_name="$4"
    
    echo "Testing $test_name..."
    response=$(curl -s -w "%{http_code}" "$url" -o /tmp/response_body)
    status_code="${response: -3}"
    body=$(cat /tmp/response_body)
    
    if [ "$status_code" -eq "$expected_status" ] && [ "$body" = "$expected_body" ]; then
        echo "  ✓ $test_name passed"
        return 0
    else
        echo "  ✗ $test_name failed"
        echo "    Expected status: $expected_status, got: $status_code"
        echo "    Expected body: '$expected_body', got: '$body'"
        return 1
    fi
}

FAILED=0

# Test 1: /ping endpoint
check_response "http://127.0.0.1:8765/ping" 200 "pong" "/ping endpoint" || FAILED=1

# Test 2: /sum endpoint with valid parameters
check_response "http://127.0.0.1:8765/sum?a=5&b=3" 200 "8" "/sum with valid params" || FAILED=1

# Test 3: /sum endpoint with missing parameters (should return 400)
check_response "http://127.0.0.1:8765/sum?a=5" 400 "Missing parameters" "/sum with missing params" || FAILED=1

# Kill server
kill $SERVER_PID 2>/dev/null || true
wait $SERVER_PID 2>/dev/null || true

if [ $FAILED -eq 0 ]; then
    echo "OK"
    exit 0
else
    echo "FAIL"
    exit 1
fi