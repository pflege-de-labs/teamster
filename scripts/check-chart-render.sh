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

# refuses <description> <helm args...> — the render has to fail.
refuses() {
	local description=$1
	shift
	if helm template teamster "$chart" --values "$chart/ci/statefulset-values.yaml" "$@" >/dev/null 2>&1; then
		fail "$description: the chart accepted it"
	fi
}

# renders <description> <helm args...> — the render has to succeed. The guards
# have to let the shapes through that are merely unusual, not wrong.
renders() {
	local description=$1
	shift
	if ! helm template teamster "$chart" --values "$chart/ci/statefulset-values.yaml" "$@" >/dev/null 2>&1; then
		fail "$description: the chart refused it"
	fi
}

# absent <rendered> <description> <pattern> — must not appear.
absent() {
	local rendered=$1 description=$2 pattern=$3
	if grep -q -- "$pattern" <<<"$rendered"; then
		fail "$description: $pattern is there and should not be"
	fi
}

# counts <rendered> <description> <expected> <pattern> — exactly this many.
counts() {
	local rendered=$1 description=$2 expected=$3 pattern=$4
	local found
	found=$(grep -c -- "$pattern" <<<"$rendered" || true)
	[ "$found" = "$expected" ] || fail "$description: $found × $pattern, want $expected"
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

	# Configuration, which a pod without it starts and then cannot do anything
	# useful.
	contains "$workload" "$name $kind" "mountPath: /etc/xdg/teamster"

	# State, which depends on where it lives. A sqlite release must mount a
	# data volume; a postgres one must not, because mounting a volume nothing
	# writes to is how a Deployment quietly acquires a ReadWriteOnce claim and
	# stops being able to roll.
	driver=$(grep -m1 -E '^ +driver: ' <<<"$rendered" | awk '{print $2}')
	case "${driver:-sqlite}" in
	sqlite)
		contains "$workload" "$name $kind" "name: data"
		grep -q "kind: PodDisruptionBudget" <<<"$rendered" &&
			fail "$name renders a disruption budget for a single-writer release"
		;;
	postgres)
		grep -q "name: data" <<<"$workload" &&
			fail "$name mounts a data volume, which a postgres release has no use for"
		contains "$rendered" "$name credentials" "TEAMSTER_DATABASE_POSTGRES_PASSWORD"
		# The password must never be in the config file: a value there beats the
		# environment variable that carries it.
		config=$(awk '/config.yaml: \|/,/^---$/' <<<"$rendered")
		grep -q "password" <<<"$config" &&
			fail "$name writes a database password into the config file"
		replicas=$(grep -m1 -E '^  replicas: ' <<<"$workload" | awk '{print $2}')
		if [ "${replicas:-1}" -gt 1 ]; then
			contains "$rendered" "$name" "kind: PodDisruptionBudget"
		fi
		;;
	*)
		fail "$name renders an unknown driver ${driver}"
		;;
	esac

	# The metrics port is published by two templates from one setting. Drift
	# between them is a Service that resolves to nothing, or a listener nothing
	# can reach — so assert they agree, whichever way this values file has it.
	services=$(awk '/^kind: Service$/,/^---$/' <<<"$rendered")
	if grep -q "name: metrics" <<<"$workload"; then
		contains "$services" "$name Service" "name: metrics"
	elif grep -q "name: metrics" <<<"$services"; then
		fail "$name: the Service publishes a metrics port the container does not open"
	fi

	# Push-only: no exporter listens, so there is nothing to publish and
	# nothing to scrape. A port here would resolve to a closed socket.
	if grep -q "prometheus: false" <<<"$rendered"; then
		absent "$rendered" "$name push-only" "name: metrics"
		absent "$rendered" "$name push-only" "kind: ServiceMonitor"
		contains "$rendered" "$name push-only" "otlp-endpoint:"
	fi

	# A ServiceMonitor names a Service port by name, and asks for protobuf
	# first, which is the only protocol native histograms travel over.
	if grep -q "kind: ServiceMonitor" <<<"$rendered"; then
		monitor=$(awk '/^kind: ServiceMonitor$/,0' <<<"$rendered")
		contains "$monitor" "$name ServiceMonitor" "port: metrics" "- PrometheusProto"
		contains "$services" "$name Service" "name: metrics"
	fi
done

# Probes render once each. The startupProbe is emitted from a `with` block, and
# a second block left behind by an edit produces a duplicate mapping key: helm
# renders it happily and the API server rejects the manifest.
echo "checking the probes"
probed=$(helm template teamster "$chart" --values "$chart/ci/statefulset-values.yaml" \
	--set startupProbe.httpGet.path=/healthz --set startupProbe.httpGet.port=http)
counts "$probed" "startupProbe" 1 "startupProbe:"
counts "$probed" "livenessProbe" 1 "livenessProbe:"
counts "$probed" "readinessProbe" 1 "readinessProbe:"

# The metrics listener takes no credentials, and the application binds it to
# loopback for that reason. In a pod loopback reaches nothing, so the chart has
# to refuse it rather than publish a Service port that resolves to silence.
echo "checking the metrics guards"
refuses "a loopback metrics addr" \
	--set config.settings.metrics.enabled=true \
	--set config.settings.metrics.addr=127.0.0.1:9090
refuses "metrics sharing the server port" \
	--set config.settings.metrics.enabled=true \
	--set config.settings.metrics.addr=:8080
refuses "a ServiceMonitor with no listener to scrape" \
	--set metrics.serviceMonitor.enabled=true
refuses "metrics collected and exported nowhere" \
	--set config.settings.metrics.enabled=true \
	--set config.settings.metrics.prometheus=false

# The addr belongs to the listener. A push-only deployment runs none, so the
# loopback guard must not fire on a value nothing reads.
renders "a push-only deployment keeping the application's loopback default" \
	--set config.settings.metrics.enabled=true \
	--set config.settings.metrics.prometheus=false \
	--set config.settings.metrics.otlp-endpoint=otel:4318 \
	--set config.settings.metrics.addr=127.0.0.1:9090

if [ "$failures" -gt 0 ]; then
	echo "$failures assertion(s) failed" >&2
	exit 1
fi
echo "chart renders what it should"
