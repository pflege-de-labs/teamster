#!/usr/bin/env bash
# The chart refuses what the application cannot do. Each case here is a
# deployment that would start and then be wrong, so a render that succeeds
# means a guard is gone.
set -euo pipefail

chart=${1:?usage: $0 <chart-dir>}
failures=0

reject() {
	if helm template teamster "$chart" "$@" >/dev/null 2>&1; then
		echo "  ✗ chart accepted: $*" >&2
		failures=$((failures + 1))
	fi
}

sqlite=(--values "$chart/ci/statefulset-values.yaml")
postgres=(--values "$chart/ci/postgres-values.yaml")

# SQLite takes one writer, and a Deployment needs a claim that outlives the pod.
reject "${sqlite[@]}" --set replicaCount=2
reject "${sqlite[@]}" --set workload.kind=Deployment
# A driver nothing implements.
reject "${sqlite[@]}" --set database.driver=cassandra
# Postgres without somewhere to connect, or without a password.
reject --set database.driver=postgres --set credentials.webhookToken=t \
	--set credentials.adminPassword=p --set credentials.graphClientSecret=s
reject --set database.driver=postgres --set database.postgres.host=db \
	--set credentials.webhookToken=t --set credentials.adminPassword=p \
	--set credentials.graphClientSecret=s
# Two passwords, which is one too many to reason about.
reject "${postgres[@]}" --set database.postgres.passwordFrom.secretName=other
# A claim a postgres release has no use for.
reject "${postgres[@]}" --set persistence.existingClaim=data-teamster-0
# A secret in the config file, where it would beat the environment variable that carries it.
reject "${postgres[@]}" --set config.settings.database.postgres.password=leaked
reject "${sqlite[@]}" --set config.settings.webhook.token=leaked

if [ "$failures" -gt 0 ]; then
	echo "chart accepted ${failures} unsupported combination(s)" >&2
	exit 1
fi
echo "chart refuses every unsupported combination"
