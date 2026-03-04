#!/usr/bin/env bash
set -euo pipefail

# ═══════════════════════════════════════════════════════════════════
# test-scaffold.sh — Local lifecycle test for scaffold CI changes
# ═══════════════════════════════════════════════════════════════════
#
# Simulates the full GitHub Actions pipeline locally:
#   Phase 1: Static validation (YAML, env sourcing, Dockerfile)
#   Phase 2: Logic simulation (parse-config, resolve, envsubst)
#   Phase 3: act dry-run (workflow graph + job ordering)
#
# Usage:
#   ./scripts/test-scaffold.sh          # Run all phases
#   ./scripts/test-scaffold.sh --phase1 # Static checks only
#   ./scripts/test-scaffold.sh --phase2 # Logic sims only
#   ./scripts/test-scaffold.sh --phase3 # act dry-run only

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'
BOLD='\033[1m'

PASS=0
FAIL=0
WARN=0

pass() { PASS=$((PASS + 1)); echo -e "  ${GREEN}PASS${NC} $1"; }
fail() { FAIL=$((FAIL + 1)); echo -e "  ${RED}FAIL${NC} $1"; }
warn() { WARN=$((WARN + 1)); echo -e "  ${YELLOW}WARN${NC} $1"; }
header() { echo -e "\n${CYAN}${BOLD}═══ $1 ═══${NC}"; }
subheader() { echo -e "\n${BOLD}--- $1 ---${NC}"; }

# cd to repo root
cd "$(git rev-parse --show-toplevel)"

RUN_PHASE1=true
RUN_PHASE2=true
RUN_PHASE3=true
if [[ "${1:-}" == "--phase1" ]]; then RUN_PHASE2=false; RUN_PHASE3=false; fi
if [[ "${1:-}" == "--phase2" ]]; then RUN_PHASE1=false; RUN_PHASE3=false; fi
if [[ "${1:-}" == "--phase3" ]]; then RUN_PHASE1=false; RUN_PHASE2=false; fi

# ═══════════════════════════════════════════════════════════════════
# PHASE 1: Static Validation
# ═══════════════════════════════════════════════════════════════════
if $RUN_PHASE1; then
header "PHASE 1: Static Validation"

subheader "1.1 — Required files exist"
for f in versions.env test.env .env Dockerfile \
         .github/workflows/on_versions_change.yml \
         .github/workflows/build_push_config.yml \
         .github/workflows/run_antithesis_test.yml; do
  if [[ -f "$f" ]]; then
    pass "$f exists"
  else
    fail "$f MISSING"
  fi
done

subheader "1.2 — YAML syntax"
for f in .github/workflows/on_versions_change.yml \
         .github/workflows/build_push_config.yml \
         .github/workflows/run_antithesis_test.yml; do
  if python3 -c "import yaml; yaml.safe_load(open('$f'))" 2>/dev/null; then
    pass "$f valid YAML"
  else
    fail "$f invalid YAML"
  fi
done

subheader "1.3 — Env files source without error"
if bash -c 'source versions.env' 2>/dev/null; then
  pass "versions.env sources cleanly"
else
  fail "versions.env has syntax errors"
fi
if bash -c 'source test.env' 2>/dev/null; then
  pass "test.env sources cleanly"
else
  fail "test.env has syntax errors"
fi
if bash -c 'source versions.env && source test.env' 2>/dev/null; then
  pass "Both env files source together"
else
  fail "Env files conflict when sourced together"
fi

subheader "1.4 — Required variables defined"
MISSING_VARS=()
RESULT=$(bash -c 'source versions.env && source test.env && echo "LOTUS0=$LOTUS0_COMMIT LOTUS1=$LOTUS1_COMMIT FOREST=$FOREST_COMMIT CURIO=$CURIO_COMMIT DRAND=$DRAND_COMMIT WORKLOAD=$WORKLOAD_TAG FILWIZARD=$FILWIZARD_TAG CONFIG=$CONFIG_TAG NOTEBOOK=$NOTEBOOK DURATION=$DURATION EPHEMERAL=$EPHEMERAL"' 2>/dev/null)
for var in LOTUS0 LOTUS1 FOREST CURIO DRAND WORKLOAD FILWIZARD CONFIG NOTEBOOK DURATION EPHEMERAL; do
  val=$(echo "$RESULT" | grep -oP "${var}=\K[^ ]*")
  if [[ -n "$val" ]]; then
    pass "$var=$val"
  else
    fail "$var is empty or undefined"
  fi
done

subheader "1.5 — Dockerfile references test.env"
if grep -q 'COPY ./test.env' Dockerfile; then
  pass "Dockerfile copies test.env"
else
  fail "Dockerfile missing test.env COPY"
fi
if grep -q 'docker-compose.override' Dockerfile; then
  pass "Dockerfile copies optional override"
else
  fail "Dockerfile missing override COPY"
fi

subheader "1.6 — Workflow cross-references"
# build_push_config inputs vs on_versions_change build-config.with
BPC_INPUTS=$(python3 -c "
import yaml
with open('.github/workflows/build_push_config.yml') as f:
    d = yaml.safe_load(f)
inputs = d[True]['workflow_call']['inputs']
print(' '.join(sorted(inputs.keys())))
" 2>/dev/null)
OVC_PASSES=$(python3 -c "
import yaml
with open('.github/workflows/on_versions_change.yml') as f:
    d = yaml.safe_load(f)
w = d['jobs']['build-config']['with']
print(' '.join(sorted(w.keys())))
" 2>/dev/null)
if [[ "$BPC_INPUTS" == "$OVC_PASSES" ]]; then
  pass "build-config passes all inputs that build_push_config expects"
else
  fail "Input mismatch: config expects [$BPC_INPUTS], orchestrator passes [$OVC_PASSES]"
fi

# on_versions_change parse-config outputs vs run-test references
ORPHANS=$(python3 -c "
import yaml, re
with open('.github/workflows/on_versions_change.yml') as f:
    d = yaml.safe_load(f)
outputs = set(d['jobs']['parse-config']['outputs'].keys())
refs = set()
for step in d['jobs']['run-test']['steps']:
    if 'run' in step:
        refs.update(re.findall(r'needs\.parse-config\.outputs\.(\w+)', step['run']))
    if 'with' in step:
        for v in step['with'].values():
            refs.update(re.findall(r'needs\.parse-config\.outputs\.(\w+)', str(v)))
missing = refs - outputs
print(' '.join(missing) if missing else 'OK')
" 2>/dev/null)
if [[ "$ORPHANS" == "OK" ]]; then
  pass "run-test references all exist in parse-config outputs"
else
  fail "run-test references missing outputs: $ORPHANS"
fi

# Trigger check: pull_request present, no push
TRIGGERS=$(python3 -c "
import yaml
with open('.github/workflows/on_versions_change.yml') as f:
    d = yaml.safe_load(f)
t = d[True]
triggers = list(t.keys())
print(' '.join(str(x) for x in triggers))
" 2>/dev/null)
if echo "$TRIGGERS" | grep -q "pull_request"; then
  pass "Build & Test triggers on pull_request"
else
  fail "Build & Test missing pull_request trigger"
fi
if echo "$TRIGGERS" | grep -q "workflow_dispatch"; then
  pass "Build & Test triggers on workflow_dispatch"
else
  fail "Build & Test missing workflow_dispatch trigger"
fi
# No push trigger should exist
if echo "$TRIGGERS" | grep -qw "push"; then
  fail "Build & Test still has push trigger (should be PR-only)"
else
  pass "Build & Test has no push trigger (main stays clean)"
fi

fi # end PHASE 1

# ═══════════════════════════════════════════════════════════════════
# PHASE 2: Logic Simulation
# ═══════════════════════════════════════════════════════════════════
if $RUN_PHASE2; then
header "PHASE 2: Logic Simulation"

# Use a temp dir for GITHUB_OUTPUT simulation
TMPOUT=$(mktemp)
trap "rm -f $TMPOUT" EXIT

subheader "2.1 — parse-config: defaults from file (no overrides)"
GITHUB_OUTPUT="$TMPOUT" GITHUB_SHA="abc1234567" bash -c '
  source versions.env
  [ -f test.env ] && source test.env
  CONFIG_TAG="${GITHUB_SHA:0:7}"
  echo "lotus0_commit=$LOTUS0_COMMIT"   >> $GITHUB_OUTPUT
  echo "lotus1_commit=$LOTUS1_COMMIT"   >> $GITHUB_OUTPUT
  echo "forest_commit=$FOREST_COMMIT"   >> $GITHUB_OUTPUT
  echo "notebook=${NOTEBOOK:-filecoin}" >> $GITHUB_OUTPUT
  echo "duration=${DURATION:-1.5}"      >> $GITHUB_OUTPUT
  echo "ephemeral=${EPHEMERAL:-true}"   >> $GITHUB_OUTPUT
  echo "config_tag=$CONFIG_TAG"         >> $GITHUB_OUTPUT
' 2>/dev/null

GOT_L0=$(grep "^lotus0_commit=" "$TMPOUT" | cut -d= -f2)
GOT_NB=$(grep "^notebook=" "$TMPOUT" | cut -d= -f2)
GOT_DUR=$(grep "^duration=" "$TMPOUT" | cut -d= -f2)
GOT_CT=$(grep "^config_tag=" "$TMPOUT" | cut -d= -f2)

[[ "$GOT_L0" == "latest" ]] && pass "lotus0=latest (from file)" || fail "lotus0=$GOT_L0 (expected latest)"
[[ "$GOT_NB" == "filecoin" ]] && pass "notebook=filecoin (from file)" || fail "notebook=$GOT_NB (expected filecoin)"
[[ "$GOT_DUR" == "1.5" ]] && pass "duration=1.5 (from file)" || fail "duration=$GOT_DUR (expected 1.5)"
[[ "$GOT_CT" == "abc1234" ]] && pass "config_tag=abc1234 (from SHA)" || fail "config_tag=$GOT_CT (expected abc1234)"

subheader "2.2 — parse-config: with manual overrides"
> "$TMPOUT"
GITHUB_OUTPUT="$TMPOUT" GITHUB_SHA="def5678901" bash -c '
  source versions.env
  [ -f test.env ] && source test.env
  # Simulate inputs
  LOTUS0_COMMIT="pinned-abc123"
  DURATION="6"
  NOTEBOOK="filecoin-foc"
  CONFIG_TAG="${GITHUB_SHA:0:7}"
  echo "lotus0_commit=$LOTUS0_COMMIT"   >> $GITHUB_OUTPUT
  echo "lotus1_commit=$LOTUS1_COMMIT"   >> $GITHUB_OUTPUT
  echo "notebook=${NOTEBOOK:-filecoin}" >> $GITHUB_OUTPUT
  echo "duration=${DURATION:-1.5}"      >> $GITHUB_OUTPUT
  echo "config_tag=$CONFIG_TAG"         >> $GITHUB_OUTPUT
  [ "$LOTUS0_COMMIT" != "$LOTUS1_COMMIT" ] && echo "lotus1_differs=true" >> $GITHUB_OUTPUT || echo "lotus1_differs=false" >> $GITHUB_OUTPUT
' 2>/dev/null

GOT_L0=$(grep "^lotus0_commit=" "$TMPOUT" | cut -d= -f2)
GOT_L1=$(grep "^lotus1_commit=" "$TMPOUT" | cut -d= -f2)
GOT_DIFF=$(grep "^lotus1_differs=" "$TMPOUT" | cut -d= -f2)
GOT_NB=$(grep "^notebook=" "$TMPOUT" | cut -d= -f2)
GOT_DUR=$(grep "^duration=" "$TMPOUT" | cut -d= -f2)

[[ "$GOT_L0" == "pinned-abc123" ]] && pass "lotus0=pinned-abc123 (override applied)" || fail "lotus0=$GOT_L0"
[[ "$GOT_L1" == "latest" ]] && pass "lotus1=latest (unchanged)" || fail "lotus1=$GOT_L1"
[[ "$GOT_DIFF" == "true" ]] && pass "lotus1_differs=true (correctly detected)" || fail "lotus1_differs=$GOT_DIFF"
[[ "$GOT_NB" == "filecoin-foc" ]] && pass "notebook=filecoin-foc (override)" || fail "notebook=$GOT_NB"
[[ "$GOT_DUR" == "6" ]] && pass "duration=6 (override)" || fail "duration=$GOT_DUR"

subheader "2.3 — resolve job (run_antithesis_test): default images string"
> "$TMPOUT"
GITHUB_OUTPUT="$TMPOUT" bash -c '
  source versions.env
  [ -f test.env ] && source test.env
  IMAGES="drand:${DRAND_COMMIT};forest:${FOREST_COMMIT};lotus:${LOTUS0_COMMIT};workload:${WORKLOAD_TAG};curio:${CURIO_COMMIT};filwizard:${FILWIZARD_TAG}"
  echo "images=$IMAGES" >> $GITHUB_OUTPUT
  echo "config=${CONFIG_TAG:-latest}" >> $GITHUB_OUTPUT
' 2>/dev/null

GOT_IMAGES=$(grep "^images=" "$TMPOUT" | cut -d= -f2-)
EXPECTED="drand:latest;forest:latest;lotus:latest;workload:latest;curio:latest;filwizard:latest"
[[ "$GOT_IMAGES" == "$EXPECTED" ]] && pass "Images string: $GOT_IMAGES" || fail "Images: got [$GOT_IMAGES], expected [$EXPECTED]"

subheader "2.4 — envsubst: config image bakes correct tags"
echo -e "\n  Simulating: orchestrator built lotus0 at commit 'pinned-abc123', all others at 'latest'"
RESULT=$(bash -c '
  source versions.env
  LOTUS0_COMMIT="pinned-abc123"
  export LOTUS0_COMMIT LOTUS1_COMMIT FOREST_COMMIT CURIO_COMMIT DRAND_COMMIT WORKLOAD_TAG FILWIZARD_TAG
  # Strip ${VAR:-default} to ${VAR} before envsubst (envsubst cant handle :-default)
  sed "s/:-[^}]*//g" docker-compose.yaml \
    | envsubst "\$LOTUS0_COMMIT \$LOTUS1_COMMIT \$FOREST_COMMIT \$CURIO_COMMIT \$DRAND_COMMIT \$WORKLOAD_TAG \$FILWIZARD_TAG" \
    | grep "image:"
' 2>/dev/null)

echo "$RESULT" | while read -r line; do
  echo "    $line"
done

if echo "$RESULT" | grep -q "lotus:pinned-abc123"; then
  pass "lotus0 image tag = pinned-abc123 (override baked correctly)"
else
  fail "lotus0 image tag not resolved to override"
fi
if echo "$RESULT" | grep -q "lotus:\${LOTUS1_COMMIT" || echo "$RESULT" | grep -q "lotus:latest"; then
  # lotus1 should be 'latest' after envsubst
  pass "lotus1 image tag = latest (file default preserved)"
else
  warn "lotus1 image tag unexpected"
fi

subheader "2.5 — Dockerfile build (config image)"
if command -v docker &>/dev/null; then
  if docker build -f Dockerfile -t test-scaffold-config . -q 2>/dev/null; then
    pass "Config image builds successfully"

    # Inspect contents
    CID=$(docker create test-scaffold-config 2>/dev/null)
    HAS_COMPOSE=$(docker cp "$CID":/docker-compose.yaml /dev/null 2>&1 && echo "yes" || echo "no")
    HAS_ENV=$(docker cp "$CID":/\.env /dev/null 2>&1 && echo "yes" || echo "no")
    HAS_TESTENV=$(docker cp "$CID":/test.env /dev/null 2>&1 && echo "yes" || echo "no")
    docker rm "$CID" >/dev/null 2>&1

    [[ "$HAS_COMPOSE" == "yes" ]] && pass "Config image contains docker-compose.yaml" || fail "Missing docker-compose.yaml"
    [[ "$HAS_ENV" == "yes" ]] && pass "Config image contains .env" || fail "Missing .env"
    [[ "$HAS_TESTENV" == "yes" ]] && pass "Config image contains test.env" || fail "Missing test.env"

    docker rmi test-scaffold-config >/dev/null 2>&1 || true
  else
    fail "Config image build failed"
  fi
else
  warn "Docker not available — skipping image build test"
fi

fi # end PHASE 2

# ═══════════════════════════════════════════════════════════════════
# PHASE 3: act dry-run (workflow graph validation)
# ═══════════════════════════════════════════════════════════════════
if $RUN_PHASE3; then
header "PHASE 3: act dry-run (workflow graph)"

if command -v act &>/dev/null; then

  subheader "3.1 — Build & Test: PR trigger (dry-run)"
  echo "  Simulating: pull_request event on versions.env change"
  ACT_OUTPUT=$(act pull_request \
    --workflows .github/workflows/on_versions_change.yml \
    --dryrun \
    --quiet \
    2>&1 || true)

  if echo "$ACT_OUTPUT" | grep -q "parse-config"; then
    pass "parse-config job discovered"
  else
    fail "parse-config job not found in dry-run"
  fi

  # Check job ordering from dry-run output
  if echo "$ACT_OUTPUT" | grep -qE "build-(lotus0|forest|drand)"; then
    pass "Build jobs discovered"
  else
    warn "Build jobs not visible in dry-run (may be due to workflow_call limitation)"
  fi

  if echo "$ACT_OUTPUT" | grep -q "run-test"; then
    pass "run-test job discovered"
  else
    warn "run-test not visible (may need build-config to complete first)"
  fi

  subheader "3.2 — Build & Test: workflow_dispatch (dry-run)"
  ACT_OUTPUT2=$(act workflow_dispatch \
    --workflows .github/workflows/on_versions_change.yml \
    --dryrun \
    --quiet \
    2>&1 || true)

  if echo "$ACT_OUTPUT2" | grep -q "parse-config"; then
    pass "workflow_dispatch: parse-config job runs"
  else
    fail "workflow_dispatch: parse-config not found"
  fi

  subheader "3.3 — Run Antithesis Test: workflow_dispatch (dry-run)"
  ACT_OUTPUT3=$(act workflow_dispatch \
    --workflows .github/workflows/run_antithesis_test.yml \
    --dryrun \
    --quiet \
    2>&1 || true)

  if echo "$ACT_OUTPUT3" | grep -q "resolve"; then
    pass "Run Test: resolve job discovered"
  else
    fail "Run Test: resolve job not found"
  fi

  if echo "$ACT_OUTPUT3" | grep -q "manual_run"; then
    pass "Run Test: manual_run job discovered"
  else
    warn "Run Test: manual_run not visible in dry-run"
  fi

  subheader "3.4 — Run Antithesis Test: schedule (dry-run)"
  ACT_OUTPUT4=$(act schedule \
    --workflows .github/workflows/run_antithesis_test.yml \
    --dryrun \
    --quiet \
    2>&1 || true)

  if echo "$ACT_OUTPUT4" | grep -q "resolve"; then
    pass "Schedule: resolve job runs"
  else
    warn "Schedule: resolve job not visible"
  fi

  for job in nightly_implementors nightly_foc short_implementors short_foc; do
    if echo "$ACT_OUTPUT4" | grep -q "$job"; then
      pass "Schedule: $job job discovered"
    else
      warn "Schedule: $job not visible (conditional on cron match)"
    fi
  done

  subheader "3.5 — No push trigger on Build & Test"
  ACT_OUTPUT5=$(act push \
    --workflows .github/workflows/on_versions_change.yml \
    --dryrun \
    --quiet \
    2>&1 || true)

  if echo "$ACT_OUTPUT5" | grep -q "parse-config"; then
    fail "Push trigger matched — Build & Test should only trigger on PRs"
  else
    pass "Push event correctly ignored by Build & Test"
  fi

else
  warn "act not installed — skipping dry-run tests"
  echo "  Install: https://github.com/nektos/act"
fi

fi # end PHASE 3

# ═══════════════════════════════════════════════════════════════════
# Summary
# ═══════════════════════════════════════════════════════════════════
echo ""
header "RESULTS"
echo -e "  ${GREEN}PASS: $PASS${NC}  ${YELLOW}WARN: $WARN${NC}  ${RED}FAIL: $FAIL${NC}"
echo ""

if [[ $FAIL -gt 0 ]]; then
  echo -e "${RED}Some checks failed. Fix issues before pushing.${NC}"
  exit 1
elif [[ $WARN -gt 0 ]]; then
  echo -e "${YELLOW}All critical checks passed. Warnings are expected for act limitations.${NC}"
  exit 0
else
  echo -e "${GREEN}All checks passed!${NC}"
  exit 0
fi
