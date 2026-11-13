# The Vault policy of the ohmlab controller's Vault target, as the vault-operator
# chart renders it (scripts/ci/check-controller-policy.sh keeps the two equal).
path "ohmlab/data/platforms/+/+/*" {
  capabilities = ["create", "update", "read"]
}
path "ohmlab/metadata/platforms/+/+/*" {
  capabilities = ["read", "update"]
}
# The claims' records are the storage target's.
path "ohmlab/data/platforms/+/+/+/s3" {
  capabilities = ["deny"]
}
path "ohmlab/metadata/platforms/+/+/+/s3" {
  capabilities = ["deny"]
}
