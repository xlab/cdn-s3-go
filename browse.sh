#!/bin/bash

# Script to connect to S3 buckets configured in .env using stu

set -e

# Check if stu is installed
if ! command -v stu &> /dev/null; then
    echo "stu not found. Installing via cargo..."
    cargo install --locked stu
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${SCRIPT_DIR}/.env"

if [[ ! -f "$ENV_FILE" ]]; then
    echo "Error: .env file not found at $ENV_FILE"
    exit 1
fi

source "$ENV_FILE"

# Parse bucket and region lists
IFS=',' read -ra BUCKETS <<< "$CDN_BUCKET_PUBLIC_NAMES"
IFS=',' read -ra REGIONS <<< "$CDN_BUCKET_REGION_ALIASES"

# Build list of available bucket+region combinations
declare -a OPTIONS
for bucket in "${BUCKETS[@]}"; do
    for region in "${REGIONS[@]}"; do
        endpoint_var="CDN_BUCKET_ENDPOINT_${bucket}_${region}"
        if [[ -n "${!endpoint_var}" ]]; then
            OPTIONS+=("${bucket}_${region}")
        fi
    done
done

if [[ ${#OPTIONS[@]} -eq 0 ]]; then
    echo "Error: No bucket configurations found in .env"
    exit 1
fi

echo "Available buckets:"
for i in "${!OPTIONS[@]}"; do
    echo "  $((i+1)). ${OPTIONS[$i]}"
done

read -p "Select bucket (1-${#OPTIONS[@]}): " selection

if [[ ! "$selection" =~ ^[0-9]+$ ]] || [[ "$selection" -lt 1 ]] || [[ "$selection" -gt ${#OPTIONS[@]} ]]; then
    echo "Invalid selection"
    exit 1
fi

SELECTED="${OPTIONS[$((selection-1))]}"

# Get configuration for selected bucket
ENDPOINT_VAR="CDN_BUCKET_ENDPOINT_${SELECTED}"
REGION_VAR="CDN_BUCKET_REGION_${SELECTED}"
NAME_VAR="CDN_BUCKET_NAME_${SELECTED}"
PREFIX_VAR="CDN_BUCKET_PATH_PREFIX_${SELECTED}"
ACCESS_KEY_VAR="CDN_BUCKET_ACCESS_KEY_ID_${SELECTED}"
SECRET_KEY_VAR="CDN_BUCKET_SECRET_ACCESS_KEY_${SELECTED}"

ENDPOINT="${!ENDPOINT_VAR}"
REGION="${!REGION_VAR}"
BUCKET_NAME="${!NAME_VAR}"
PREFIX="${!PREFIX_VAR}"
ACCESS_KEY="${!ACCESS_KEY_VAR}"
SECRET_KEY="${!SECRET_KEY_VAR}"

echo ""
echo "Connecting to: $BUCKET_NAME"
echo "  Endpoint: $ENDPOINT"
echo "  Region: $REGION"
[[ -n "$PREFIX" ]] && echo "  Prefix: $PREFIX"
echo ""

# Build stu command
STU_CMD=(stu --endpoint-url "$ENDPOINT" --region "$REGION" --bucket "$BUCKET_NAME" --path-style always)
[[ -n "$PREFIX" ]] && STU_CMD+=(--prefix "$PREFIX")

# Set AWS credentials and run stu
export AWS_ACCESS_KEY_ID="$ACCESS_KEY"
export AWS_SECRET_ACCESS_KEY="$SECRET_KEY"

exec "${STU_CMD[@]}"
