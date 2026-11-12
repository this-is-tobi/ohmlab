# Changelog

## [0.9.0](https://github.com/this-is-tobi/ohmlab/compare/v0.8.0...v0.9.0) (2026-11-12)


### Features

* **api:** the S3Bucket kind ([b9fa668](https://github.com/this-is-tobi/ohmlab/commit/b9fa6683c25a6a85731716fe7a15c1f455a34ff9))
* **controller:** a RustFS client for buckets and identities ([fe9869f](https://github.com/this-is-tobi/ohmlab/commit/fe9869f07abe9bb63b1d9e518064f953ab858f91))
* **controller:** configure the storage target ([35c8f42](https://github.com/this-is-tobi/ohmlab/commit/35c8f42b2e9d707547b58b59fee522ccc01606bd))
* **controller:** reconcile S3Buckets into RustFS and their Vault records ([01fe7ef](https://github.com/this-is-tobi/ohmlab/commit/01fe7ef514b913d51c7ac796e6f9b773e350be2a))


### Bug Fixes

* **controller:** bound every store call of the storage target ([2d09e6f](https://github.com/this-is-tobi/ohmlab/commit/2d09e6fd1933bd698f0f41bfa0822d483a30a150))
* **controller:** keep a rotated key's grace when Vault fails between two writes ([ce788c0](https://github.com/this-is-tobi/ohmlab/commit/ce788c0ed943808c39b87867b95cbed8b4925d9f))
* **controller:** let a claim's deletion finish when its bucket is gone ([bb17ebc](https://github.com/this-is-tobi/ohmlab/commit/bb17ebc8aa6fb17e6dd0aea89997ebedb6679169))
* **controller:** refuse a second claim on a bucket through another record ([740dd2e](https://github.com/this-is-tobi/ohmlab/commit/740dd2e522f8ecfb42fa8d3a2cdae677a61353e4))
* **deps:** update dependency hashicorp/vault to v2.2.2 ([dfecb50](https://github.com/this-is-tobi/ohmlab/commit/dfecb50c9b5cf7b0f1c0a008266e3737702f9e8b))
* **deps:** update module golang.org/x/crypto to v0.59.0 ([3dd8939](https://github.com/this-is-tobi/ohmlab/commit/3dd8939b38a3daa40486dba0d03b22d19986a3e5))

## [0.8.0](https://github.com/this-is-tobi/ohmlab/compare/v0.7.0...v0.8.0) (2026-11-08)


### Features

* **ohmlab-controller:** keep the certificates target's patches to its annotation ([5bc1308](https://github.com/this-is-tobi/ohmlab/commit/5bc130860a27b5870846fbac3b9965471562a69d))

## [0.7.0](https://github.com/this-is-tobi/ohmlab/compare/v0.6.0...v0.7.0) (2026-11-08)


### Features

* **cli:** add ohmlab provision ([0dedafa](https://github.com/this-is-tobi/ohmlab/commit/0dedafaa24fe92ffff7a235b05bbed7b385830ab))
* **cli:** add ohmlab sops ([3c12b0d](https://github.com/this-is-tobi/ohmlab/commit/3c12b0df1399b08946c580c4c95b107c74d8f2e7))
* **cli:** ask for a typed word before a step that cannot be undone ([9508e3f](https://github.com/this-is-tobi/ohmlab/commit/9508e3f32df1f4d0f54144b777049eb24c58528a))
* **cli:** build the provision commands and a kubeconfig holding one context ([553b138](https://github.com/this-is-tobi/ohmlab/commit/553b13814a1b3bde4f26ebccfd17aa9788ac7d12))
* **controller:** configure the certificates target ([e9cf6e2](https://github.com/this-is-tobi/ohmlab/commit/e9cf6e2504d2d5b95ea6328b508f5eee5ed769b9))
* **controller:** roll workloads when their certificate is issued again ([9332415](https://github.com/this-is-tobi/ohmlab/commit/9332415f10cc608bd9aa6d672247b04121179b39))


### Bug Fixes

* **cli:** give ansible the vault password file through its environment ([075ce0d](https://github.com/this-is-tobi/ohmlab/commit/075ce0d0a6d62d03673dc8920b4610e1db00184f))
* **cli:** remove the fetched kubeconfig on a hangup too ([b7e8749](https://github.com/this-is-tobi/ohmlab/commit/b7e87493bbd88c14eec0827bd0dd43f2b7223c2c))

## [0.6.0](https://github.com/this-is-tobi/ohmlab/compare/v0.5.1...v0.6.0) (2026-11-07)


### Features

* **cli:** fetch the gateway api bundle at run.sh's pinned version ([6831d76](https://github.com/this-is-tobi/ohmlab/commit/6831d76dd55a7c94e54cbdb6ae32c1fc6206cc4f))
* **cli:** ohmlab argocd password ([301a5d1](https://github.com/this-is-tobi/ohmlab/commit/301a5d1511d067280ce9351727d7ec4bed9ed1b3))
* **cli:** ohmlab bootstrap ([9d982b1](https://github.com/this-is-tobi/ohmlab/commit/9d982b1c02e452ac71f1a8918b322cf84f968448))
* **cli:** resolve an instance's bootstrap chart and release ([50f189a](https://github.com/this-is-tobi/ohmlab/commit/50f189a0f6a5ff949d191a7bef4bbed573672147))
* **cli:** the argo cd admin password of a bootstrap ([983b39b](https://github.com/this-is-tobi/ohmlab/commit/983b39bc9f3239383148bb5d842b57d1cb5f9baa))
* **cli:** the bootstrap engine ([6d6af5e](https://github.com/this-is-tobi/ohmlab/commit/6d6af5efb9b5e2b373fdd8ed720701e87691463f))
* **cli:** the cluster side of a bootstrap ([053a574](https://github.com/this-is-tobi/ohmlab/commit/053a57415c46f6bea23455f90b96b1655341fbcd))


### Bug Fixes

* **cli:** aim bootstrap's checks and hints at the context it confirmed ([b550e05](https://github.com/this-is-tobi/ohmlab/commit/b550e05ffc2c6a3df1893f3c10255d19ee66f830))
* **cli:** give an interrupted helm time to mark its release failed ([cee4e3f](https://github.com/this-is-tobi/ohmlab/commit/cee4e3ffdc3d77b06b03bfca6cce5e49ef6dc9ca))
* **cli:** keep the admin password when a bootstrap is given none ([3b9e8d5](https://github.com/this-is-tobi/ohmlab/commit/3b9e8d5b4e102d3f797dd673923f1ed13c8f2782))
* **cli:** set a given admin password from bootstrap's first phase ([6585fc3](https://github.com/this-is-tobi/ohmlab/commit/6585fc30cbe61616c295d85904b0f3d9d52f874f))

## [0.5.1](https://github.com/this-is-tobi/ohmlab/compare/v0.5.0...v0.5.1) (2026-11-06)


### Bug Fixes

* **deps:** update module github.com/spf13/pflag to v1.1.0 ([1d95e9a](https://github.com/this-is-tobi/ohmlab/commit/1d95e9ab014854063cb5d16e908e3bfb72c6fa65))

## [0.5.0](https://github.com/this-is-tobi/ohmlab/compare/v0.4.0...v0.5.0) (2026-11-05)


### Features

* **cli:** classify what a port answers to a TLS hello ([df0e722](https://github.com/this-is-tobi/ohmlab/commit/df0e72271bff504161eed0313f6b54e80d934ec2))
* **cli:** fetch a cluster's kubeconfig and merge it as a named context ([4b1e35c](https://github.com/this-is-tobi/ohmlab/commit/4b1e35c1276a40d8ee58f99aa23441d72012b6b9))
* **cli:** find every Service port and judge the probes ([5535451](https://github.com/this-is-tobi/ohmlab/commit/5535451cb8ac470ed1a6cf5289d853f0ebadad20))
* **cli:** find the instance to act on ([e5e07c9](https://github.com/this-is-tobi/ohmlab/commit/e5e07c9a9ef2e9202f67f0fe19ca2377646c8afd))
* **cli:** logs for people on a terminal, JSON elsewhere ([8bdaf68](https://github.com/this-is-tobi/ohmlab/commit/8bdaf68a45acb784d362006095c194a78a215b8e))
* **cli:** ohmlab audit tls ([a64fc5d](https://github.com/this-is-tobi/ohmlab/commit/a64fc5d981a2be492dfb0ac2c43aaf948932b42b))
* **cli:** ohmlab kubeconfig ([2f9346e](https://github.com/this-is-tobi/ohmlab/commit/2f9346e786e7cbb3a082ac0a8225258db1838e24))
* **cli:** output for a terminal or for machines ([cc4bcc2](https://github.com/this-is-tobi/ohmlab/commit/cc4bcc239decd3fa7b83722893fd491c560f72c2))
* **cli:** read the workspace file ([789927d](https://github.com/this-is-tobi/ohmlab/commit/789927d8d9c3cf08c0b6143bddc24e56aa7ecb69))
* **cli:** run external tools through one interface ([fcbde8e](https://github.com/this-is-tobi/ohmlab/commit/fcbde8eec47684f24023c4dfeab690ee1532a7ef))
* **cli:** the workspace settings, from flags, environment, ohmlab.yaml or defaults ([41b6f16](https://github.com/this-is-tobi/ohmlab/commit/41b6f1615c18a64bf245dd89ada0322ea646fdaa))

## [0.4.0](https://github.com/this-is-tobi/ohmlab/compare/v0.3.1...v0.4.0) (2026-11-01)


### ⚠ BREAKING CHANGES

* **harbor:** the harbor config command drops --keycloak-namespace and --keycloak-secret (KEYCLOAK_NAMESPACE, KEYCLOAK_SECRET_NAME); pass the Keycloak domain and realm with --keycloak-domain/KEYCLOAK_DOMAIN and --keycloak-realm/KEYCLOAK_REALM.
* **keycloak:** the realm import moves from keycloakConfigCli.config.homelab.inline to realms.defaults, realms.system and realms.instance; an instance that overrides the old keys moves its overrides there.
* a chart rendered without the instance-manager now falls back to the KV mount ohmlab, the realm ohmlab shown as Ohmlab and the Gateway ohmlab-gateway, and the Traefik Gateway follows names.gateway instead of names.traefikGateway.

### Features

* fall back to the ohmlab names, with one Gateway name for every implementation ([22107ee](https://github.com/this-is-tobi/ohmlab/commit/22107eeba71535008e41673c0045041e3e9cc4d7))
* **keycloak:** import the ohmlab system realm next to the instance's ([c00f057](https://github.com/this-is-tobi/ohmlab/commit/c00f0573a0bd34938862e2409e63d60bfe7a63b1))


### Bug Fixes

* **deps:** update kubernetes monorepo to v0.37.2 ([3d1b629](https://github.com/this-is-tobi/ohmlab/commit/3d1b6298d27a57cffc478aebb6f23e770f646c2e))
* **harbor:** take the Keycloak domain and realm only from flags or env ([af51bc4](https://github.com/this-is-tobi/ohmlab/commit/af51bc44fb4adf0a0d7323a74d443e80ec5693ac))

## [0.3.1](https://github.com/this-is-tobi/ohmlab/compare/v0.3.0...v0.3.1) (2026-10-31)


### Bug Fixes

* **utils:** name the product ohmlab in the CLI help ([2723a68](https://github.com/this-is-tobi/ohmlab/commit/2723a6883a4344d456902e78e92ddc9578bc0eda))

## [0.3.0](https://github.com/this-is-tobi/homelab/compare/v0.2.0...v0.3.0) (2026-10-25)


### Features

* **utils:** name the keys behind each change the controller plans or writes ([a63ff25](https://github.com/this-is-tobi/homelab/commit/a63ff2508cf2ce745f39b2387625f8cb007ca477))

## [0.2.0](https://github.com/this-is-tobi/homelab/compare/v0.1.3...v0.2.0) (2026-10-25)


### Features

* **utils:** add a context-aware KV v2 client with compare-and-set ([b349924](https://github.com/this-is-tobi/homelab/commit/b3499245a36398ebe82b027095ccbb7172e7acda))
* **utils:** add the ohmlab-controller binary ([5f1e15a](https://github.com/this-is-tobi/homelab/commit/5f1e15aae3880694af38e4e9e5ea99583bf000d1))
* **utils:** add the planner that decides what a VaultSeed changes ([4093582](https://github.com/this-is-tobi/homelab/commit/4093582bd091d6d5d35d5915ef53b90bcef2c2f8))
* **utils:** add the seeding loop and test it against a real Vault ([58de584](https://github.com/this-is-tobi/homelab/commit/58de584e191742659b3a98a548662b4cd4c20794))
* **utils:** add the VaultSeed API and an ohmlab validate command ([eefe372](https://github.com/this-is-tobi/homelab/commit/eefe372ce6d10bf8d9fcb68b5c1c02b2c505c2bf))
* **utils:** add the VaultSeed reconciler ([a425998](https://github.com/this-is-tobi/homelab/commit/a425998947f4d3bdce71a10a412c534984860bda))
* **utils:** publish the controller image ([fe676ee](https://github.com/this-is-tobi/homelab/commit/fe676ee548d396a8456a6329e52a2217304dde43))


### Bug Fixes

* **deps:** update dependency hashicorp/vault to v2.2.1 ([bf51040](https://github.com/this-is-tobi/homelab/commit/bf5104090ed1a3334b9352419f5b2fe55f50f1bd))
* **deps:** update module github.com/go-logr/logr to v1.4.4 ([b71e456](https://github.com/this-is-tobi/homelab/commit/b71e45651daea987cfc7eabb70a7d44a53f3036b))

## [0.1.3](https://github.com/this-is-tobi/homelab/compare/v0.1.2...v0.1.3) (2026-10-22)


### Bug Fixes

* **utils:** build with Go 1.27.3 for the crypto/x509 fix ([060d180](https://github.com/this-is-tobi/homelab/commit/060d18058884b9633d5d5ab85e8d8c948368025c))

## [0.1.2](https://github.com/this-is-tobi/homelab/compare/v0.1.1...v0.1.2) (2026-10-16)


### Bug Fixes

* **deps:** update dependency hashicorp/vault to v2.2.0 ([c90661c](https://github.com/this-is-tobi/homelab/commit/c90661c7e2f978b138fa38541e92bcfae494c01f))

## [0.1.1](https://github.com/this-is-tobi/homelab/compare/v0.1.0...v0.1.1) (2026-10-09)


### Bug Fixes

* **deps:** update dependency hashicorp/vault to v2.1.2 ([bc42eb6](https://github.com/this-is-tobi/homelab/commit/bc42eb682aced7bd9780dae0754d65512b6e335c))
* **deps:** update module golang.org/x/crypto to v0.58.0 ([788f96f](https://github.com/this-is-tobi/homelab/commit/788f96f4ba66904aa4e62347c3bc7fd8eb0df1fc))
* **utils:** build with Go 1.27.2 for the net/http fixes ([02b09bb](https://github.com/this-is-tobi/homelab/commit/02b09bb5be0bfba48a7bde5431d0fc1c3887ba6c))

## 0.1.0 (2026-10-03)


### Features

* **install:** make CNI and ServiceLB user-selectable ([6543183](https://github.com/this-is-tobi/homelab/commit/6543183a0fe63625a34c4e3f53282c0aa341a8c4))
* **k3s:** upgrade to v1.36.4 and bump k3s pins in lockstep ([69b281e](https://github.com/this-is-tobi/homelab/commit/69b281e01198dd0ad45e3f8fd7289f4d2c012e85))
* **netpol:** zero-trust NetworkPolicy design for all 15 app namespaces ([ee51fa1](https://github.com/this-is-tobi/homelab/commit/ee51fa1cc35311acb796fd200594ddebd1bb1130))
* **ohmlab:** add dormant CoreDNS capability, disabled by default ([cc8b30a](https://github.com/this-is-tobi/homelab/commit/cc8b30ae3f67453a7468fbc8a1b781f3bde7efd7))
* **ohmlab:** allow CNPG ClusterImageCatalog in the admin-core project ([f257d9c](https://github.com/this-is-tobi/homelab/commit/f257d9cacf0b5ad6e49dcf7ab408addef2c85ae6))
* **traefik-migration:** dual-attach every route to both gateways ([a6f07c1](https://github.com/this-is-tobi/homelab/commit/a6f07c18e383d3778159cd3e25a49b2f7a838f21))
* **traefik-migration:** gitops UIs over Gateway API with verified TLS upstream ([ce8dbe9](https://github.com/this-is-tobi/homelab/commit/ce8dbe9c156acb2d4f9063fb4a034be0072d5aac))
* unified vault post-config with declarative identity management ([0b207df](https://github.com/this-is-tobi/homelab/commit/0b207df075cabbef840a6cc95f20adbd4110a47f))
* **utils:** ohmlab check — read-only platform health sweep ([00e51ae](https://github.com/this-is-tobi/homelab/commit/00e51ae80ab101e55661a4dbdea73c31ccc2f175))
* **vault:** add a &lt;hex:N&gt; placeholder to the secret templating engine ([dda0e6a](https://github.com/this-is-tobi/homelab/commit/dda0e6afa78aefc5990e03017da2d6e05855a37b))


### Bug Fixes

* **argo-cd:** end-to-end TLS, single exposure path, resource limits ([df72fa3](https://github.com/this-is-tobi/homelab/commit/df72fa3bddf007a344032855cb826dd67f50e288))
* **argo-cd:** fix helm rendering errors for vault-operator and ohmlab ([83bd559](https://github.com/this-is-tobi/homelab/commit/83bd559169eb39219b181f4b7f1c4f59af469d83))
* **argo-cd:** raise core app-controller memory limit to 2Gi ([4e3b670](https://github.com/this-is-tobi/homelab/commit/4e3b670d299221a9414bbae98b04feeedfb927f8))
* **argo-cd:** stop reserving 250m per split-brain check, more controller headroom ([375a45b](https://github.com/this-is-tobi/homelab/commit/375a45b89c901493b98f069c2d8f000a7a95fefc))
* **coredns:** set dnsPolicy Default on the optional HA CoreDNS ([f1edab3](https://github.com/this-is-tobi/homelab/commit/f1edab37c32d0b664d5a2f46b5c8ae4ce5904e46))
* **deps:** update module filippo.io/age to v1.3.2 ([ec364aa](https://github.com/this-is-tobi/homelab/commit/ec364aab561759d50044a2f8ca95b53c40a20144))
* **deps:** update module golang.org/x/crypto to v0.55.0 ([a246f18](https://github.com/this-is-tobi/homelab/commit/a246f18883bc9953ef5373cd2e4ca1f888005716))
* **deps:** update module golang.org/x/crypto to v0.56.0 ([4e5dc5a](https://github.com/this-is-tobi/homelab/commit/4e5dc5aad7123fab77885894c3c6fcf141e4743f))
* **deps:** update module golang.org/x/crypto to v0.57.0 ([3a6f5d1](https://github.com/this-is-tobi/homelab/commit/3a6f5d15727ce55b417baaf4b7de5648b0093c11))
* **gitops:** never cascade-delete resources when an Application is deleted ([0e9b4a7](https://github.com/this-is-tobi/homelab/commit/0e9b4a77cc3aa1fa9df8e8a501ec81621cbfd304))
* **ohmlab:** rbac policy.default/scopes under configs.rbac, enable RollingSync gate ([453b015](https://github.com/this-is-tobi/homelab/commit/453b01541b8fec7ac3ac34a206024e74df7752f8))
* **resources:** size heavy pods by real usage, give bursty sidecars CPU headroom ([9554230](https://github.com/this-is-tobi/homelab/commit/9554230891363ffb847b846ff97fc092f716d3b3))
* **utils:** pin the Go toolchain so CI and releases build with a patched stdlib ([22319da](https://github.com/this-is-tobi/homelab/commit/22319da79a38e79c796530ab0c8034cc0fd9d79f))
* **utils:** read VSO ≥1.4 Ready condition in the vault-secrets probe ([f40771d](https://github.com/this-is-tobi/homelab/commit/f40771dfc6e1f599f01d77bd053a7334aa6f10c1))
* **utils:** remove obsolete cmd/helpers.go after package reorganization ([0ffbdb7](https://github.com/this-is-tobi/homelab/commit/0ffbdb7ac1e3e0413367a2edd782cf487d403a89))


### Performance Improvements

* **argo-cd:** raise core app-controller CPU burst limit to 2 cores ([cee338d](https://github.com/this-is-tobi/homelab/commit/cee338d7ad465668228662b8a15300f440c6050c))
