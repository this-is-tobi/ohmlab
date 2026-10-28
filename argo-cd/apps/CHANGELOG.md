# Changelog

## [0.9.1](https://github.com/this-is-tobi/homelab/compare/catalog-v0.9.0...catalog-v0.9.1) (2026-10-28)


### Bug Fixes

* **ohmlab-controller:** pull the image from the ohmlab project scope ([c2ab1f2](https://github.com/this-is-tobi/homelab/commit/c2ab1f2042fe8c1b6c6caba53396d3ebfa94b1db))

## [0.9.0](https://github.com/this-is-tobi/homelab/compare/catalog-v0.8.0...catalog-v0.9.0) (2026-10-28)


### Features

* **kyverno:** verify the images published under the ohmlab project scope ([aa7fa5e](https://github.com/this-is-tobi/homelab/commit/aa7fa5e086c5c010bac7c9d195833cffc61aa761))

## [0.8.0](https://github.com/this-is-tobi/homelab/compare/catalog-v0.7.0...catalog-v0.8.0) (2026-10-28)


### Features

* **longhorn:** extra StorageClasses by replica count, and a two-replica one for the homelab ([d3e1296](https://github.com/this-is-tobi/homelab/commit/d3e12963d35088b75ec5ec87f10a786608d5ca4e))


### Bug Fixes

* **deps:** update cnpg postgres images ([c8afdd1](https://github.com/this-is-tobi/homelab/commit/c8afdd17bf31560c4a7ff41ec9939ca0f93f942e))
* **deps:** update helm release argo-cd to v10.11.3 ([2a92810](https://github.com/this-is-tobi/homelab/commit/2a92810faee22320db288ab5d952e816d256d7be))
* **deps:** update helm release argo-cd to v10.11.6 ([ee50d8e](https://github.com/this-is-tobi/homelab/commit/ee50d8e33bfed1ab40f5c64eb6891f8909a0aeba))
* **deps:** update helm release coder to v2.38.3 ([71253cf](https://github.com/this-is-tobi/homelab/commit/71253cf8ff7ff476d017089e0a4e35d36c525bf6))
* **deps:** update helm release loki to v20.0.2 ([aef167c](https://github.com/this-is-tobi/homelab/commit/aef167c9a87237c174fdf39ef1225b0480e1f1f0))
* **deps:** update helm release trust-manager to v0.26.0 ([4846c0e](https://github.com/this-is-tobi/homelab/commit/4846c0e1f916a688993762358e5798b1f5de82ba))
* **deps:** update kube-prometheus-stack docker tag to v92.14.0 ([ae72298](https://github.com/this-is-tobi/homelab/commit/ae72298bb9f2b8d88bb7e603970c470530a47881))
* **deps:** update kube-prometheus-stack docker tag to v92.14.1 ([f8ab070](https://github.com/this-is-tobi/homelab/commit/f8ab07077894b7d4b61bae228e4794d85c4ad208))
* **prometheus-stack:** keep Prometheus's TSDB off NFS and out of snapshots ([28da936](https://github.com/this-is-tobi/homelab/commit/28da936d6430c7fbbb3bbab7a095d46f53c05e90))

## [0.7.0](https://github.com/this-is-tobi/homelab/compare/catalog-v0.6.0...catalog-v0.7.0) (2026-10-26)


### ⚠ BREAKING CHANGES

* **vault-operator:** the vault-operator chart no longer writes ohmlab.vault.secrets by itself. Enable the ohmlab-controller app with its Vault target, and ohmlab.controller.enabled and ohmlab.vault.seeds.enabled in the vault-operator values, before upgrading (docs/08-controller.md, "Migrating from the post-config Job"). After the upgrade, delete the leftover Vault role and policy by hand: `vault delete auth/kubernetes/role/vault-post-config` and `vault policy delete vault-post-config`.

### Features

* **deps:** update helm release loki to v20 ([2340ccf](https://github.com/this-is-tobi/homelab/commit/2340ccfa6578e4bceb14859a0bc2e4c8dfaa5f4d))
* **ohmlab-controller:** log in to Vault with a token meant for Vault only ([3bb3b0a](https://github.com/this-is-tobi/homelab/commit/3bb3b0af394cef10b46b6348e1f09577b02102aa))
* **vault-operator:** accept only Vault-audience tokens on the controller's role ([977e502](https://github.com/this-is-tobi/homelab/commit/977e502230e5e5a72d7a34794fb0a04a0cd623ba))
* **vault-operator:** accept only Vault-audience tokens on the VSO roles ([be2bc05](https://github.com/this-is-tobi/homelab/commit/be2bc050b1846bb27898124db856472e8c200891))
* **vso:** log in to Vault with a token meant for Vault only ([f1bc178](https://github.com/this-is-tobi/homelab/commit/f1bc1785c172448c33e97798b64b18efcd80b634))


### Bug Fixes

* **deps:** update ghcr.io/this-is-tobi/homelab/ohmlab-controller docker tag to v0.3.0 ([cfecae2](https://github.com/this-is-tobi/homelab/commit/cfecae2ffe32f1b1ede03ae7a1d0900087bc5e86))
* **deps:** update helm release loki to v19.4.0 ([8e55ccb](https://github.com/this-is-tobi/homelab/commit/8e55ccbfeb4401eef0016c0f896a705922743445))
* **deps:** update helm release loki to v20.0.1 ([7f5021c](https://github.com/this-is-tobi/homelab/commit/7f5021c47a27a99f470c57578edfcd4727015528))
* **deps:** update helm release rustfs to v1.0.4 ([c67d41b](https://github.com/this-is-tobi/homelab/commit/c67d41b7ef0a917e08c8cd36236cfb2cfe2f1b32))
* **ohmlab-controller:** let an adopter enable the controller as documented ([e34aa25](https://github.com/this-is-tobi/homelab/commit/e34aa2512d7ad5f8e8be1e73a67bff67558e6bbb))
* **vault-operator:** sync the VaultSeeds after the rest of the app ([2d0da14](https://github.com/this-is-tobi/homelab/commit/2d0da14454a4db0d61ec83c88d2cd9e1aacb26fa))


### Miscellaneous Chores

* **vault-operator:** retire the post-config Job ([0de8d27](https://github.com/this-is-tobi/homelab/commit/0de8d27b05333000b84df15f91c248d5478cf008))

## [0.6.0](https://github.com/this-is-tobi/homelab/compare/catalog-v0.5.0...catalog-v0.6.0) (2026-10-25)


### Features

* **ohmlab-controller:** add the controller's chart ([b2d121b](https://github.com/this-is-tobi/homelab/commit/b2d121b4e8d4bc0c7fce3fc7802482a79e2482c5))
* **ohmlab-controller:** run the controller in shadow on the homelab ([10353a1](https://github.com/this-is-tobi/homelab/commit/10353a1333116b81077d8bf7ab04db7916919e45))
* **utils:** add the VaultSeed API and an ohmlab validate command ([eefe372](https://github.com/this-is-tobi/homelab/commit/eefe372ce6d10bf8d9fcb68b5c1c02b2c505c2bf))
* **vault-operator:** apply identity groups through bank-vaults ([926990c](https://github.com/this-is-tobi/homelab/commit/926990ce5a46a3c6084b24d8cdcfdda7e58ae8d6))
* **vault-operator:** grant the controller its policy and render VaultSeed objects ([28c4b6c](https://github.com/this-is-tobi/homelab/commit/28c4b6c94dc4be24062cb36de71f858d7c8ba5ae))


### Bug Fixes

* **deps:** update dependency hashicorp/vault to v2.2.1 ([bf51040](https://github.com/this-is-tobi/homelab/commit/bf5104090ed1a3334b9352419f5b2fe55f50f1bd))
* **deps:** update gha-runner-scale-set docker tag to v0.15.1 ([8e8c4d6](https://github.com/this-is-tobi/homelab/commit/8e8c4d6848cfc4a43f3edc5a0f83f8e9c3503cb7))
* **deps:** update gha-runner-scale-set-controller docker tag to v0.15.1 ([604bbf8](https://github.com/this-is-tobi/homelab/commit/604bbf81500247672c2624a7a463b1f0b265aacf))
* **deps:** update ghcr.io/this-is-tobi/mirror/outline docker tag to v1.12.0 ([c008f8f](https://github.com/this-is-tobi/homelab/commit/c008f8f2dc88d484bb9b00f5eb5597d00e7dcef7))
* **deps:** update helm release alloy to v1.14.3 ([d2e9401](https://github.com/this-is-tobi/homelab/commit/d2e940140ae773d3c4f4c32b24c90a4208ddb0eb))
* **deps:** update helm release argo-cd to v10.11.1 ([b105631](https://github.com/this-is-tobi/homelab/commit/b105631701e6f48e724084787326616670f6822d))
* **deps:** update helm release cert-manager to v1.21.3 ([a3306df](https://github.com/this-is-tobi/homelab/commit/a3306df9733521f8b79ac65548152003a0729770))
* **deps:** update helm release cilium to v1.20.4 ([fd21c68](https://github.com/this-is-tobi/homelab/commit/fd21c6863d6754165010287c37061432052ce182))
* **deps:** update helm release coder to v2.38.2 ([7ae231a](https://github.com/this-is-tobi/homelab/commit/7ae231ab695564a906dd06c2bc729bb1793b4b58))
* **deps:** update helm release loki to v19.2.1 ([fee28fb](https://github.com/this-is-tobi/homelab/commit/fee28fbd87a140a6675076270d2c41b5a393da13))
* **deps:** update helm release loki to v19.3.0 ([201d34f](https://github.com/this-is-tobi/homelab/commit/201d34f85ec7e3490d3b1e7efb66d498a883863f))
* **deps:** update kube-prometheus-stack docker tag to v92.13.1 ([b657983](https://github.com/this-is-tobi/homelab/commit/b657983e2fd114c1222f74ecca511a7c6233e8bb))

## [0.5.0](https://github.com/this-is-tobi/homelab/compare/catalog-v0.4.0...catalog-v0.5.0) (2026-10-22)


### Features

* **deps:** update helm release loki to v19 ([ec858cf](https://github.com/this-is-tobi/homelab/commit/ec858cfc8afc675faba3b7c10762f1b4fd48bc0e))


### Bug Fixes

* **deps:** update ghcr.io/this-is-tobi/homelab/ohmlab docker tag to v0.1.3 ([9725091](https://github.com/this-is-tobi/homelab/commit/97250913848266ad2c57e10da24ed567f34d1668))
* **deps:** update helm release alloy to v1.14.1 ([553eb30](https://github.com/this-is-tobi/homelab/commit/553eb30fcf1d18954a19d13d67b1e5ba9f93bd7d))
* **deps:** update helm release alloy to v1.14.2 ([c60e364](https://github.com/this-is-tobi/homelab/commit/c60e3643ecab98de10835671c984517190f2e4a0))
* **deps:** update helm release argo-workflows to v2.0.14 ([ef98033](https://github.com/this-is-tobi/homelab/commit/ef98033700a5136953dd76744f533be28adf8ca7))
* **deps:** update helm release teleport-cluster to v18.11.7 ([52a2244](https://github.com/this-is-tobi/homelab/commit/52a2244271b355c75d05ed93205b9ff55000819c))
* **deps:** update helm release traefik to v42.1.3 ([ec6165e](https://github.com/this-is-tobi/homelab/commit/ec6165ea0f73a643eab23d7a0566a360365c75a4))
* **deps:** update keycloak docker tag to v0.21.49 ([eedbf75](https://github.com/this-is-tobi/homelab/commit/eedbf754cdf8f3f7b76292754059f4206ef21a85))
* **deps:** update kube-prometheus-stack docker tag to v92.11.0 ([9b3e7b6](https://github.com/this-is-tobi/homelab/commit/9b3e7b6b938156e5bcd00c1a50ebe42229c6dc6f))
* **deps:** update valkey docker tag to v0.25.15 ([8dbfb81](https://github.com/this-is-tobi/homelab/commit/8dbfb812f316dda3c35d7ca7f0bf942859a80ee5))
* **deps:** update valkey docker tag to v0.25.16 ([48b9d4b](https://github.com/this-is-tobi/homelab/commit/48b9d4b4aaa4f6cebd33e13a14166038678640f7))

## [0.4.0](https://github.com/this-is-tobi/homelab/compare/catalog-v0.3.1...catalog-v0.4.0) (2026-10-21)


### Features

* **deps:** update helm release traefik to v42 ([75874d4](https://github.com/this-is-tobi/homelab/commit/75874d47e967d0d6a531a4662b652092c0b35dd8))


### Bug Fixes

* **deps:** update cnpg postgres images ([d37ff3d](https://github.com/this-is-tobi/homelab/commit/d37ff3d7f95b1c2df1ee191179d018eb77816d48))
* **deps:** update ghcr.io/this-is-tobi/homelab/ohmlab docker tag to v0.1.2 ([ef269da](https://github.com/this-is-tobi/homelab/commit/ef269da2d0d629ca64175660a4fb2e78bf4a6d76))
* **deps:** update helm release coder to v2.38.1 ([ce7a7fb](https://github.com/this-is-tobi/homelab/commit/ce7a7fbf9e70b40e9602a54d587d1dc1117dad69))
* **deps:** update helm release kyverno to v3.9.2 ([6425cf9](https://github.com/this-is-tobi/homelab/commit/6425cf9ca49353fb48893f5eacfbc2772fc4b7da))
* **deps:** update helm release loki to v18.17.0 ([ecb6a52](https://github.com/this-is-tobi/homelab/commit/ecb6a52c095fda5493358790273e6e74efae4860))
* **deps:** update helm release loki to v18.17.1 ([b6410ae](https://github.com/this-is-tobi/homelab/commit/b6410ae16f7f9da6a1a5a8f4f3ef9aa85aca6735))
* **deps:** update helm release rustfs to v1.0.3 ([0f2abbe](https://github.com/this-is-tobi/homelab/commit/0f2abbea8d89f30a8e4191960420538e11237406))
* **deps:** update helm release teleport-cluster to v18.11.6 ([03f0362](https://github.com/this-is-tobi/homelab/commit/03f0362abe47f7c4f9fe1ed76826068683148bd1))
* **deps:** update helm release vault-secrets-operator to v1.6.2 ([2e3dcbb](https://github.com/this-is-tobi/homelab/commit/2e3dcbb3f37e83fd5ac878d869806fc3ae43e55c))
* **deps:** update keycloak docker tag to v0.21.48 ([edce52e](https://github.com/this-is-tobi/homelab/commit/edce52e9e75006ccc3c526de8763fa287aa0a148))
* **deps:** update kube-prometheus-stack docker tag to v92.10.1 ([1deabbb](https://github.com/this-is-tobi/homelab/commit/1deabbbdfe929369abf62cb29ec0d304971b7c20))

## [0.3.1](https://github.com/this-is-tobi/homelab/compare/catalog-v0.3.0...catalog-v0.3.1) (2026-10-16)


### Bug Fixes

* **deps:** update cnpg postgres images ([cc51a5c](https://github.com/this-is-tobi/homelab/commit/cc51a5cfe497c46adeff7fbec3d9893484bb1930))
* **deps:** update dependency hashicorp/vault to v2.2.0 ([c90661c](https://github.com/this-is-tobi/homelab/commit/c90661c7e2f978b138fa38541e92bcfae494c01f))
* **deps:** update ghcr.io/this-is-tobi/mirror/outline docker tag to v1.11.0 ([162f88d](https://github.com/this-is-tobi/homelab/commit/162f88dd7a693b2e82feb411161506b3167dab6d))
* **deps:** update helm release apisix to v2.18.1 ([5f333eb](https://github.com/this-is-tobi/homelab/commit/5f333eb99981e601966fe60fec464625cf33f422))
* **deps:** update helm release apisix to v2.18.2 ([43413ff](https://github.com/this-is-tobi/homelab/commit/43413ffa518a9fc2c2ee7016e7aef820e9ee37a2))
* **deps:** update helm release argo-cd to v10.10.3 ([bf4573b](https://github.com/this-is-tobi/homelab/commit/bf4573b0575661f4e1fef77b566ecb4602b67d0d))
* **deps:** update helm release argo-cd to v10.10.5 ([d8f595a](https://github.com/this-is-tobi/homelab/commit/d8f595a31caf9446511aa1f169eb18e8902134d1))
* **deps:** update helm release argo-workflows to v2.0.13 ([55dfa71](https://github.com/this-is-tobi/homelab/commit/55dfa719e38b000cd527e05493e6d20f905f415f))
* **deps:** update helm release cilium to v1.20.3 ([e49033b](https://github.com/this-is-tobi/homelab/commit/e49033b432a378e42d6c9d7f79b86dd8b0a0fe36))
* **deps:** update helm release harbor to v1.19.4 ([f6e7da4](https://github.com/this-is-tobi/homelab/commit/f6e7da43818018b6742a572088b155b3a95b5627))
* **deps:** update helm release loki to v18.16.2 ([6bceaac](https://github.com/this-is-tobi/homelab/commit/6bceaac86205e2a979478b53b00a7a9db59248a0))
* **deps:** update helm release mattermost-team-edition to v6.6.109 ([e830f03](https://github.com/this-is-tobi/homelab/commit/e830f03a6fdb416b342135cf70e7c6ee25c3e2ec))
* **deps:** update helm release rustfs to v1.0.2 ([2d2a094](https://github.com/this-is-tobi/homelab/commit/2d2a094e8116f62dc3168afa9d5de28743311dff))
* **deps:** update helm release teleport-cluster to v18.11.4 ([5673a88](https://github.com/this-is-tobi/homelab/commit/5673a88927bcfa2ad9a5afb627108078bc0433a9))
* **deps:** update kube-prometheus-stack docker tag to v92.4.0 ([af73a24](https://github.com/this-is-tobi/homelab/commit/af73a240f236fa0dc005a70d77e49b56930137a8))
* **deps:** update kube-prometheus-stack docker tag to v92.6.0 ([8a6efd0](https://github.com/this-is-tobi/homelab/commit/8a6efd045cedba90cc006def47e9a02cf74600ba))
* **deps:** update kube-prometheus-stack docker tag to v92.7.0 ([1be5967](https://github.com/this-is-tobi/homelab/commit/1be5967e482dc466eaa3f5dd7bb3e639253524db))

## [0.3.0](https://github.com/this-is-tobi/homelab/compare/catalog-v0.2.1...catalog-v0.3.0) (2026-10-11)


### Features

* **homepage:** use the hardened this-is-tobi homepage chart ([4e612e6](https://github.com/this-is-tobi/homelab/commit/4e612e64412963258540755123902658c79c2534))

## [0.2.1](https://github.com/this-is-tobi/homelab/compare/catalog-v0.2.0...catalog-v0.2.1) (2026-10-10)


### Bug Fixes

* **deps:** update ghcr.io/this-is-tobi/homelab/ohmlab docker tag to v0.1.1 ([75f2013](https://github.com/this-is-tobi/homelab/commit/75f2013cced9a85926d95d1f72f55ababea52ea6))
* **deps:** update kube-prometheus-stack docker tag to v92.3.0 ([7a38bfb](https://github.com/this-is-tobi/homelab/commit/7a38bfb5f906e6c512d26d9a654dab3ed65fee88))

## [0.2.0](https://github.com/this-is-tobi/homelab/compare/catalog-v0.1.0...catalog-v0.2.0) (2026-10-09)


### Features

* **kyverno:** replace every ClusterPolicy and legacy exception with CEL policies ([1794f9d](https://github.com/this-is-tobi/homelab/commit/1794f9da2f2a8b15062f93e877600d6fc7a2caad))


### Bug Fixes

* **deps:** update dependency hashicorp/vault to v2.1.2 ([bc42eb6](https://github.com/this-is-tobi/homelab/commit/bc42eb682aced7bd9780dae0754d65512b6e335c))
* **deps:** update helm release alloy to v1.13.1 ([d07aaf2](https://github.com/this-is-tobi/homelab/commit/d07aaf22df45b7e2591be15488b2780893af05b7))
* **deps:** update helm release argo-cd to v10.10.2 ([fa23f1a](https://github.com/this-is-tobi/homelab/commit/fa23f1ac16236af543136cfc2e6187df4777f463))
* **deps:** update helm release argo-workflows to v2.0.12 ([f0a355f](https://github.com/this-is-tobi/homelab/commit/f0a355f1f55350295f5b2c6ce3e33aeb7b516bf0))
* **deps:** update helm release loki to v18.15.1 ([956cc0b](https://github.com/this-is-tobi/homelab/commit/956cc0bd9f164bfd1d2c33dedfa6bd0c6fdd1787))
* **deps:** update helm release traefik to v41.7.1 ([8849af6](https://github.com/this-is-tobi/homelab/commit/8849af61ec9ca666f7d06aa608b1c1d46c464387))
* **deps:** update keycloak docker tag to v0.21.46 ([7ddb817](https://github.com/this-is-tobi/homelab/commit/7ddb8176a854a8426442277f718809ecc367a4bb))
* **deps:** update kube-prometheus-stack docker tag to v92.2.0 ([6e5c247](https://github.com/this-is-tobi/homelab/commit/6e5c24757bfcd85b18d1521c7a3f56284a482093))
* **kyverno:** read the CEL policy metrics in the denial alert and the dashboard ([72a5905](https://github.com/this-is-tobi/homelab/commit/72a59057693d2f8e9f5d8c2363654b1b00a65398))

## 0.1.0 (2026-10-06)


### Features

* **actions-runner-controller:** deliver the GitHub App from Vault ([25830cd](https://github.com/this-is-tobi/homelab/commit/25830cd8b946be3490905c2fac0657ad129a07e1))
* **instance-manager:** take an app's chart from a named registry ([128d483](https://github.com/this-is-tobi/homelab/commit/128d48388550180eb051cd95b7321708abdf761f))
* **instance-manager:** take charts from a bundle catalog ([0d2d992](https://github.com/this-is-tobi/homelab/commit/0d2d992e0acf4af4d145c9750d1bcaf068d05bb1))


### Bug Fixes

* **actions-runner-controller:** key the runner values by the dependency aliases ([9de40e0](https://github.com/this-is-tobi/homelab/commit/9de40e0549b2f3633b110f77dee09551f9686f24))
* **example:** make the example Vault config agree with the chart defaults ([a7ac740](https://github.com/this-is-tobi/homelab/commit/a7ac740fdf4f683cb7fc308a369e981175d31334))
* **vso:** keep the whole Vault path out of every Secret the operator writes ([1642b88](https://github.com/this-is-tobi/homelab/commit/1642b88a80be1cb96bd736f70eca3adfded6e251))
