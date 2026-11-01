# The Vault policy of the ohmlab controller's Vault target, as the vault-operator
# chart renders it (scripts/ci/check-controller-policy.sh keeps the two equal).
path "ohmlab/data/platforms/+/+/*" {
  capabilities = ["create", "update", "read"]
}
path "ohmlab/metadata/platforms/+/+/*" {
  capabilities = ["read", "update"]
}
