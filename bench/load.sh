#!/usr/bin/env bash
# Usage: FRAMEWORKS="ginject gin" DURATION=10s CONNECTIONS="64 256" ./load.sh
set -euo pipefail
cd "$(dirname "$0")"

frameworks=${FRAMEWORKS:-ginject}
duration=${DURATION:-10s}
connections=${CONNECTIONS:-"64 256"}
threads=${THREADS:-4}
port=${PORT:-18080}
url="http://127.0.0.1:${port}"

go build -o "${TMPDIR:-/tmp}/ginject-bench-server" ./cmd/server
server="${TMPDIR:-/tmp}/ginject-bench-server"

echo "| framework | scenario | connections | requests/s | p50 | p99 | non-2xx |"
echo "|---|---|---|---|---|---|---|"
for framework in $frameworks; do
  "$server" -framework "$framework" -port "$port" >/dev/null 2>&1 &
  pid=$!
  trap 'kill $pid 2>/dev/null || true' EXIT
  for _ in $(seq 50); do curl -s -o /dev/null "$url/static" && break; sleep 0.1; done

  for conns in $connections; do
    for scenario in Static Param JSONBody Chain; do
      args=(-t"$threads" -c"$conns" -d"$duration" --latency)
      case $scenario in
        Static) path=/static ;;
        Param) path=/users/42 ;;
        JSONBody) path=/users; args+=(-s post_users.lua) ;;
        Chain) path=/chain; args+=(-H "Authorization: Bearer bench") ;;
      esac
      out=$(wrk "${args[@]}" "$url$path")
      rps=$(awk '/Requests\/sec/ {print $2}' <<<"$out")
      p50=$(awk '$1=="50%" {print $2}' <<<"$out")
      p99=$(awk '$1=="99%" {print $2}' <<<"$out")
      bad=$(awk '/Non-2xx/ {print $NF}' <<<"$out")
      echo "| $framework | $scenario | $conns | $rps | $p50 | $p99 | ${bad:-0} |"
    done
  done

  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
done
