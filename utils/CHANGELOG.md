# Changelog

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
