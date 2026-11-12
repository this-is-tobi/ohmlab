# The storage target's Vault policy in the e2e: write the claims' records,
# read the store's root keys. Phase 1b's rendered policy is checked against it.
path "ohmlab/data/platforms/+/+/+/s3"           { capabilities = ["create", "update", "read"] }
path "ohmlab/metadata/platforms/+/+/+/s3"       { capabilities = ["read", "update"] }
path "ohmlab/data/platforms/e2e/core/rustfs"    { capabilities = ["read"] }
