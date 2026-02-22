#!/bin/bash
trap "kill 0" EXIT

# Build
go build -o example/server example/main.go

# Start servers
echo "Starting servers..."
./example/server -port 8001 &
PID1=$!
./example/server -port 8002 &
PID2=$!
./example/server -port 8003 &
PID3=$!

sleep 2
echo "Servers started."

echo ">>> Requesting Tom (hit 8001)"
curl "http://localhost:8001/api?key=Tom"
echo ""

echo ">>> Requesting Tom again (hit 8001 - cached)"
curl "http://localhost:8001/api?key=Tom"
echo ""

echo ">>> Requesting Jack (hit 8001 -> forward to peer)"
curl "http://localhost:8001/api?key=Jack"
echo ""

echo ">>> Requesting Sam (hit 8002)"
curl "http://localhost:8002/api?key=Sam"
echo ""

# Kill servers
kill $PID1 $PID2 $PID3
