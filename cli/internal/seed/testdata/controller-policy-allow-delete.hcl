# The same policy with deletion allowed (ohmlab.controller.allowDelete). The
# controller only ever calls DELETE on the metadata path, which removes every
# version and the metadata, so delete is granted there and nowhere else.
path "ohmlab/data/platforms/+/+/*" {
  capabilities = ["create", "update", "read"]
}
path "ohmlab/metadata/platforms/+/+/*" {
  capabilities = ["read", "update", "delete"]
}
