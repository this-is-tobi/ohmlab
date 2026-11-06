#!/usr/bin/env bash
# The examples run on managed clusters, which a render does not show they can.
# On each cluster, from the objects every app renders for it:
# - a route attaches to a listener of a Gateway on that cluster: the Gateway
#   its parentRef names is rendered there, the listener its sectionName names
#   exists, takes its kind and admits its namespace, and each of its hostnames
#   matches that listener's (a Gateway leaves a route it does not accept
#   unserved, and the render shows nothing of it);
# - a https host an app names (its URL, an OIDC issuer) under the hostname of
#   one of the cluster's listeners is a hostname of a route that attaches, or
#   of an Ingress;
# - nothing selects nodes by a label but kubernetes.io/os and
#   kubernetes.io/arch, which every node carries, or names a StorageClass: a
#   managed cluster has neither another cluster's node labels nor its
#   StorageClasses, and gives a volume that names none its default class.
#
#   check-example-clusters.sh <render dir>
#
# <render dir> is render-instance.sh's RENDER_OUT: <instance>/<tier>/<app>.yaml
# and .destination. Prints `ok   <instance>/<tier>/<app>` or
# `FAIL <instance>/<tier>/<app>: ...`, never a render; exits with the number of
# failures, at most 255 (an exit status is taken modulo 256). Needs yq and jq.
set -uo pipefail

dir="${1:?render dir}"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# Every object of every app, with its app, cluster and namespace. CRDs are left
# out: their schemas name the fields this check reads.
: >"$work/objects.jsonl"
: >"$work/apps"
while IFS= read -r file; do
  id="${file#"$dir"/}"
  id="${id%.yaml}"
  printf '%s\n' "$id" >>"$work/apps"
  read -r server namespace <"${file%.yaml}.destination"
  yq -o=json -I=0 'select(. != null and .kind != null and .kind != "CustomResourceDefinition")' "$file" \
    | jq -c --arg id "$id" --arg server "$server" --arg ns "$namespace" '{$id, $server, ns: (.metadata.namespace // $ns), o: .}' \
    >>"$work/objects.jsonl"
done < <(command find "$dir" -mindepth 3 -maxdepth 3 -name '*.yaml' | sort)

jq -rs '
  # Whether a hostname of a listener and one of a route share a host: an empty
  # one is every host, *.a.b every host under a.b.
  def under($p; $h): ($p | startswith("*.")) and ($h | endswith($p[1:]));
  def meets($l; $h): ($l // "") == "" or ($h // "") == "" or $l == $h or under($l; $h) or under($h; $l);
  def takes($kind): if $kind == "TLSRoute" then ["TLS"] else ["HTTP", "HTTPS"] end;
  def name($o): "\($o.kind)/\($o.metadata.name)";
  . as $all
  | [$all[] | select(.o.kind == "Gateway")] as $gateways
  # Each parentRef of each route: the hosts it serves through the listeners it
  # attaches to, or why it attaches to none.
  | [ $all[] | select(.o.kind | IN("HTTPRoute", "GRPCRoute", "TLSRoute")) as $r
      | ($r.o.spec.parentRefs // [])[] | select((.kind // "Gateway") == "Gateway") as $p
      | ($p.namespace // $r.ns) as $gns
      | "\($gns)/\($p.name)" as $gw
      | (first($gateways[] | select(.server == $r.server and .ns == $gns and .o.metadata.name == $p.name)) // null) as $g
      | "\($r.o.kind) \($r.o.metadata.name)" as $route
      | if $g == null then {id: $r.id, fail: "\($route): no app renders the Gateway \($gw) on \($r.server)"}
        else
          [($g.o.spec.listeners // [])[] | select($p.sectionName == null or .name == $p.sectionName)] as $named
          | [$named[] | select(.protocol | IN(takes($r.o.kind)[]))
              | select((.allowedRoutes.namespaces.from // "Same") != "Same" or $gns == $r.ns)] as $open
          | if $named == [] then {id: $r.id, fail: "\($route): \($gw) has no listener \($p.sectionName)"}
            elif $open == [] then {id: $r.id, fail: "\($route): listener \($named | map(.name) | join(", ")) of \($gw) admits no route of namespace \($r.ns)"}
            else
              (($r.o.spec.hostnames // []) | if . == [] then [""] else . end)[] as $h
              | [$open[] | select(meets(.hostname; $h))] as $on
              | if $on == [] then {id: $r.id, fail: "\($route): host \($h) matches no listener it attaches to on \($gw) (\($open | map("\(.name) \(.hostname // "*")") | join(", ")))"}
                else $on[] | {server: $r.server, protocol, host: (if $h == "" then (.hostname // "") else $h end)} end
            end
        end ] as $attach
  # An https URL is served through an HTTPS or a TLS listener only: a route on
  # the plain one (the redirect to https) serves none.
  | ([$attach[] | select(has("host") and (.protocol | IN("HTTPS", "TLS"))) | {server, host}]
     + [$all[] | select(.o.kind == "Ingress") as $i | (($i.o.spec.rules // [])[].host, ($i.o.spec.tls // [])[].hosts[]?) | {server: $i.server, host: .}]
    ) as $served
  | [$gateways[] as $g | ($g.o.spec.listeners // [])[] | select((.hostname // "") != "")
      | {server: $g.server, gw: "\($g.ns)/\($g.o.metadata.name)", name, hostname}] as $listeners
  # The https hosts an app names that no route or Ingress serves where they
  # point: the cluster of the most specific listener that covers them, as their
  # DNS names would. A URL on another port (an API server) is not one a listener
  # serves.
  | [ $all[] | select(.o.kind != "Gateway") as $x
      | [$x.o | .. | strings | scan("https://([a-z0-9](?:[a-z0-9.-]*[a-z0-9])?)(:[0-9]+)?") | select(.[1] == null or .[1] == ":443") | .[0]]
      | unique[] as $h
      | ([$listeners[] | select(.hostname == $h or under(.hostname; $h))]
         | sort_by(if .hostname == $h then 1000 else (.hostname | length) end) | last) as $l
      | select($l != null)
      | select(any($served[]; .server == $l.server and (.host == "" or .host == $h or under(.host; $h))) | not)
      | {id: $x.id, fail: "names https://\($h), which listener \($l.name) of \($l.gw) serves, but no route or Ingress on \($l.server) has it"} ] as $hosts
  # What a managed cluster lacks: node labels, StorageClasses.
  | [ $all[] as $x | $x.o as $o
      | ( ([$o | .. | objects | select(has("nodeSelector")) | .nodeSelector | objects | keys[]]
           + [$o | .. | objects | select(has("nodeSelectorTerms")) | .nodeSelectorTerms | arrays | .[].matchExpressions[]?
              | select(.operator | IN("In", "Exists", "Gt", "Lt")) | .key])
          | unique[] | select(IN("kubernetes.io/os", "kubernetes.io/arch") | not)
          | {id: $x.id, fail: "\(name($o)) selects nodes by \(.), which a managed cluster'"'"'s nodes do not carry"} ),
        ( [$o | .. | objects | select(has("storageClassName")) | .storageClassName | strings] | unique[]
          | {id: $x.id, fail: (if . == "" then "\(name($o)) asks for no StorageClass (\"\"), which only a volume made by hand binds: leave it unset for the cluster'"'"'s default"
                               else "\(name($o)) names the StorageClass \(.): leave it unset for the cluster'"'"'s default" end)} ),
        ( [$o | .. | objects | select(has("storageClass")) | .storageClass | strings | select(. != "")] | unique[]
          | {id: $x.id, fail: "\(name($o)) names the StorageClass \(.): leave it unset for the cluster'"'"'s default"} ) ] as $nodes
  | [$attach[], $hosts[], $nodes[] | select(.fail)] | unique[] | "\(.id)\t\(.fail)"
' "$work/objects.jsonl" >"$work/fails" || { echo "FAIL cannot read the objects of $dir"; exit 1; }

failures=0
while IFS= read -r id; do
  if grep -q "^$id	" "$work/fails"; then
    while IFS=$'\t' read -r _ why; do
      echo "FAIL $id: $why"
      failures=$((failures + 1))
    done < <(grep "^$id	" "$work/fails")
  else
    echo "ok   $id"
  fi
done <"$work/apps"
exit $((failures > 255 ? 255 : failures))
