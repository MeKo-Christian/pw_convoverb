#!/bin/bash
# Convert all assets/*.aif to ir-library.irlib
#
# Usage:
#   ./scripts/convert-assets.sh
#
# This script builds the ir-convert tool and uses it to convert
# all AIFF files in the assets directory to a single IR library file.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$PROJECT_ROOT"

echo "Building ir-convert..."
go build -o ./ir-convert ./cmd/ir-convert

echo "Converting assets..."
./ir-convert -verbose -category "Default" ./assets ./ir-library.irlib

echo ""
echo "Done! Library created at: ./ir-library.irlib"
echo "You can now use it with pw-convoverb:"
echo "  ./pw-convoverb -ir-library ./ir-library.irlib -ir-name \"Large Hall\""

# Clean up
rm -f ./ir-convert
