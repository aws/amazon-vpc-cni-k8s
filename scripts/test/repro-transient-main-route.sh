#!/usr/bin/env bash

set -euo pipefail

readonly CLIENT_NAMESPACE="vpc-cni-client-$$"
readonly SERVER_NAMESPACE="vpc-cni-server-$$"
readonly CLIENT_VETH="vc$$"
readonly SERVER_VETH="vs$$"
readonly PRIMARY_IP="10.0.0.10"
readonly GATEWAY_IP="10.0.0.1"
readonly ENI_IP="172.20.37.151"
readonly ENI_PREFIX="172.20.0.0/18"
readonly REGISTRY_IP="172.20.54.213"
readonly REGISTRY_PORT="18443"

cleanup() {
  if [[ -n ${server_pid:-} ]]; then
    kill "$server_pid" >/dev/null 2>&1 || true
  fi
  ip netns delete "$CLIENT_NAMESPACE" >/dev/null 2>&1 || true
  ip netns delete "$SERVER_NAMESPACE" >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

route_to_registry() {
  ip -n "$CLIENT_NAMESPACE" -4 route get "$REGISTRY_IP"
}

expect_route() {
  local route=$1
  local device=$2
  local source=$3

  [[ $route == *"dev $device"* ]] || fail "expected device $device, got: $route"
  [[ $route == *"src $source"* ]] || fail "expected source $source, got: $route"
}

connect_to_registry() {
  ip netns exec "$CLIENT_NAMESPACE" \
    timeout 3 bash -c "exec 3<>/dev/tcp/$REGISTRY_IP/$REGISTRY_PORT; sleep 1"
}

wait_for_socket() {
  local state=$1
  local source=$2
  local socket

  for ((attempt = 0; attempt < 100; attempt++)); do
    socket=$(ip netns exec "$CLIENT_NAMESPACE" ss -Hnt state "$state" || true)
    if [[ $socket == *"$source"* ]]; then
      echo "$socket"
      return 0
    fi
    sleep 0.01
  done

  return 1
}

if [[ $(uname -s) != Linux ]]; then
  fail "this reproducer requires Linux"
fi

if (( EUID != 0 )); then
  fail "run as root; the script creates isolated network namespaces"
fi

for command in bash ip nc ss timeout; do
  command -v "$command" >/dev/null || fail "$command is required"
done

cleanup
ip netns add "$CLIENT_NAMESPACE"
ip netns add "$SERVER_NAMESPACE"
ip link add "$CLIENT_VETH" type veth peer name "$SERVER_VETH"
ip link set "$CLIENT_VETH" netns "$CLIENT_NAMESPACE"
ip link set "$SERVER_VETH" netns "$SERVER_NAMESPACE"

ip -n "$CLIENT_NAMESPACE" link set "$CLIENT_VETH" name eth0
ip -n "$CLIENT_NAMESPACE" link add eth1 type dummy
ip -n "$CLIENT_NAMESPACE" link set lo up
ip -n "$CLIENT_NAMESPACE" link set eth0 up
ip -n "$CLIENT_NAMESPACE" link set eth1 up
ip -n "$CLIENT_NAMESPACE" address add "$PRIMARY_IP/24" dev eth0
ip -n "$CLIENT_NAMESPACE" route add 172.20.0.0/16 via "$GATEWAY_IP" dev eth0

ip -n "$SERVER_NAMESPACE" link set "$SERVER_VETH" name eth0
ip -n "$SERVER_NAMESPACE" link set lo up
ip -n "$SERVER_NAMESPACE" link set eth0 up
ip -n "$SERVER_NAMESPACE" address add "$GATEWAY_IP/24" dev eth0
ip -n "$SERVER_NAMESPACE" address add "$REGISTRY_IP/32" dev lo

ip netns exec "$SERVER_NAMESPACE" \
  nc -lk -s "$REGISTRY_IP" -p "$REGISTRY_PORT" >/dev/null &
server_pid=$!

baseline=$(route_to_registry)
expect_route "$baseline" eth0 "$PRIMARY_IP"
connect_to_registry || fail "the baseline TCP connection did not succeed"
echo "Stable route:       $baseline"
echo "Stable TCP:         connected"

# This is the current setupENINetwork() behavior. Adding an address with a
# prefix makes Linux install a connected route in the main table immediately.
ip -n "$CLIENT_NAMESPACE" address add "$ENI_IP/18" dev eth1

transient=$(route_to_registry)
expect_route "$transient" eth1 "$ENI_IP"
ip -n "$CLIENT_NAMESPACE" route show table main | grep -F "$ENI_PREFIX" >/dev/null || \
  fail "the implicit connected route was not created"
echo "Current behavior:   $transient"

# Open the connection while the connected route is present, then model the
# RouteDel() that VPC CNI performs after programming the ENI-specific table.
# The socket keeps the ENI source address even after route convergence.
connect_to_registry &
client_pid=$!
socket=$(wait_for_socket syn-sent "$ENI_IP") || \
  fail "a SYN-SENT socket sourced from $ENI_IP was not observed"
ip -n "$CLIENT_NAMESPACE" route delete "$ENI_PREFIX" dev eth1

converged=$(route_to_registry)
expect_route "$converged" eth0 "$PRIMARY_IP"
echo "After RouteDel:     $converged"
echo "Pinned TCP socket:  $socket"

if wait "$client_pid"; then
  fail "the connection opened during the transient route unexpectedly succeeded"
fi
echo "Current TCP:        timed out with the transient ENI source"

ip -n "$CLIENT_NAMESPACE" address delete "$ENI_IP/18" dev eth1

# This is the behavior from PR #3858. NOPREFIXROUTE assigns the same ENI
# address without exposing an intermediate main-table route.
ip -n "$CLIENT_NAMESPACE" address add "$ENI_IP/18" dev eth1 noprefixroute

fixed=$(route_to_registry)
expect_route "$fixed" eth0 "$PRIMARY_IP"
if ip -n "$CLIENT_NAMESPACE" route show table main | grep -F "$ENI_PREFIX" >/dev/null; then
  fail "noprefixroute unexpectedly created a connected route"
fi
connect_to_registry &
client_pid=$!
socket=$(wait_for_socket established "$PRIMARY_IP") || \
  fail "an established socket sourced from $PRIMARY_IP was not observed"
wait "$client_pid" || fail "TCP failed with noprefixroute"
echo "With noprefixroute: $fixed"
echo "Fixed TCP socket:   $socket"

echo "PASS: reproduced the transient route and pinned TCP source; noprefixroute prevented both"
