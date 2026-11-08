# Changelog

## [0.7.0](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.6.3...chart-v0.7.0) (2026-11-08)


### Features

* **ohmlab:** let core apps sync validating admission policies ([255fc83](https://github.com/this-is-tobi/ohmlab/commit/255fc83e3221c1b33ce17b298dd1acf935753426))


### Bug Fixes

* **deps:** update helm release argo-cd to v10.17.0 ([698aa04](https://github.com/this-is-tobi/ohmlab/commit/698aa0441fc890f1c963acb75b74614eaab65e77))

## [0.6.3](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.6.2...chart-v0.6.3) (2026-11-08)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.15.0 ([61dddbf](https://github.com/this-is-tobi/ohmlab/commit/61dddbf8ad1792ed10e0ae184f8336f578fce1da))

## [0.6.2](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.6.1...chart-v0.6.2) (2026-11-06)


### Bug Fixes

* **deps:** update vso-utils docker tag to v2.4.0 ([18cec5b](https://github.com/this-is-tobi/ohmlab/commit/18cec5ba55d13a8780ce64c159ba5098f0f9be8a))

## [0.6.1](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.6.0...chart-v0.6.1) (2026-11-06)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.14.0 ([c0fdb49](https://github.com/this-is-tobi/ohmlab/commit/c0fdb491937032c28731cb52fe0aa13401b3829c))

## [0.6.0](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.5.4...chart-v0.6.0) (2026-11-05)


### Features

* **charts:** the VaultAuth of every app follows its instance ([99cc10f](https://github.com/this-is-tobi/ohmlab/commit/99cc10f35557f7b45e737385c462c9e8c8c08afe))
* **ohmlab:** projects that inherit a tier's rules ([107da15](https://github.com/this-is-tobi/ohmlab/commit/107da152d2c5b3149f1e7860b6573edfe8928074))
* **ohmlab:** register remote clusters in Argo CD from Vault ([2f65826](https://github.com/this-is-tobi/ohmlab/commit/2f6582672b400dbc0ecad7f426cb1e0ac84bb901))


### Bug Fixes

* **deps:** update vso-utils docker tag to v2.3.0 ([e6645c7](https://github.com/this-is-tobi/ohmlab/commit/e6645c739b92c0b5a8cd4325c40c13ad76b2eb1f))
* **deps:** update vso-utils docker tag to v2.3.1 ([9d9a725](https://github.com/this-is-tobi/ohmlab/commit/9d9a725365cbeefa9b5f404a64f954cdfd5af05c))

## [0.5.4](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.5.3...chart-v0.5.4) (2026-11-04)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.12.1 ([4ed6117](https://github.com/this-is-tobi/ohmlab/commit/4ed6117a3645325d6b457dc56a260fb5d018c5e3))

## [0.5.3](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.5.2...chart-v0.5.3) (2026-11-04)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.12.0 ([5ff210b](https://github.com/this-is-tobi/ohmlab/commit/5ff210b4137d7a8bdf082cf82a4e910e82238917))

## [0.5.2](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.5.1...chart-v0.5.2) (2026-11-04)


### Bug Fixes

* **deps:** update vso-utils docker tag to v2.2.1 ([c25e152](https://github.com/this-is-tobi/ohmlab/commit/c25e152bd42436258315afb828c423fbcd9d38c9))

## [0.5.1](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.5.0...chart-v0.5.1) (2026-11-03)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.11.7 ([c9a2825](https://github.com/this-is-tobi/ohmlab/commit/c9a28259df1ea6be0812ae8a607cd119c76e5bd2))

## [0.5.0](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.4.2...chart-v0.5.0) (2026-11-02)


### ⚠ BREAKING CHANGES

* **ohmlab:** the chart is no longer at utils/helm. An instance whose core.yaml gives the ohmlab app chartPath: utils/helm, or reads the chart from a catalog bundle by that path, now uses charts/ohmlab.

### Code Refactoring

* **ohmlab:** move the bootstrap chart to charts/ohmlab ([c6c6630](https://github.com/this-is-tobi/ohmlab/commit/c6c663071ea73c727daca26de35f00b8fb99dd42))

## [0.4.2](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.4.1...chart-v0.4.2) (2026-11-02)


### Bug Fixes

* give argocd-redis, the Prometheus admission Jobs, vault and the apisix controller limits ([e8f6046](https://github.com/this-is-tobi/ohmlab/commit/e8f6046554ca839feae8bd5d127f9e4b0e18a59b))

## [0.4.1](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.4.0...chart-v0.4.1) (2026-11-02)


### Bug Fixes

* **ohmlab:** run one Argo CD application controller ([53e9cd2](https://github.com/this-is-tobi/ohmlab/commit/53e9cd2d95c526f21e0a22ecce3ac24d79697721))

## [0.4.0](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.3.1...chart-v0.4.0) (2026-11-01)


### ⚠ BREAKING CHANGES

* a chart rendered without the instance-manager now falls back to the KV mount ohmlab, the realm ohmlab shown as Ohmlab and the Gateway ohmlab-gateway, and the Traefik Gateway follows names.gateway instead of names.traefikGateway.

### Features

* default every chart's Vault mount and path to the manager's ([401bd18](https://github.com/this-is-tobi/ohmlab/commit/401bd18fae8227acc5a0eb9d00f4a3e9c14c983f))
* fall back to the ohmlab names, with one Gateway name for every implementation ([22107ee](https://github.com/this-is-tobi/ohmlab/commit/22107eeba71535008e41673c0045041e3e9cc4d7))
* take the gateway names from the instance, with today's names as defaults ([a8f949c](https://github.com/this-is-tobi/ohmlab/commit/a8f949c25ef70a9df5329cde7647106a82be91ec))

## [0.3.1](https://github.com/this-is-tobi/ohmlab/compare/chart-v0.3.0...chart-v0.3.1) (2026-10-31)


### Bug Fixes

* **ohmlab:** default the root manager to the ohmlab repository ([fd7e725](https://github.com/this-is-tobi/ohmlab/commit/fd7e7250d7e7f5d4309a1ac12195b8730eb9006b))
* **ohmlab:** name the AppProjects' default descriptions after ohmlab ([dbc19df](https://github.com/this-is-tobi/ohmlab/commit/dbc19dfbf0a5cf53457550fc792c14e235ee96ab))

## [0.3.0](https://github.com/this-is-tobi/homelab/compare/chart-v0.2.1...chart-v0.3.0) (2026-10-28)


### Features

* **ohmlab:** let the root manager take git or OCI sources and a separate values repository ([63ce890](https://github.com/this-is-tobi/homelab/commit/63ce890fc0248ada68e57659e98e7a5b70977d52))

## [0.2.1](https://github.com/this-is-tobi/homelab/compare/chart-v0.2.0...chart-v0.2.1) (2026-10-28)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.11.3 ([2a92810](https://github.com/this-is-tobi/homelab/commit/2a92810faee22320db288ab5d952e816d256d7be))
* **deps:** update helm release argo-cd to v10.11.6 ([ee50d8e](https://github.com/this-is-tobi/homelab/commit/ee50d8e33bfed1ab40f5c64eb6891f8909a0aeba))

## [0.2.0](https://github.com/this-is-tobi/homelab/compare/chart-v0.1.7...chart-v0.2.0) (2026-10-26)


### Features

* **vso:** log in to Vault with a token meant for Vault only ([f1bc178](https://github.com/this-is-tobi/homelab/commit/f1bc1785c172448c33e97798b64b18efcd80b634))

## [0.1.7](https://github.com/this-is-tobi/homelab/compare/chart-v0.1.6...chart-v0.1.7) (2026-10-25)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.11.1 ([b105631](https://github.com/this-is-tobi/homelab/commit/b105631701e6f48e724084787326616670f6822d))
* **deps:** update rancher/mirrored-coredns-coredns docker tag to v1.15.0 ([105d9de](https://github.com/this-is-tobi/homelab/commit/105d9de5d6a7f6a968130e203e155bc6b6c94674))

## [0.1.6](https://github.com/this-is-tobi/homelab/compare/chart-v0.1.5...chart-v0.1.6) (2026-10-16)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.10.5 ([d8f595a](https://github.com/this-is-tobi/homelab/commit/d8f595a31caf9446511aa1f169eb18e8902134d1))

## [0.1.5](https://github.com/this-is-tobi/homelab/compare/chart-v0.1.4...chart-v0.1.5) (2026-10-14)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.10.3 ([bf4573b](https://github.com/this-is-tobi/homelab/commit/bf4573b0575661f4e1fef77b566ecb4602b67d0d))

## [0.1.4](https://github.com/this-is-tobi/homelab/compare/chart-v0.1.3...chart-v0.1.4) (2026-10-09)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.10.2 ([fa23f1a](https://github.com/this-is-tobi/homelab/commit/fa23f1ac16236af543136cfc2e6187df4777f463))
