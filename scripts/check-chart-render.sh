#!/usr/bin/env bash
# Asserts what the chart renders, not just that it renders.
#
# helm lint and a template run catch a chart that is broken. They say nothing
# about a chart that is quietly wrong — probes dropped because a values key was
# emptied or a helper refactored, the data volume gone, the config no longer
# mounted. Each of those deploys happily and fails at three in the morning.
set -euo pipefail

chart=${1:?usage: $0 <chart-dir>}
failures=0

fail() {
	echo "  ✗ $1" >&2
	failures=$((failures + 1))
}

# contains <rendered> <description> <pattern...> — every pattern must appear.
contains() {
	local rendered=$1 description=$2
	shift 2
	for pattern in "$@"; do
		grep -q -- "$pattern" <<<"$rendered" || fail "$description: no $pattern"
	done
}

for values in "$chart"/ci/*-values.yaml; do
	name=$(basename "$values")
	echo "checking $name"

	rendered=$(helm template teamster "$chart" --values "$values")
	kind=$(grep -m1 -E '^kind: (Deployment|StatefulSet)$' <<<"$rendered" | cut -d' ' -f2)
	[ -n "$kind" ] || { fail "$name renders neither a Deployment nor a StatefulSet"; continue; }

	# The workload the values asked for, with both probes on it. A probe that
	# disappears is the failure this script exists for: nothing else notices.
	workload=$(awk "/^kind: $kind\$/,/^---\$/" <<<"$rendered")
	contains "$workload" "$name $kind" \
		"livenessProbe:" "path: /healthz" \
		"readinessProbe:" "path: /readyz"

	# The service has to point at the port the probes name.
	contains "$workload" "$name $kind" "name: http"

	# Configuration and state, which a pod without them starts and then cannot
	# do anything useful.
	contains "$workload" "$name $kind" "mountPath: /etc/xdg/teamster" "name: data"
done

if [ "$failures" -gt 0 ]; then
	echo "$failures assertion(s) failed" >&2
	exit 1
fi
echo "chart renders what it should"
