#!/bin/bash
set -e

# Connektn Linker Agent Runner
# This script sets up required environment variables and runs the agent

echo "🚀 Starting Connektn Linker Agent..."
echo ""

# Check if STRIPE_API_KEY is set
if [ -z "$STRIPE_API_KEY" ]; then
    echo "❌ ERROR: STRIPE_API_KEY environment variable is not set"
    echo ""
    echo "Please set your Stripe API key:"
    echo "  export STRIPE_API_KEY='sk_test_...'"
    echo ""
    echo "Or run this script with:"
    echo "  STRIPE_API_KEY='sk_test_...' ./run.sh"
    echo ""
    exit 1
fi

# Generate a random tenant salt if not set
if [ -z "$TENANT_SALT" ]; then
    export TENANT_SALT=$(openssl rand -hex 32)
    echo "🔐 Generated TENANT_SALT: $TENANT_SALT"
else
    echo "🔐 Using existing TENANT_SALT"
fi

echo ""
echo "▶️  Running linker agent..."
echo ""

# Run the agent
go run main.go

echo ""
echo "✨ Done!"