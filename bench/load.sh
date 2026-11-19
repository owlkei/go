#!/usr/bin/env bash
# Usage: FRAMEWORKS="owlky gin" LOGS="on off" PROCS="1 2 all" DURATION=10s CONNECTIONS="64 256" ./load.sh
set -euo pipefail
cd "$(dirname "$0")"

frameworks=${FRAMEWORKS:-owlky}
logs=${LOGS:-on}
procs=${PROCS:-all}
duration=${DURATION:-10s}
connections=${CONNECTIONS:-"64 256"}
scenarios=${SCENARIOS:-"Static Param JSONBody Chain"}
threads=${THREADS:-4}
port=${PORT:-18080}
log_dir=${LOG_DIR:-${TMPDIR:-/tmp}}
url="http://127.0.0.1:${port}"
server="${TMPDIR:-/tmp}/owlky-bench-server"

go build -trimpath -ldflags="-s -w" -o "$server" ./cmd/server

cpu_us() {
  local before
  before=$(grep -c '^cpu_us' "$1" || true)
  kill -USR1 "$2"
  for _ in $(seq 50); do
    if [ "$(grep -c '^cpu_us' "$1" || true)" -gt "$before" ]; then break; fi
    sleep 0.02
  done
  awk '/^cpu_us/ {v=$2} END {print v}' "$1"
}

echo "| framework | log | GOMAXPROCS | scenario | connections | requests/s | p50 | p99 | non-2xx | requests | log lines | CPU per request |"
echo "|---|---|---|---|---|---|---|---|---|---|---|---|"
for maxprocs in $procs; do
  for framework in $frameworks; do
    framework_logs=$logs
    if [ "$framework" = nethttp ]; then framework_logs=none; fi
    for log in $framework_logs; do
      if curl -s -o /dev/null "$url/static"; then
        echo "port $port is already in use; set PORT to a free one" >&2
        exit 1
      fi
      log_file="$log_dir/owlky-bench-$framework-$log.log"
      err_file="$log_dir/owlky-bench-$framework-$log.err"
      flags=(-framework "$framework" -port "$port")
      if [ "$log" = on ]; then flags+=(-log); fi
      if [ "$maxprocs" = all ]; then
        env -u GOMAXPROCS "$server" "${flags[@]}" >"$log_file" 2>"$err_file" &
      else
        GOMAXPROCS=$maxprocs "$server" "${flags[@]}" >"$log_file" 2>"$err_file" &
      fi
      pid=$!
      trap 'kill $pid 2>/dev/null || true' EXIT
      ready=false
      for _ in $(seq 50); do
        if curl -s -o /dev/null "$url/static"; then ready=true; break; fi
        sleep 0.1
      done
      if [ "$ready" != true ] || ! kill -0 "$pid" 2>/dev/null; then
        echo "$framework server did not start on port $port" >&2
        exit 1
      fi

      for conns in $connections; do
        for scenario in $scenarios; do
          args=(-t"$threads" -c"$conns" -d"$duration" --latency)
          case $scenario in
            Static) path=/static ;;
            Param) path=/users/42 ;;
            JSONBody) path=/users; args+=(-s post_users.lua) ;;
            Chain) path=/chain; args+=(-H "Authorization: Bearer bench") ;;
          esac
          sleep 0.5
          lines_before=$(wc -l <"$log_file")
          cpu_before=$(cpu_us "$err_file" "$pid")
          out=$(wrk "${args[@]}" "$url$path")
          sleep 0.5
          cpu_after=$(cpu_us "$err_file" "$pid")
          lines=$(($(wc -l <"$log_file") - lines_before))
          rps=$(awk '/Requests\/sec/ {print $2}' <<<"$out")
          p50=$(awk '$1=="50%" {print $2}' <<<"$out")
          p99=$(awk '$1=="99%" {print $2}' <<<"$out")
          bad=$(awk '/Non-2xx/ {print $NF}' <<<"$out")
          requests=$(awk '/requests in/ {print $1}' <<<"$out")
          cpu=$(awk -v a="$cpu_before" -v b="$cpu_after" -v n="$requests" 'BEGIN {printf "%.1f µs", (b - a) / n}')
          echo "| $framework | $log | $maxprocs | $scenario | $conns | $rps | $p50 | $p99 | ${bad:-0} | $requests | $lines | $cpu |"
        done
      done

      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    done
  done
done
