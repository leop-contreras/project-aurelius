#!/bin/bash

if ! command -v docker > /dev/null 2>&1; then
  echo "docker not found"
  exit 1
fi

if ! docker info > /dev/null 2>&1; then
  echo "docker is installed but not running"
  exit 1
fi

echo "Building and starting containers..."
docker compose up -d --build

echo "Waiting for services..."
sleep 20

echo ""
echo "Checking middleware..."
curl -s http://localhost:8000/status
echo ""

echo ""
echo "Testing endpoints..."
curl -s "http://localhost:8000/instance?id=1"
echo ""

echo ""
echo "Done. Open http://localhost:3000"