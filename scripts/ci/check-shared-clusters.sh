#!/usr/bin/env bash
# Instances that share a cluster do not collide: on each destination server,
# no two Applications deploy to one namespace, and no two render the same
# cluster-scoped object (a ClusterRole, a webhook, a CRD...), which each one's
# sync would take from the other. What their projects cannot tell, as the
# projects allow each instance its own namespaces only.
#
# Reads a render directory of render-instance.sh (RENDER_OUT): per app
# <instance>/<tier>/<app>.destination ("<server> <namespace>") and
# .unnamespaced ("<Kind>/<name>" of the objects without a namespace), per
# instance crd-scopes ("<Kind> Cluster|Namespaced" of the CRDs its apps
# render). A kind is cluster-scoped when it is one of the API's own below or a
# CRD of any instance declares it so; any other kind is namespaced, the
# release's namespace taking an object that names none. Servers are compared
# as strings: one cluster has one server string across instances.
#
#   check-shared-clusters.sh <render dir>
#
# Prints `ok   <server>: ...` or `FAIL <server>: ...`; exits with the number
# of failures.
set -uo pipefail

dir="${1:?usage: check-shared-clusters.sh <render dir>}"
dir="${dir%/}"
# The API's own cluster-scoped kinds that charts render.
BUILTIN='Namespace Node PersistentVolume StorageClass CSIDriver CSINode VolumeAttachment ClusterRole ClusterRoleBinding
CustomResourceDefinition APIService MutatingWebhookConfiguration ValidatingWebhookConfiguration ValidatingAdmissionPolicy
ValidatingAdmissionPolicyBinding MutatingAdmissionPolicy MutatingAdmissionPolicyBinding PriorityClass RuntimeClass
IngressClass GatewayClass CertificateSigningRequest FlowSchema PriorityLevelConfiguration'

shopt -s nullglob
dests=("$dir"/*/*/*.destination)
if [ ${#dests[@]} -eq 0 ]; then
  echo "ok   $dir: no rendered app, nothing to check"
  exit 0
fi
scopes=""
for f in "$dir"/*/crd-scopes; do scopes="$scopes$(cat "$f")"$'\n'; done

out=$(
  for f in "${dests[@]}"; do
    id="${f#"$dir"/}"
    id="${id%.destination}"
    read -r server namespace <"$f"
    printf 'D\t%s\t%s\t%s\n' "$server" "$id" "$namespace"
    u="${f%.destination}.unnamespaced"
    [ -f "$u" ] || continue
    while IFS= read -r object; do
      [ -n "$object" ] && printf 'O\t%s\t%s\t%s\n' "$server" "$id" "$object"
    done <"$u"
  done | awk -F'\t' -v builtin="$(tr '\n' ' ' <<<"$BUILTIN")" -v scopes="$(tr '\n' ';' <<<"$scopes")" '
    function owners(list,   n, a, i, s) {
      n = split(list, a, " ")
      s = a[1]
      for (i = 2; i < n; i++) s = s ", " a[i]
      return s " and " a[n]
    }
    BEGIN {
      n = split(builtin, b, /[ \n]+/)
      for (i = 1; i <= n; i++) cluster[b[i]] = 1
      n = split(scopes, sc, ";")
      for (i = 1; i <= n; i++) if (split(sc[i], kv, " ") == 2 && kv[2] == "Cluster") cluster[kv[1]] = 1
    }
    $1 == "D" {
      apps[$2]++
      k = $2 SUBSEP $4
      if (!((k, $3) in seen_ns)) { seen_ns[k, $3] = 1; ns[k] = ns[k] (ns[k] == "" ? "" : " ") $3; nns[k]++ }
    }
    $1 == "O" {
      split($4, ko, "/")
      if (!(ko[1] in cluster)) next
      k = $2 SUBSEP $4
      if (!((k, $3) in seen_obj)) { seen_obj[k, $3] = 1; obj[k] = obj[k] (obj[k] == "" ? "" : " ") $3; nobj[k]++ }
    }
    END {
      for (k in nns) if (nns[k] > 1) { split(k, p, SUBSEP); bad[p[1]] = 1
        print "FAIL " p[1] ": namespace " p[2] " is the destination of " owners(ns[k]) }
      for (k in nobj) if (nobj[k] > 1) { split(k, p, SUBSEP); bad[p[1]] = 1
        print "FAIL " p[1] ": " p[2] " is rendered by " owners(obj[k]) }
      for (srv in apps) if (!(srv in bad)) print "ok   " srv ": " apps[srv] " app(s), no shared namespace or cluster-scoped object"
    }' | sort
)
printf '%s\n' "$out"
exit "$(grep -c '^FAIL' <<<"$out")"
