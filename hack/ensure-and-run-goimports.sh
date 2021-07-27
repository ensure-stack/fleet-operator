#!/bin/bash
set -euo pipefail

# this script ensures that the `goimports` dependency is present
# and then executes goimport passing all arguments forward

make -s goimports
.cache/dependencies/bin/goimports -local github.com/ensure-stack/fleet-operator -w -l "$@"
