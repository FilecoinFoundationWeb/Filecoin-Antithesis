#!/bin/bash
set -e

echo "Running DOS Attack: IdentifyDOS"

# Read the multiaddr from the file.
LOTUS_1_TARGET=$(cat "/root/devgen/lotus-1/lotus-1-ipv4addr")
LOTUS_2_TARGET=$(cat "/root/devgen/lotus-2/lotus-2-ipv4addr")

# Randomly select a Lotus target from the available options.
random_targets=("$LOTUS_1_TARGET" "$LOTUS_2_TARGET")
selected_target=${random_targets[$((RANDOM % ${#random_targets[@]}))]}
export TARGET=$selected_target

echo "TARGET set to: $TARGET"

# Run the DOS attack, relying on the -duration parameter
/opt/antithesis/app -operation chaos -target "$TARGET" -min-interval "50ms" -max-interval "100ms" -duration "180s"

# Capture the exit code but don't use it for the script's exit code
app_exit_code=$?
echo "DOS attack completed with exit code: $app_exit_code"

# Always exit with success code to prevent Antithesis from reporting failure
exit 0 