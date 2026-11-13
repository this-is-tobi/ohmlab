#!/usr/bin/env bash
# The VaultSeed objects the vault-operator chart renders for an instance must be
# valid: templates well formed, no two objects on one path, no reference cycle.
# `ohmlab validate` is what the controller applies, run here without a cluster
# on what each instance would deploy.
#
# A remote instance (vault.hub) reads its secrets from its hub's Vault, where
# the hub's VaultSeeds create them: every key its apps' VaultStaticSecrets read
# must be in the hub's seed of that path. An instance with a Vault of its own
# (vault-operator values and no vault.hub) is its own hub: nothing but its
# seeds writes what its own apps read either.
#
#   check-vault-seeds.sh
#   check-vault-seeds.sh --reads <reads.json> <seeds.json> <hub>
#
# reads.json: [{label, secret, path, texts}], the <mount>/<path> each
# VaultStaticSecret reads and its template texts; seeds.json: [{path, data}],
# the hub's VaultSeeds. Needs helm, yq, jq and Go (it runs the CLI from cli/).
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/lib/instances.sh
source "$ROOT/scripts/lib/instances.sh"

# seed_reads <reads.json> <seeds.json> <hub> [<ok prefix>]: one FAIL line per
# key read that the hub's seed of its path does not have, and per entry of
# reads.json that is a failure ({label, fail}), else one ok line. Each template
# action that uses .Secrets must be one of
#   get .Secrets "<k>" [| default dict] [| dig "<k2>"... "<default>"] [| <filter>]...
#   .Secrets.<k>[.<k2>...] [| <filter>]...
#   printf "<format>" with such gets in parentheses, and strings, as arguments
# where a filter is default "<s>", b64dec, trim or toJson, and any other fails,
# naming the secret: it could read keys this check does not see. A whole text
#   {{ with get .Secrets "<claim>" }}{{ dig "<k>" "" . }}[{{ else }}<a text>]{{ end }}
# reads a storage claim's key where the data has the claim, else its fallback.
# A path that ends in /s3 is a claim's record, which the controller's storage
# target writes, not a seed: its reads are <claim>.<one of the record's keys>.
# A secret without templates needs its path seeded, unless it is a record.
# Returns 1 when the files cannot be read.
seed_reads() {
  jq -r --slurpfile seeds "$2" --arg hub "$3" --arg prefix "${4:-}" '
    def lit: "\"(?:[^\"\\\\]|\\\\.)*\"";
    def filters: "(?:\\s*\\|\\s*(?:default\\s+" + lit + "|b64dec|trim|toJson))*";
    def get_re: "get\\s+\\.Secrets\\s+(" + lit + ")(?:\\s*\\|\\s*default\\s+dict)?((?:\\s*\\|\\s*dig(?:\\s+" + lit + ")+)?)" + filters;
    def field_re: "\\.Secrets((?:\\.[A-Za-z_][A-Za-z0-9_]*)+)" + filters;
    # The key path of a get_re match (the last argument of dig is its
    # default), or null for a dig without one.
    def get_path: [.captures[1].string | match(lit; "g") | .string | fromjson] as $dig
      | if $dig != [] and ($dig | length) < 2 then null else [.captures[0].string | fromjson] + $dig[:-1] end;
    # The key paths an action (the text between {{ and }}) reads, or null.
    def action_reads:
      if test("\\.Secrets") | not then []
      elif test("^\\s*" + get_re + "\\s*$") then [match(get_re) | get_path]
      elif test("^\\s*" + field_re + "\\s*$") then [match(field_re) | .captures[0].string | ltrimstr(".") | split(".")]
      elif test("^\\s*printf\\s+" + lit + "(?:\\s+(?:\\(\\s*" + get_re + "\\s*\\)|" + lit + "))+\\s*$") then [match(get_re; "g") | get_path]
      else null end;
    # An action, the text between {{ and }}: as in Go templates, a }} inside
    # a string does not end it.
    def act: "\\{\\{-?((?:" + lit + "|`[^`]*`|[^\"`}]|\\}(?!\\}))*?)-?\\}\\}";
    # The key paths a template text reads, or null when one of its actions is
    # none of the forms, or one is left open.
    def keys_read:
      if gsub(act; "") | test("\\{\\{") then null
      else [match(act; "g") | .captures[0].string | action_reads] as $acts
        | if any($acts[]; . == null or any(.[]; . == null)) then null else [$acts[][]] end end;
    # Whether the seed data has the key path, whatever its value: an empty
    # one is a placeholder that an operator fills.
    def seeded($data; $p): (try ($data | getpath($p[:-1])) catch null) | type == "object" and has($p[-1]);
    def with_re: "^\\{\\{-?\\s*with\\s+get\\s+\\.Secrets\\s+(" + lit + ")\\s*-?\\}\\}\\{\\{-?\\s*dig\\s+(" + lit + ")\\s+\"\"\\s+\\.\\s*-?\\}\\}(?:\\{\\{-?\\s*else\\s*-?\\}\\}(.*))?\\{\\{-?\\s*end\\s*-?\\}\\}$";
    # The key paths a text reads from a path, or null: the claim form reads
    # the claim key on a record or where the data has it, else its fallback.
    def text_reads($data; $record):
      if test(with_re) then
        (match(with_re) | .captures | map(.string)) as $c
        | ($c[0] | fromjson) as $claim
        | if $record or seeded($data; [$claim]) then [[$claim, ($c[1] | fromjson)]]
          elif $c[2] then ($c[2] | keys_read)
          else [] end
      else keys_read end;
    def record_keys: ["accessKey", "secretKey", "endpoint", "region", "pathStyle", "bucket", "ca"];
    [ .[] as $r
      | ($seeds[0] | map(select(.path == $r.path)) | first) as $seed
      | (($r.path // "") | endswith("/s3")) as $record
      | if $r.fail then {fail: "\($r.label): \($r.fail)"}
        elif ($r.texts | length) == 0 then
          if $seed == null and ($record | not) then {fail: "\($r.label): \($r.path) is not seeded by hub \($hub)"} else empty end
        else
          $r.texts[] | text_reads($seed.data; $record) as $paths
          | if $paths == null then {fail: "\($r.label): VaultStaticSecret \($r.secret) reads .Secrets in a form this check cannot follow: \(.[0:160])"}
            else $paths[] as $p
              | if $record then
                  if ($p | length) == 2 and ($p[1] | IN(record_keys[])) then {read: "\($r.path) \($p | join("."))"}
                  else {fail: "\($r.label): \($r.path) is a claim'"'"'s record: \($p | join(".")) is not one of its keys (<claim>.\(record_keys | join(", ")))"} end
                elif seeded($seed.data; $p) then {read: "\($r.path) \($p | join("."))"}
                else {fail: "\($r.label): \($r.path) key \($p | join(".")) is not seeded by hub \($hub)"} end
            end
        end ]
    | (map(.fail // empty) | unique) as $fails
    | if $fails == [] then "ok   \($prefix)\(map(.read // empty) | unique | length) key(s) read, all seeded by \($hub)"
      else $fails[] | "FAIL " + . end' "$1" 2>/dev/null
}

if [ "${1:-}" = "--reads" ]; then
  out=$(seed_reads "${2:?reads.json}" "${3:?seeds.json}" "${4:?hub}") || { echo "FAIL cannot read $2 or $3"; exit 2; }
  printf '%s\n' "$out"
  if grep -q '^FAIL' <<<"$out"; then exit 1; fi
  exit 0
fi

failures=0
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
(cd "$ROOT/cli" && go build -o "$work/ohmlab-validate" .) || { echo "FAIL: cannot build the CLI" >&2; exit 1; }

# render_seeds <instance dir>: sets `rendered` to the vault-operator chart
# rendered with the instance's values and the parameters its manager gives the
# core tier, its seeds on as if the cluster had the VaultSeed kind. When it
# does not render, sets it to the reason and returns 1.
render_seeds() {
  local values="$1/values/core/vault-operator.yaml" lines p args=()
  if ! lines=$(manager_parameters "$1" core 2>"$work/err"); then
    rendered="the instance-manager does not render: $(cat "$work/err")"
    return 1
  fi
  [ -f "$values" ] && args=(-f "$values")
  while IFS= read -r p; do [ -n "$p" ] && args+=(--set-string "$p"); done <<<"$lines"
  # Helm's warnings stay out of the manifests the seeds are read from.
  if ! rendered=$(helm template check "$ROOT/argo-cd/apps/vault-operator" "${args[@]+"${args[@]}"}" --namespace vault-operator-system \
        --set ohmlab.vault.seeds.enabled=true --set ohmlab.vault.seeds.requireCRD=false 2>"$work/err"); then
    rendered=$(grep -m1 '^Error' "$work/err" | cut -c1-240)
    return 1
  fi
}

for values in "$INSTANCES_DIR"/*/values/core/vault-operator.yaml; do
  dir="$(dirname "$(dirname "$(dirname "$values")")")"
  instance="$(basename "$dir")"
  if ! render_seeds "$dir"; then
    echo "FAIL $instance: $rendered"
    failures=$((failures + 1))
    continue
  fi
  # The seeds are the app's last sync wave, so nothing else waits for them.
  if ! yq -o=json -I=0 'select(. != null) | [.kind, (.metadata.annotations["argocd.argoproj.io/sync-wave"] // "0")]' <<<"$rendered" \
      | jq -se 'map([.[0], (.[1] | tonumber)]) | (map(select(.[0] == "VaultSeed") | .[1]) | min) as $seed
                | (map(select(.[0] != "VaultSeed") | .[1]) | max) as $other | $seed == null or $seed > $other' >/dev/null; then
    echo "FAIL $instance: an object of vault-operator syncs in the same wave as the VaultSeeds or after them"
    failures=$((failures + 1))
    continue
  fi
  if out=$("$work/ohmlab-validate" validate - <<<"$rendered" 2>&1); then
    echo "ok   $instance: $out"
  else
    echo "FAIL $instance:"; printf '%s\n' "$out" | sed 's/^/     /'
    failures=$((failures + 1))
  fi
done

# reads_of <instance dir>: one reads.json entry per VaultStaticSecret of the
# instance's enabled apps, rendered as their Applications render them, and a
# failure entry ({label, fail}) for an app that does not render.
reads_of() {
  local dir="$1" instance="${1##*/}" scope prefix suffix entry label ns lines
  prefix=$(yq '.namespacePrefix // ""' "$dir/instance.yaml")
  suffix=$(yq '.namespaceSuffix // ""' "$dir/instance.yaml")
  for scope in core tenant; do
    [ -f "$dir/$scope.yaml" ] || continue
    if ! lines=$(manager_parameters "$dir" "$scope" 2>"$work/err"); then
      jq -cn --arg label "$instance" --arg why "$(cat "$work/err")" '{$label, fail: "the instance-manager does not render: \($why)"}'
      return
    fi
    while IFS= read -r entry; do
      [ -n "$entry" ] || continue
      label="$instance/$scope/$(jq -r '.app' <<<"$entry")"
      ns=$(jq -r --arg p "$prefix" --arg s "$suffix" '.namespace // "\($p)\(.app)\($s)"' <<<"$entry")
      vso_render "$dir" "$scope" "$entry" "$lines" "$ns" "$work/app" >"$work/why"
      case $? in
        0) yq ea -o=json -I=0 '[select(.kind == "VaultStaticSecret")]' "$work/app" \
             | jq -c --arg label "$label" '.[] | {$label, secret: .metadata.name, path: "\(.spec.mount)/\(.spec.path)",
                 texts: [(.spec.destination.transformation.templates // {})[] | .text // empty]}' ;;
        2) ;;
        *) jq -cn --arg label "$label" --arg why "$(cat "$work/why")" '{$label, fail: "does not render: \($why)"}' ;;
      esac
    done < <(yq -o=json -I=0 '.apps[] | select(.enabled == "true" or .enabled == true)' "$dir/$scope.yaml")
  done
}

for dir in "$INSTANCES_DIR"/*/; do
  dir="${dir%/}"
  instance="${dir##*/}"
  hub=$(yq '.vault.hub // ""' "$dir/instance.yaml" 2>/dev/null)
  if [ -z "$hub" ]; then
    [ -f "$dir/values/core/vault-operator.yaml" ] || continue
    hub=$instance
  elif [ ! -f "$INSTANCES_DIR/$hub/instance.yaml" ]; then
    echo "FAIL $instance: hub $hub is not an instance of $INSTANCES_DIR"
    failures=$((failures + 1))
    continue
  fi
  if ! render_seeds "$INSTANCES_DIR/$hub"; then
    # The loop above reported an instance's own seeds that do not render.
    [ "$hub" = "$instance" ] && continue
    echo "FAIL $instance: the seeds of hub $hub do not render: $rendered"
    failures=$((failures + 1))
    continue
  fi
  yq ea -o=json -I=0 '[select(.kind == "VaultSeed") | .spec | {"path": .path, "data": (.data // {})}]' - <<<"$rendered" >"$work/seeds.json"
  reads_of "$dir" | jq -s . >"$work/reads.json"
  if ! out=$(seed_reads "$work/reads.json" "$work/seeds.json" "$hub" "$instance: "); then
    out="FAIL $instance: cannot read the seeds of hub $hub or the VaultStaticSecrets of its apps"
  fi
  printf '%s\n' "$out"
  failures=$((failures + $(grep -c '^FAIL' <<<"$out")))
done
# An exit status is taken modulo 256.
exit $((failures > 255 ? 255 : failures))
