#!/usr/bin/env bash
set -euo pipefail
IFS=$'\n\t'

# -------------------------------------------------------------------
# Connektn Stripe Test Data Seeder
# -------------------------------------------------------------------
# Creates realistic test data in Stripe test mode:
# - 50 customers with fake names/emails
# - 5 products with monthly recurring prices
# - Random subscriptions (~90% of customers)
# - Random refunds (~15% of charges)
# All objects tagged with metadata[seeded_by]=ConnektnSeeder
# -------------------------------------------------------------------

NUM_CUSTOMERS=${NUM_CUSTOMERS:-50}
NUM_PRODUCTS=${NUM_PRODUCTS:-5}
SUBSCRIBE_PROBABILITY=${SUBSCRIBE_PROBABILITY:-100}  # Percentage (0-100) - all customers get subscriptions
CANCEL_PROBABILITY=${CANCEL_PROBABILITY:-20}  # Percentage of subscriptions to cancel
REFUND_PERCENT=${REFUND_PERCENT:-15}         # Percentage of charges to refund
PAYMENT_CARD="4242424242424242"
CURRENCY="usd"
REPORT_DIR="seed_reports"

mkdir -p "${REPORT_DIR}"

# -------------------------------------------------------------------
# Counters
# -------------------------------------------------------------------
COUNT_CUSTOMERS=0
COUNT_PRODUCTS=0
COUNT_PRICES=0
COUNT_SUBSCRIPTIONS=0
COUNT_INVOICES=0
COUNT_REFUNDS=0

# -------------------------------------------------------------------
# Helpers
# -------------------------------------------------------------------

log() { printf "\n[+] %s\n" "$*"; }

random_from() {
    local arr=("$@")
    echo "${arr[RANDOM % ${#arr[@]}]}"
}

generate_person() {
    local firsts=(Oliver George Noah Arthur Leo Muhammad Oscar Harry Jacob Thomas Charlie)
    local lasts=(Smith Johnson Williams Brown Jones Garcia Miller Davis Rodriguez Martinez)
    local f=$(random_from "${firsts[@]}")
    local l=$(random_from "${lasts[@]}")
    local num=$((RANDOM % 900 + 100))
    local email="$(echo "${f}.${l}${num}@example.test" | tr '[:upper:]' '[:lower:]')"
    echo "${f} ${l}|${email}"
}

# -------------------------------------------------------------------
# Pre-flight checks
# -------------------------------------------------------------------

log "🔍 Checking environment..."

if [[ -z "${STRIPE_API_KEY:-}" ]]; then
    echo "❌ STRIPE_API_KEY not set. Please export it first:"
    echo "   export STRIPE_API_KEY=sk_test_123..."
    exit 1
fi

if [[ "${STRIPE_API_KEY}" != sk_test_* ]]; then
    echo "❌ STRIPE_API_KEY must be a TEST key (sk_test_...)."
    echo "   Current key starts with: ${STRIPE_API_KEY:0:7}"
    exit 1
fi

if ! command -v stripe >/dev/null 2>&1; then
    echo "❌ Stripe CLI not found. Install it: https://stripe.com/docs/stripe-cli"
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "❌ jq not found. Install jq first."
    exit 1
fi

log "✅ Environment validated (using TEST mode key)"

# -------------------------------------------------------------------
# Create products + recurring monthly prices
# -------------------------------------------------------------------
log "Creating $NUM_PRODUCTS products with monthly prices..."
PRICE_IDS=()

for i in $(seq 1 $NUM_PRODUCTS); do
    NAME="Pro Plan $i"
    AMOUNT=$(( (RANDOM % 9000) + 1000 ))  # $10.00–$99.99

    PROD_JSON=$(stripe products create --api-key "$STRIPE_API_KEY" --name "$NAME" -d 'metadata[seeded_by]=ConnektnSeeder')
    PROD_ID=$(echo "$PROD_JSON" | jq -r '.id')

    PRICE_JSON=$(stripe prices create --api-key "$STRIPE_API_KEY" --product "$PROD_ID" --unit-amount "$AMOUNT" --currency "$CURRENCY" --recurring.interval month -d 'metadata[seeded_by]=ConnektnSeeder')
    PRICE_ID=$(echo "$PRICE_JSON" | jq -r '.id')

    PRICE_IDS+=("$PRICE_ID")
    COUNT_PRODUCTS=$((COUNT_PRODUCTS + 1))
    COUNT_PRICES=$((COUNT_PRICES + 1))

    # Calculate dollars (bash integer arithmetic)
    DOLLARS=$((AMOUNT / 100))
    CENTS=$((AMOUNT % 100))
    log "📦 Product: $PROD_ID → Price: $PRICE_ID (\$${DOLLARS}.$(printf "%02d" $CENTS))"
done

printf '%s\n' "${PRICE_IDS[@]}" > "${REPORT_DIR}/price_ids.txt"

# -------------------------------------------------------------------
# Create customers + payment methods + subscriptions
# -------------------------------------------------------------------
log "Creating $NUM_CUSTOMERS customers with subscriptions..."
CUSTOMER_IDS=()
SUBSCRIPTION_IDS=()

for i in $(seq 1 $NUM_CUSTOMERS); do
    person=$(generate_person)
    name="${person%%|*}"
    email="${person##*|}"

    # Create customer with test payment method attached
    C_JSON=$(stripe customers create --api-key "$STRIPE_API_KEY" --name "$name" --email "$email" -d "source=tok_visa" -d 'metadata[seeded_by]=ConnektnSeeder')
    C_ID=$(echo "$C_JSON" | jq -r '.id')
    CUSTOMER_IDS+=("$C_ID")
    COUNT_CUSTOMERS=$((COUNT_CUSTOMERS + 1))

    # Randomly subscribe (based on probability percentage)
    random_num=$((RANDOM % 100))
    if (( random_num < SUBSCRIBE_PROBABILITY )); then
        PRICE=${PRICE_IDS[$((RANDOM % ${#PRICE_IDS[@]}))]}

        # 30% trialing, 70% active
        trial_num=$((RANDOM % 100))
        if (( trial_num < 30 )); then
            # Create subscription with trial period
            SUB_JSON=$(stripe subscriptions create --api-key "$STRIPE_API_KEY" --customer "$C_ID" -d "items[0][price]=$PRICE" -d "trial_period_days=30" -d 'metadata[seeded_by]=ConnektnSeeder')
            STATUS_MSG="trialing"
        else
            # Create active subscription (payment method is already attached via source=tok_visa)
            SUB_JSON=$(stripe subscriptions create --api-key "$STRIPE_API_KEY" --customer "$C_ID" -d "items[0][price]=$PRICE" -d 'metadata[seeded_by]=ConnektnSeeder')
            STATUS_MSG="active"
        fi

        SUB_ID=$(echo "$SUB_JSON" | jq -r '.id')
        SUBSCRIPTION_IDS+=("$SUB_ID")
        COUNT_SUBSCRIPTIONS=$((COUNT_SUBSCRIPTIONS + 1))

        log "💳 Customer $i/$NUM_CUSTOMERS: $C_ID → Subscribed to $PRICE ($STATUS_MSG)"
    else
        log "⚪ Customer $i/$NUM_CUSTOMERS: $C_ID (no subscription)"
    fi

    sleep 0.05  # Rate limiting (faster for larger batches)
done

printf '%s\n' "${CUSTOMER_IDS[@]}" > "${REPORT_DIR}/customers.txt"
printf '%s\n' "${SUBSCRIPTION_IDS[@]}" > "${REPORT_DIR}/subscriptions.txt"

# -------------------------------------------------------------------
# Count invoices created
# -------------------------------------------------------------------
log "Counting invoices created..."
INVOICES_JSON=$(stripe invoices list --api-key "$STRIPE_API_KEY" --limit 100)
COUNT_INVOICES=$(echo "$INVOICES_JSON" | jq '[.data[] | select(.metadata.seeded_by == "ConnektnSeeder")] | length')

# -------------------------------------------------------------------
# Refund random subset of charges
# -------------------------------------------------------------------
log "Fetching charges for refunds..."
CHARGES_JSON=$(stripe charges list --api-key "$STRIPE_API_KEY" --limit 100)

# Filter charges that have metadata.seeded_by (from our subscriptions)
CHARGE_IDS=($(echo "$CHARGES_JSON" | jq -r '.data[].id'))
NUM_CHARGES=${#CHARGE_IDS[@]}

if (( NUM_CHARGES == 0 )); then
    log "⚠️  No charges found to refund (subscriptions may not have been billed yet)"
else
    NUM_REFUNDS=$(( NUM_CHARGES * REFUND_PERCENT / 100 ))
    if (( NUM_REFUNDS < 1 )); then NUM_REFUNDS=1; fi

    log "Found $NUM_CHARGES charges → refunding $NUM_REFUNDS (~${REFUND_PERCENT}%)..."

    # Shuffle and take first NUM_REFUNDS charges
    for i in $(seq 1 $NUM_REFUNDS); do
        if (( i > NUM_CHARGES )); then break; fi

        CH_ID=${CHARGE_IDS[$((RANDOM % NUM_CHARGES))]}

        CH_JSON=$(stripe charges retrieve "$CH_ID" --api-key "$STRIPE_API_KEY")
        AMOUNT=$(echo "$CH_JSON" | jq -r '.amount')
        ALREADY_REFUNDED=$(echo "$CH_JSON" | jq -r '.refunded')

        if [[ "$ALREADY_REFUNDED" == "true" ]]; then
            log "⏭️  Skipping $CH_ID (already refunded)"
            continue
        fi

        # 50/50 chance of full vs partial refund
        if (( RANDOM % 2 == 0 )); then
            stripe refunds create \
                --api-key "$STRIPE_API_KEY" \
                --charge "$CH_ID" \
                -d 'metadata[seeded_by]=ConnektnSeeder' \
                >/dev/null 2>&1
            COUNT_REFUNDS=$((COUNT_REFUNDS + 1))
            DOLLARS=$((AMOUNT / 100))
            CENTS=$((AMOUNT % 100))
            log "💸 Full refund: $CH_ID (\$${DOLLARS}.$(printf "%02d" $CENTS))"
        else
            PART=$(( AMOUNT * ((RANDOM % 40 + 30)) / 100 ))
            stripe refunds create \
                --api-key "$STRIPE_API_KEY" \
                --charge "$CH_ID" \
                --amount "$PART" \
                -d 'metadata[seeded_by]=ConnektnSeeder' \
                >/dev/null 2>&1
            COUNT_REFUNDS=$((COUNT_REFUNDS + 1))
            PART_DOLLARS=$((PART / 100))
            PART_CENTS=$((PART % 100))
            TOTAL_DOLLARS=$((AMOUNT / 100))
            TOTAL_CENTS=$((AMOUNT % 100))
            log "💸 Partial refund: $CH_ID (\$${PART_DOLLARS}.$(printf "%02d" $PART_CENTS)/\$${TOTAL_DOLLARS}.$(printf "%02d" $TOTAL_CENTS))"
        fi

        sleep 0.05  # Rate limiting (faster for larger batches)
    done
fi

# -------------------------------------------------------------------
# Save summary report
# -------------------------------------------------------------------
log "Generating summary report..."

cat > "${REPORT_DIR}/summary.json" <<EOF
{
  "seeded_by": "ConnektnSeeder",
  "timestamp": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")",
  "counts": {
    "customers": $COUNT_CUSTOMERS,
    "products": $COUNT_PRODUCTS,
    "prices": $COUNT_PRICES,
    "subscriptions": $COUNT_SUBSCRIPTIONS,
    "invoices": $COUNT_INVOICES,
    "refunds": $COUNT_REFUNDS
  },
  "stripe_test_key": "${STRIPE_API_KEY:0:20}..."
}
EOF

# -------------------------------------------------------------------
# Final summary
# -------------------------------------------------------------------
log "✅ Seeding complete!"
echo ""
echo "📊 Summary:"
echo "   Customers:     $COUNT_CUSTOMERS"
echo "   Products:      $COUNT_PRODUCTS"
echo "   Prices:        $COUNT_PRICES"
echo "   Subscriptions: $COUNT_SUBSCRIPTIONS"
echo "   Invoices:      $COUNT_INVOICES"
echo "   Refunds:       $COUNT_REFUNDS"
echo ""
echo "💾 Reports saved in: ${REPORT_DIR}/"
echo "   - summary.json"
echo "   - customers.txt"
echo "   - subscriptions.txt"
echo "   - price_ids.txt"
echo ""
log "🎯 All objects tagged with metadata[seeded_by]=ConnektnSeeder for easy cleanup"
