# Changelog

## [0.21.1](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.21.0...catalog-v0.21.1) (2026-11-14)


### Bug Fixes

* **cnpg:** take the base backups on the primary ([5cb2e52](https://github.com/this-is-tobi/ohmlab/commit/5cb2e52b2a0fc343d8c3e23df3573b1fc2ccd250))
* **deps:** update backup-utils docker tag to v2.6.1 ([7fb6e5a](https://github.com/this-is-tobi/ohmlab/commit/7fb6e5a59607a053bf1f2b59b785e096135c2d6d))
* **deps:** update helm release alloy to v1.16.0 ([cdafe92](https://github.com/this-is-tobi/ohmlab/commit/cdafe9295dde5a97fb17010cf0fef81ce63aae2c))
* **deps:** update helm release apisix to v2.19.0 ([5889cee](https://github.com/this-is-tobi/ohmlab/commit/5889cee877eef6803f1dff12beec54ae5ddf68e2))
* **deps:** update homepage docker tag to v0.3.0 ([2855455](https://github.com/this-is-tobi/ohmlab/commit/2855455dbcbc4151f49ad0118b9ed365d45132e0))
* **rustfs:** make a store wait for the instance-manager's wiring ([77b12ef](https://github.com/this-is-tobi/ohmlab/commit/77b12ef882293808d568465f77bf966c9a930262))
* **storage:** leave out the egress policy of a claim whose namespace keeps egress open ([8827119](https://github.com/this-is-tobi/ohmlab/commit/8827119c4286a6a46197d4b9878426fcea39bfea))
* **vault-operator:** address the store's buckets by path in the snapshot job ([2074b34](https://github.com/this-is-tobi/ohmlab/commit/2074b348a8cb4dc3c84212cfb071087eeb199b82))

## [0.21.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.20.0...catalog-v0.21.0) (2026-11-13)


### Features

* **catalog:** back up Postgres to the app's storage claim ([a58ae21](https://github.com/this-is-tobi/ohmlab/commit/a58ae216e33089aca31041a15b2d7c319c9dceba))
* **instance-manager:** a storage registry that wires each app to its claims ([669199b](https://github.com/this-is-tobi/ohmlab/commit/669199b827a429b03a877f9eefb0c75d9569324f))
* **ohmlab-controller:** alert on stale Postgres and Vault backups ([46b69f7](https://github.com/this-is-tobi/ohmlab/commit/46b69f7ec00e54d70dab6e61dbacbf961da230e7))
* **storage-claims:** render the claims of a tier's enabled apps ([6a94c63](https://github.com/this-is-tobi/ohmlab/commit/6a94c6348460b89934fb30b7018eb37989ba7714))
* **vault-operator:** set up Vault for the storage target and the snapshot job ([63dbb20](https://github.com/this-is-tobi/ohmlab/commit/63dbb20dace69511d90a06340d341f409b0d0114))


### Bug Fixes

* **deps:** backup-utils 2.6.0 ([69ef6af](https://github.com/this-is-tobi/ohmlab/commit/69ef6afdb1866e4444aede5f44ad50eca06480f6))
* **deps:** update cnpg postgres images ([8dfb16b](https://github.com/this-is-tobi/ohmlab/commit/8dfb16bb5f852eeea968c28ba633663c57de033f))
* **deps:** update ghcr.io/this-is-tobi/ohmlab/controller docker tag to v0.10.0 ([3ff34e2](https://github.com/this-is-tobi/ohmlab/commit/3ff34e2b6965a11d1857037610a1bd6d9188a321))
* **deps:** update ghcr.io/this-is-tobi/ohmlab/controller docker tag to v0.9.0 ([2c0debf](https://github.com/this-is-tobi/ohmlab/commit/2c0debf71a42789bb6dfd7e035f2da3629c82898))
* **deps:** update helm release loki to v21.2.3 ([1d0bb59](https://github.com/this-is-tobi/ohmlab/commit/1d0bb592f3c2c59313555370491c17f982a70305))
* **deps:** update helm release sops-secrets-operator to v0.30.0 ([ba4718e](https://github.com/this-is-tobi/ohmlab/commit/ba4718e196f9c022857749ed8419212b3d3d3d8e))
* **deps:** update helm release traefik to v42.4.0 ([4dffb9c](https://github.com/this-is-tobi/ohmlab/commit/4dffb9c0bae01900c92f69a596bfc555af38b5e3))
* **storage:** close the final review's findings before rollout ([27c8bf7](https://github.com/this-is-tobi/ohmlab/commit/27c8bf753c17f0c0b444193d679d76dfa15e9c90))

## [0.20.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.19.3...catalog-v0.20.0) (2026-11-12)


### Features

* **api:** the S3Bucket kind ([b9fa668](https://github.com/this-is-tobi/ohmlab/commit/b9fa6683c25a6a85731716fe7a15c1f455a34ff9))
* **ohmlab-controller:** package the storage target ([180656e](https://github.com/this-is-tobi/ohmlab/commit/180656e701c13f69b16774a52fc119cb9036788c))


### Bug Fixes

* **controller:** keep a rotated key's grace when Vault fails between two writes ([ce788c0](https://github.com/this-is-tobi/ohmlab/commit/ce788c0ed943808c39b87867b95cbed8b4925d9f))
* **deps:** update dependency hashicorp/vault to v2.2.2 ([dfecb50](https://github.com/this-is-tobi/ohmlab/commit/dfecb50c9b5cf7b0f1c0a008266e3737702f9e8b))
* **deps:** update valkey docker tag to v0.26.0 ([a8d7084](https://github.com/this-is-tobi/ohmlab/commit/a8d7084a22729c0c1913f39ff67bbc4189e1bb55))
* **ohmlab-controller:** give the storage target the memory its key derivations need ([3755aa5](https://github.com/this-is-tobi/ohmlab/commit/3755aa53fa016947b8036e2972c733f6864d6277))

## [0.19.3](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.19.2...catalog-v0.19.3) (2026-11-11)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.18.0 ([12c9d28](https://github.com/this-is-tobi/ohmlab/commit/12c9d2800bd43144b7cdafce51f95570a6de01a0))
* **deps:** update helm release kyverno to v3.10.0 ([76ae084](https://github.com/this-is-tobi/ohmlab/commit/76ae084e08802a8a54f3ac53c446359ffd7e3762))
* **deps:** update helm release loki to v21.2.1 ([e4dd3ce](https://github.com/this-is-tobi/ohmlab/commit/e4dd3ce33f9a0a91dc8c97372106504bffb245f2))
* **deps:** update helm release plugin-barman-cloud to v0.9.0 ([241947a](https://github.com/this-is-tobi/ohmlab/commit/241947a9e8643015c4cd73497aa9531a59fa4b9c))

## [0.19.2](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.19.1...catalog-v0.19.2) (2026-11-11)


### Bug Fixes

* **deps:** update backup-utils docker tag to v2.5.9 ([64217ec](https://github.com/this-is-tobi/ohmlab/commit/64217ec06f2777c11428cf427865ec4c642d5c22))
* **deps:** update cnpg postgres images ([3e33584](https://github.com/this-is-tobi/ohmlab/commit/3e33584cbc69c0dd6c63c614829c1b2999cdf44d))
* **deps:** update helm release apisix to v2.18.3 ([14db171](https://github.com/this-is-tobi/ohmlab/commit/14db1719e21e209e9bf8219a45de823baffefae5))
* **deps:** update helm release argo-cd to v10.17.1 ([a00a438](https://github.com/this-is-tobi/ohmlab/commit/a00a438e853cb21dd74bfea2c56f3ba8893083cd))
* **deps:** update helm release argo-cd to v10.17.2 ([ad8da1f](https://github.com/this-is-tobi/ohmlab/commit/ad8da1fcb52b7acbf7572839a7da601a92731573))
* **deps:** update helm release coder to v2.39.1 ([da5deb5](https://github.com/this-is-tobi/ohmlab/commit/da5deb59076f35d883e654693b836107b2726b93))
* **deps:** update helm release oauth2-proxy to v10.7.3 ([6d396c9](https://github.com/this-is-tobi/ohmlab/commit/6d396c91981e629d8058b78d8e84fc476ea0558f))
* **deps:** update helm release oauth2-proxy to v10.7.4 ([c4dac56](https://github.com/this-is-tobi/ohmlab/commit/c4dac567c2186815196246b8ef54e0919574f8d2))
* **deps:** update helm release rustfs to v1.0.5 ([0b24889](https://github.com/this-is-tobi/ohmlab/commit/0b24889344f0f2e7cde788a6945070285fcd2e14))
* **deps:** update helm release sops-secrets-operator to v0.29.2 ([15fc44d](https://github.com/this-is-tobi/ohmlab/commit/15fc44debffb720cba5e44c77f054b6f12d4bc36))
* **deps:** update helm release sops-secrets-operator to v0.29.3 ([25b3e40](https://github.com/this-is-tobi/ohmlab/commit/25b3e40d0756f01e1d5d04e11ab71d9e95f27991))
* **deps:** update helm release teleport-cluster to v18.11.10 ([43b9f04](https://github.com/this-is-tobi/ohmlab/commit/43b9f04115109ffb1e9c0d491e32b03975be3a14))
* **deps:** update helm release traefik to v42.3.2 ([e689a79](https://github.com/this-is-tobi/ohmlab/commit/e689a79a59df09812f08d802fab8db3d809adc27))

## [0.19.1](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.19.0...catalog-v0.19.1) (2026-11-09)


### Bug Fixes

* **deps:** update ghcr.io/this-is-tobi/ohmlab/controller docker tag to v0.8.0 ([2e69768](https://github.com/this-is-tobi/ohmlab/commit/2e697686fd0d1a7b199e72f6c92e8a67d1432484))

## [0.19.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.18.0...catalog-v0.19.0) (2026-11-08)


### Features

* **argo-cd:** serve the tenant Argo CD's Redis over TLS by default ([df0bfb6](https://github.com/this-is-tobi/ohmlab/commit/df0bfb64e8bac7bca28b2b4c66a5162e0896eb74))
* **argo-cd:** serve the tenant Argo CD's Redis over TLS, as an option ([4d136b5](https://github.com/this-is-tobi/ohmlab/commit/4d136b5dc5bd11e075182483a8b6820e67bf683b))
* **cilium,longhorn:** serve the oauth2-proxies over TLS from the internal CA ([245490f](https://github.com/this-is-tobi/ohmlab/commit/245490ff9f0de7dc35cd7e7b880b977d7ede97d9))
* **gitea:** serve Gitea over TLS from the internal CA ([6f69d34](https://github.com/this-is-tobi/ohmlab/commit/6f69d34e361a2101804d293d9ee2b9a27e1e96b3))
* **keycloak:** serve Keycloak over TLS from the internal CA, as an option ([f7fc0ba](https://github.com/this-is-tobi/ohmlab/commit/f7fc0ba1b2ab8d096709510be4d4f9da82e54fa8))
* **mattermost:** cap the ephemeral storage of its emptyDir volumes ([7431cb4](https://github.com/this-is-tobi/ohmlab/commit/7431cb4596ff68ca38a4064884a1de71b15631ba))
* **ohmlab-controller:** keep the certificates target's patches to its annotation ([5bc1308](https://github.com/this-is-tobi/ohmlab/commit/5bc130860a27b5870846fbac3b9965471562a69d))
* **ohmlab-controller:** package the certificates target ([c9fb2c3](https://github.com/this-is-tobi/ohmlab/commit/c9fb2c31b2b72bdb8e398199eb16a8d06b213425))
* **prometheus-stack:** serve Grafana over TLS from the internal CA ([192442e](https://github.com/this-is-tobi/ohmlab/commit/192442e15480d787f7fe45ad1773272a9d2b426a))
* **rustfs:** serve RustFS over TLS from the internal CA ([1d56e69](https://github.com/this-is-tobi/ohmlab/commit/1d56e6918159c17004624c28c4588a89008e91ad))
* **trust-manager:** publish each CA only where the instance lists it ([c594d4e](https://github.com/this-is-tobi/ohmlab/commit/c594d4ee9c0b3ba2e082d5346cad7a53a84b0956))


### Bug Fixes

* **argo-cd:** mount no service account token in the tenant Redis pod ([0648794](https://github.com/this-is-tobi/ohmlab/commit/0648794d12aa6505835193516f000f27c7178024))
* **argo-cd:** run the Redis exporter its ServiceMonitor scrapes ([27226a6](https://github.com/this-is-tobi/ohmlab/commit/27226a6bc6efcecd93bb6bace8e8a86a8abb622f))
* **argo-cd:** turn off the Redis exporter's /scrape endpoint ([b07679e](https://github.com/this-is-tobi/ohmlab/commit/b07679eb97113b7c493048626c02d49e6bca006e))
* **deps:** update ghcr.io/this-is-tobi/ohmlab/controller docker tag to v0.7.0 ([1a00362](https://github.com/this-is-tobi/ohmlab/commit/1a00362df7e2a50246276427a423eee871e1c317))
* **deps:** update helm release argo-cd to v10.17.0 ([698aa04](https://github.com/this-is-tobi/ohmlab/commit/698aa0441fc890f1c963acb75b74614eaab65e77))
* **keycloak:** accept any https URL for keycloak-config-cli with TLS on ([4588306](https://github.com/this-is-tobi/ohmlab/commit/4588306accf45ad9d98a059506481e291ac09714))
* **keycloak:** add the gateway's policy with the route that uses it ([3ab8e93](https://github.com/this-is-tobi/ohmlab/commit/3ab8e93e1357676cda7b1becb12bd4b32e9bdf00))
* **rustfs:** cover both RustFS ports with one policy targetRef ([8b80f0c](https://github.com/this-is-tobi/ohmlab/commit/8b80f0c1f64a551c9fb0f3ed73047112c3b19745))

## [0.18.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.17.0...catalog-v0.18.0) (2026-11-08)


### Features

* **deps:** update kube-prometheus-stack docker tag to v93 ([eb63cca](https://github.com/this-is-tobi/ohmlab/commit/eb63cca1ce49b0192302ac8616480e3b4dbe062b))


### Bug Fixes

* **deps:** update ghcr.io/this-is-tobi/ohmlab/controller docker tag to v0.6.0 ([720ad46](https://github.com/this-is-tobi/ohmlab/commit/720ad468e3bc9c5f68d02cba6eb105ca4ad31599))
* **deps:** update helm release argo-cd to v10.15.0 ([61dddbf](https://github.com/this-is-tobi/ohmlab/commit/61dddbf8ad1792ed10e0ae184f8336f578fce1da))
* **keycloak:** drop the vault-core callback no flow sends ([45ce904](https://github.com/this-is-tobi/ohmlab/commit/45ce904a4e1ac71ae1a09ce15afaf6b5c47a611f))


### Reverts

* **prometheus-stack:** go back to kube-prometheus-stack 92.22.0 ([ac27521](https://github.com/this-is-tobi/ohmlab/commit/ac2752133ab1fe442b6b51048192d93b39e8e03c))

## [0.17.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.16.0...catalog-v0.17.0) (2026-11-07)


### Features

* **gitea:** issue Valkey's certificate from a chosen issuer ([1839164](https://github.com/this-is-tobi/ohmlab/commit/1839164e2a310c895a5d75da2d21f3556149f17f))
* **gitea:** verify Valkey's certificate ([7eb5bcd](https://github.com/this-is-tobi/ohmlab/commit/7eb5bcd292df5ccb1dbeb509ab064435ce80209b))


### Bug Fixes

* **deps:** update helm release teleport-cluster to v18.11.9 ([8f47eb7](https://github.com/this-is-tobi/ohmlab/commit/8f47eb7adf5fc56a9776f68d65016ca9a09b3c49))
* **deps:** update kube-prometheus-stack docker tag to v92.22.0 ([2dffa97](https://github.com/this-is-tobi/ohmlab/commit/2dffa974a6c157e34f238e27e1d6fe1f7e041d19))
* **gitea:** keep the namespace CA's key when it renews ([7e4dd0a](https://github.com/this-is-tobi/ohmlab/commit/7e4dd0a62be42a1b18d6baf002c9fc0a72920391))
* **gitea:** reload Valkey's certificate when it renews ([da20eca](https://github.com/this-is-tobi/ohmlab/commit/da20ecae4fba63708952c57ac1e273dbe7be712a))
* **keycloak:** allow the callback Gitea signs in with ([8d41e42](https://github.com/this-is-tobi/ohmlab/commit/8d41e4208761960811ae8c8cf666a7828f85ee87))

## [0.16.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.15.0...catalog-v0.16.0) (2026-11-06)


### Features

* **instance-manager:** name the hub to an instance's apps ([05bfe8b](https://github.com/this-is-tobi/ohmlab/commit/05bfe8bb6b007ceb07ba3ff8e1f01e615d764ffb))
* **keycloak:** a remote instance's Keycloak leaves out the system realm ([5527672](https://github.com/this-is-tobi/ohmlab/commit/552767286ece32aba0e21cf96c3cf35d84918078))


### Bug Fixes

* **deps:** update dependency cnescatlab/sonar-cnes-report to v5.0.5 ([70f2943](https://github.com/this-is-tobi/ohmlab/commit/70f29438cda47d3a8fc2efd3406a71ee91f7aa72))
* **deps:** update ghcr.io/this-is-tobi/ohmlab/controller docker tag to v0.5.1 ([57d2501](https://github.com/this-is-tobi/ohmlab/commit/57d25012378b552222150d41e3383ce1bf36b662))
* **deps:** update helm release argo-workflows to v2.0.18 ([db87760](https://github.com/this-is-tobi/ohmlab/commit/db87760f28b79aa80a93f7da4426cf417b067462))
* **deps:** update helm release traefik to v42.3.1 ([9f6f7d5](https://github.com/this-is-tobi/ohmlab/commit/9f6f7d5ba99d749c36d59dc41ab584ddd247cc83))
* **deps:** update kube-prometheus-stack docker tag to v92.21.2 ([c1f2e66](https://github.com/this-is-tobi/ohmlab/commit/c1f2e66bb03469424efd1d0cc18ff3f2bbda5732))
* **deps:** update vso-utils docker tag to v2.4.0 ([18cec5b](https://github.com/this-is-tobi/ohmlab/commit/18cec5ba55d13a8780ce64c159ba5098f0f9be8a))
* **traefik:** set the Service type where the chart reads it ([f388e2b](https://github.com/this-is-tobi/ohmlab/commit/f388e2bd8f086e2e7dfb94bc759721bbea534538))

## [0.15.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.14.0...catalog-v0.15.0) (2026-11-06)


### Features

* **kyverno:** let a built-in exception cover more namespaces ([680e86c](https://github.com/this-is-tobi/ohmlab/commit/680e86c291440c3e3bf64995f4a2bf0eba69c039))


### Bug Fixes

* **deps:** update helm release argo-cd to v10.14.0 ([c0fdb49](https://github.com/this-is-tobi/ohmlab/commit/c0fdb491937032c28731cb52fe0aa13401b3829c))
* **deps:** update helm release crowdsec to v0.24.3 ([a0ed912](https://github.com/this-is-tobi/ohmlab/commit/a0ed9120fb3cb09d3a7a834031d92b9198ded5a0))
* **deps:** update helm release mattermost-team-edition to v6.6.111 ([9b185ed](https://github.com/this-is-tobi/ohmlab/commit/9b185ed36be2acbf538975e35924ddbbf9b1c697))
* **deps:** update helm release oauth2-proxy to v10.7.2 ([b47d6c8](https://github.com/this-is-tobi/ohmlab/commit/b47d6c8a6761a66751232502d12301cc632a80e9))
* **deps:** update kube-prometheus-stack docker tag to v92.21.1 ([958be8a](https://github.com/this-is-tobi/ohmlab/commit/958be8a6282b6a41823f091a5e41e22724bd7421))
* **prometheus-stack:** render Grafana's database only with Grafana ([a956e39](https://github.com/this-is-tobi/ohmlab/commit/a956e39474e66e524b61e393dba705c7d53d9059))
* **vault-operator:** a spoke instance's seeds keep its mount in their names ([0cfe3fd](https://github.com/this-is-tobi/ohmlab/commit/0cfe3fd88a481dff386e157f48d6a1955a47acd7))

## [0.14.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.13.1...catalog-v0.14.0) (2026-11-05)


### ⚠ BREAKING CHANGES

* **vault-operator:** spoke clusters declare their instances

### Features

* **charts:** the VaultAuth of every app follows its instance ([99cc10f](https://github.com/this-is-tobi/ohmlab/commit/99cc10f35557f7b45e737385c462c9e8c8c08afe))
* **deps:** update helm release loki to v21 ([27ba99e](https://github.com/this-is-tobi/ohmlab/commit/27ba99e25050a9e01501ccdc8dc23326997e7040))
* **instance-manager:** an instance that reads another instance's Vault ([24e6b0c](https://github.com/this-is-tobi/ohmlab/commit/24e6b0caa5e1d484ea4ccf240d276d543595a84c))
* **keycloak:** import only the realms the values keep ([85f7cab](https://github.com/this-is-tobi/ohmlab/commit/85f7cabb3e35fd3043e0ea46418a9036e4c538b8))
* **vault-operator:** admit only some source ranges on Vault's routes ([a7e0037](https://github.com/this-is-tobi/ohmlab/commit/a7e003720f623a3e307b8d73cb631c396a2e304b))
* **vault-operator:** run VSO alone on a cluster that reads a remote Vault ([9dc8fb7](https://github.com/this-is-tobi/ohmlab/commit/9dc8fb7591f0e6d44d1f5164aee432064f865307))
* **vault-operator:** spoke clusters declare their instances ([7ccb4df](https://github.com/this-is-tobi/ohmlab/commit/7ccb4df80236b0093b779db2b658e8bbca11c405))


### Bug Fixes

* **deps:** update ghcr.io/this-is-tobi/ohmlab/controller docker tag to v0.5.0 ([a3986a7](https://github.com/this-is-tobi/ohmlab/commit/a3986a78bc9a971d1c3e7cc9c1e0d238be3f4ea2))
* **deps:** update vso-utils docker tag to v2.3.0 ([e6645c7](https://github.com/this-is-tobi/ohmlab/commit/e6645c739b92c0b5a8cd4325c40c13ad76b2eb1f))
* **deps:** update vso-utils docker tag to v2.3.1 ([9d9a725](https://github.com/this-is-tobi/ohmlab/commit/9d9a725365cbeefa9b5f404a64f954cdfd5af05c))
* **gitea:** refuse plaintext Postgres clients ([e94d9c9](https://github.com/this-is-tobi/ohmlab/commit/e94d9c9d23b626f8f0a7e471825df046d238c6c9))
* **keycloak:** refuse plaintext Postgres clients ([5dee390](https://github.com/this-is-tobi/ohmlab/commit/5dee390541a7bd9aa5519cdf16a837e785d0dc0c))
* **vault-operator:** each Argo CD reads only its own tier's path ([f1ab1a0](https://github.com/this-is-tobi/ohmlab/commit/f1ab1a05ae239184afb33f52b88ba6e1a2dd44e2))
* **vaultwarden:** refuse plaintext Postgres clients ([4b3502a](https://github.com/this-is-tobi/ohmlab/commit/4b3502acd856f1d0f23a634ddb46eb1ed0c1b00c))

## [0.13.1](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.13.0...catalog-v0.13.1) (2026-11-04)


### Bug Fixes

* **deps:** update helm release argo-cd to v10.12.1 ([4ed6117](https://github.com/this-is-tobi/ohmlab/commit/4ed6117a3645325d6b457dc56a260fb5d018c5e3))
* **deps:** update helm release sops-secrets-operator to v0.29.1 ([c85de85](https://github.com/this-is-tobi/ohmlab/commit/c85de856ebd95b87f53af61b80e50682e64f3a43))
* **deps:** update kube-prometheus-stack docker tag to v92.18.0 ([a901378](https://github.com/this-is-tobi/ohmlab/commit/a901378f03eb32c544bc47635864e70d678cbdbc))

## [0.13.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.12.0...catalog-v0.13.0) (2026-11-04)


### Features

* **cert-manager:** opt-in internal CA for in-cluster TLS ([6d16f85](https://github.com/this-is-tobi/ohmlab/commit/6d16f854ed1da9b70c36ecc8db24999302aca4b6))
* **trust-manager:** opt-in bundle for the internal CA ([5e4d9a4](https://github.com/this-is-tobi/ohmlab/commit/5e4d9a42be751d059aaaf1e34104c6888e0a4b1d))


### Bug Fixes

* **catalog:** refuse plaintext Postgres clients in the apps without a live cluster ([2851d14](https://github.com/this-is-tobi/ohmlab/commit/2851d14b1ff6c4e61212ea9bf4be55bbb54ea1b6))
* **crowdsec:** refuse plaintext Postgres clients ([cb56ad5](https://github.com/this-is-tobi/ohmlab/commit/cb56ad51c614f09f872c0ab51c8ff48a281ea56a))
* **deps:** update backup-utils docker tag to v2.5.8 ([e18f790](https://github.com/this-is-tobi/ohmlab/commit/e18f790a32d7dbc5ee7d1b2c7f160ad88b2ddbd2))
* **deps:** update ghcr.io/cloudnative-pg/postgresql docker tag to v18.6-202611030838-standard-trixie ([68f4dc4](https://github.com/this-is-tobi/ohmlab/commit/68f4dc4099d095014c49081937461bd50bc0d3f3))
* **deps:** update helm release argo-cd to v10.11.7 ([c9a2825](https://github.com/this-is-tobi/ohmlab/commit/c9a28259df1ea6be0812ae8a607cd119c76e5bd2))
* **deps:** update helm release argo-cd to v10.12.0 ([5ff210b](https://github.com/this-is-tobi/ohmlab/commit/5ff210b4137d7a8bdf082cf82a4e910e82238917))
* **deps:** update helm release argo-workflows to v2.0.16 ([b92ca71](https://github.com/this-is-tobi/ohmlab/commit/b92ca710292e2f36befa195b2ab36a72fb93e1ec))
* **deps:** update helm release coder to v2.39.0 ([d203821](https://github.com/this-is-tobi/ohmlab/commit/d2038215071d30e309261b7243dd2828927a5e9a))
* **deps:** update helm release loki to v20.4.1 ([6a55740](https://github.com/this-is-tobi/ohmlab/commit/6a55740d292b1f7f8b80ade91fd096f497f73126))
* **deps:** update helm release sops-secrets-operator to v0.29.0 ([698559c](https://github.com/this-is-tobi/ohmlab/commit/698559c6c6340c7fc304f3d841c676a81dbf2d2b))
* **deps:** update helm release traefik to v42.3.0 ([0a5ce1c](https://github.com/this-is-tobi/ohmlab/commit/0a5ce1cecfee0c7efcff85be598ec74b8f3807af))
* **deps:** update helm release vaultwarden to v0.46.4 ([01bef37](https://github.com/this-is-tobi/ohmlab/commit/01bef37c84adf4416f7307b6ec9f60c3aecd460d))
* **deps:** update homepage docker tag to v0.2.1 ([0e44f60](https://github.com/this-is-tobi/ohmlab/commit/0e44f608878cf140f40e41bc222d902b60b5c6ac))
* **deps:** update kube-prometheus-stack docker tag to v92.17.1 ([9d74b8e](https://github.com/this-is-tobi/ohmlab/commit/9d74b8e3174c899131c20c9f1e9a5d151d000ac0))
* **deps:** update system-upgrade-controller docker tag to v0.1.1 ([09124d9](https://github.com/this-is-tobi/ohmlab/commit/09124d932a97240281e93d2c86492ca011291cd0))
* **deps:** update vso-utils docker tag to v2.2.1 ([c25e152](https://github.com/this-is-tobi/ohmlab/commit/c25e152bd42436258315afb828c423fbcd9d38c9))
* **mattermost:** refuse plaintext Postgres clients ([80c66fb](https://github.com/this-is-tobi/ohmlab/commit/80c66fb38843ed0d9f6c8a5c227985471563034d))
* **prometheus-stack:** refuse plaintext Postgres clients ([3951873](https://github.com/this-is-tobi/ohmlab/commit/39518732a34e61317f75914d23c28138681338b6))
* **trust-manager:** read Secrets only from its own namespace ([4319f2f](https://github.com/this-is-tobi/ohmlab/commit/4319f2f980212acd4c07dedcd7bb5e379f145be4))

## [0.12.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.11.0...catalog-v0.12.0) (2026-11-02)


### ⚠ BREAKING CHANGES

* **ohmlab:** the chart is no longer at utils/helm. An instance whose core.yaml gives the ohmlab app chartPath: utils/helm, or reads the chart from a catalog bundle by that path, now uses charts/ohmlab.

### Bug Fixes

* **deps:** update ghcr.io/cloudnative-pg/postgresql docker tag to v17.11-202611020810-standard-trixie ([95853fc](https://github.com/this-is-tobi/ohmlab/commit/95853fc99d00e013d010f10b7e582c2065cf2a0c))
* **kyverno:** bind the pod spec of the trivy exception without cel.bind ([d6e1759](https://github.com/this-is-tobi/ohmlab/commit/d6e1759c8f779f41aaa2673dd1eb94f55d4b9603))


### Code Refactoring

* **ohmlab:** move the bootstrap chart to charts/ohmlab ([c6c6630](https://github.com/this-is-tobi/ohmlab/commit/c6c663071ea73c727daca26de35f00b8fb99dd42))

## [0.11.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.10.0...catalog-v0.11.0) (2026-11-02)


### ⚠ BREAKING CHANGES

* an instance that lists ingress-nginx must drop the entry (moving to traefik or apisix if it is enabled) before taking this catalog revision: the entry would point at a chart that no longer exists, which validate-instance.yml refuses. An app whose Ingress relied on the nginx class default now gets traefik's.

### Features

* drop ingress-nginx from the catalog ([d3a3a76](https://github.com/this-is-tobi/ohmlab/commit/d3a3a768109dac6447fc34c8258c515d078c3cb4))
* **trivy-operator:** run infra assessment, its node-collector held to what it needs ([19aad49](https://github.com/this-is-tobi/ohmlab/commit/19aad49782a650c00906ec678e3e1f32c62ed82e))


### Bug Fixes

* **actions-runner-controller:** run the controller, listeners, runners and job pods non-root ([565291b](https://github.com/this-is-tobi/ohmlab/commit/565291b909438483a1f9648351f876e03ad0a2c5))
* **apisix:** run the gateway and the ingress controller with its ADC sidecar as non-root users ([d04d33d](https://github.com/this-is-tobi/ohmlab/commit/d04d33d2952444cb287a31146ac2c654efad8375))
* **deps:** update ghcr.io/actions/actions-runner docker tag to v2.338.2 ([ccb46a7](https://github.com/this-is-tobi/ohmlab/commit/ccb46a7d602bf02b3a90d141883ef1fdf167ff2a))
* **deps:** update ghcr.io/this-is-tobi/ohmlab/controller docker tag to v0.4.0 ([3142b3d](https://github.com/this-is-tobi/ohmlab/commit/3142b3df1b2c60abaa2bef366203f7be705d1321))
* **deps:** update helm release homepage to v0.2.0 ([beb83f2](https://github.com/this-is-tobi/ohmlab/commit/beb83f206ec707454c98811e3ddb8d5fa2be6eaa))
* **deps:** update helm release loki to v20.2.1 ([6f904ac](https://github.com/this-is-tobi/ohmlab/commit/6f904acb775f581259cc88f842c47e0f88f71778))
* **deps:** update helm release traefik to v42.2.0 ([db6b89c](https://github.com/this-is-tobi/ohmlab/commit/db6b89c24a405ce18a22ef863f402e5825ff09e5))
* **deps:** update helm release trivy-operator to v0.38.1 ([e147018](https://github.com/this-is-tobi/ohmlab/commit/e147018ba64ddee5e3d02274bef5342868ff5e8d))
* **deps:** update keycloak docker tag to v0.22.0 ([12b6e28](https://github.com/this-is-tobi/ohmlab/commit/12b6e284ed9b848574aad84dfe4f9303e302fa50))
* **deps:** update kube-prometheus-stack docker tag to v92.17.0 ([8942910](https://github.com/this-is-tobi/ohmlab/commit/8942910f39a9a49509b8bfca0d2b4645c135ad26))
* give argo-workflows, harbor, the mlflow proxy and sonarqube's init containers limits ([c77c687](https://github.com/this-is-tobi/ohmlab/commit/c77c687b2e2ab629f8edad47cd99463c057cbf2b))
* give argo-workflows, mlflow, outline and coder the restricted pod profile ([2e2e0db](https://github.com/this-is-tobi/ohmlab/commit/2e2e0db190d8627efa3084a2ff70e034b545ceef))
* give argocd-redis, the Prometheus admission Jobs, vault and the apisix controller limits ([e8f6046](https://github.com/this-is-tobi/ohmlab/commit/e8f6046554ca839feae8bd5d127f9e4b0e18a59b))
* **mlflow:** give oauth2-proxy its credentials from Vault through existingSecret ([3529a08](https://github.com/this-is-tobi/ohmlab/commit/3529a081461b485961f56beb247ae268037c98c6))
* **rustfs:** run RustFS under the container runtime's default seccomp profile ([3531516](https://github.com/this-is-tobi/ohmlab/commit/35315169fb2bc94571a7fe109f232bb5646f04d0))
* say lanOnly.sourceRange is required when lanOnly itself is null ([e8891c6](https://github.com/this-is-tobi/ohmlab/commit/e8891c696fc9b4faf0a3a2837d940faa39401620))
* **sonarqube:** drop the privileged sysctl init container, run the webapp download non-root ([e4b7d2d](https://github.com/this-is-tobi/ohmlab/commit/e4b7d2d792d936d21648c0f65a0210d568a15797))
* **teleport:** run the auth server, proxies and config-check Jobs as the image's non-root user ([1b171dd](https://github.com/this-is-tobi/ohmlab/commit/1b171dd9ec0caa3e671181de66c41ee133ac86ab))
* **trivy-operator:** run the operator and its scans non-root, against one shared database ([fe036d9](https://github.com/this-is-tobi/ohmlab/commit/fe036d9210cf87a45f049fe103c577d7e49bdd41))
* **vault-operator:** scrape the bank-vaults operator and the Vault Secrets Operator ([fd06a5f](https://github.com/this-is-tobi/ohmlab/commit/fd06a5f1f312dac7437c99a1ded1bfc9b0310059))
* **vaultwarden:** give the chart its values, a Postgres cluster and the restricted pod profile ([e982423](https://github.com/this-is-tobi/ohmlab/commit/e982423640c6dc73d9339b9c3dd1065663204434))

## [0.10.0](https://github.com/this-is-tobi/ohmlab/compare/catalog-v0.9.1...catalog-v0.10.0) (2026-11-01)


### ⚠ BREAKING CHANGES

* an instance must set lanOnly.sourceRange (traefik, and longhorn and cilium when their gateway is on) and cilium.k8sServiceHost. The bouncer's clientTrustedIPs and CrowdSec's whitelist no longer trust 192.168.1.0/24: an instance that relied on it sets its own ranges. The tenant argo-cd chart no longer opens the ApplicationSet webhook port; use networkPolicy.extraIngress.
* **instance-manager:** instance.env is now a segment of every secret's path and must be a lowercase DNS label; another value fails the manager's render.
* **keycloak:** the realm import moves from keycloakConfigCli.config.homelab.inline to realms.defaults, realms.system and realms.instance; an instance that overrides the old keys moves its overrides there.
* **vault-operator:** an instance that keeps its core secrets in its own KV mount must set systemMount to that mount's name in its vault-operator values before upgrading, or its core apps lose access to their secrets.
* a chart rendered without the instance-manager now falls back to the KV mount ohmlab, the realm ohmlab shown as Ohmlab and the Gateway ohmlab-gateway, and the Traefik Gateway follows names.gateway instead of names.traefikGateway.
* **instance-manager:** an instance that ran on the fixed names now gets names derived from its folder name. Before upgrading, pin in instance.yaml the ones that differ: names.vaultMount: homelab, names.keycloakRealm: <the realm stored in Vault>, names.keycloakRealmDisplayName: Homelab, names.gateway: homelab-gateway-traefik (Traefik) or homelab-gateway (APISIX, Cilium). names.traefikGateway is gone, and a folder name that is not a lowercase DNS label of at most 55 characters, or that is master, sys, auth, identity or cubbyhole, fails the manager render.

### Features

* **coder:** deliver coder's database and SSO secrets from Vault ([8893aed](https://github.com/this-is-tobi/ohmlab/commit/8893aeddcfc6451d338f51c21d2784c7b9fe971f))
* default every chart's Vault mount and path to the manager's ([401bd18](https://github.com/this-is-tobi/ohmlab/commit/401bd18fae8227acc5a0eb9d00f4a3e9c14c983f))
* fall back to the ohmlab names, with one Gateway name for every implementation ([22107ee](https://github.com/this-is-tobi/ohmlab/commit/22107eeba71535008e41673c0045041e3e9cc4d7))
* **instance-manager:** derive the instance's names from its name ([f5a0bd0](https://github.com/this-is-tobi/ohmlab/commit/f5a0bd074b3237f6fc737aa120dc35f46f2e9861))
* **instance-manager:** give each app its Vault mount and path prefix ([c3f2783](https://github.com/this-is-tobi/ohmlab/commit/c3f2783e20d9e078ea62964614d735b5358c4c49))
* **instance-manager:** pass the instance's names to every app as Helm parameters ([eb97308](https://github.com/this-is-tobi/ohmlab/commit/eb973089f9e25046eaf72945ba8cf8322a82d9fb))
* **keycloak:** import the ohmlab system realm next to the instance's ([c00f057](https://github.com/this-is-tobi/ohmlab/commit/c00f0573a0bd34938862e2409e63d60bfe7a63b1))
* **keycloak:** keep only the tenant apps' clients in the instance's realm ([b79ac13](https://github.com/this-is-tobi/ohmlab/commit/b79ac13fe74a460821081d3055302203c42ea872))
* **keycloak:** take the realm display name from the instance, with Homelab as default ([1197e69](https://github.com/this-is-tobi/ohmlab/commit/1197e691f5acdd150a370609b245173c4994cdde))
* no site-specific defaults in the charts ([dc6d37b](https://github.com/this-is-tobi/ohmlab/commit/dc6d37b03e5b78dadeb54abc4631fb86ca85918a))
* take the gateway names from the instance, with today's names as defaults ([a8f949c](https://github.com/this-is-tobi/ohmlab/commit/a8f949c25ef70a9df5329cde7647106a82be91ec))
* **vault-operator:** keep the core tier's secrets in the system mount ([e1b9d32](https://github.com/this-is-tobi/ohmlab/commit/e1b9d32a6abe332fd643d897460ed5b285a1633f))
* **vault-operator:** take the KV mount from the instance, with homelab as default ([6116407](https://github.com/this-is-tobi/ohmlab/commit/61164074c08b1b36fdb4a98ded555f4be62052ae))


### Bug Fixes

* **argo-cd:** open the ApplicationSet webhook only behind a route to it ([6e7c916](https://github.com/this-is-tobi/ohmlab/commit/6e7c91628fbecec5c8f67656972ebe119cacbe2f))
* **deps:** update etcd docker tag to v0.9.0 ([2cec347](https://github.com/this-is-tobi/ohmlab/commit/2cec347105bc6fdceef1911d9ba2a4eea8312e9b))
* **deps:** update helm release alloy to v1.15.0 ([59cec9b](https://github.com/this-is-tobi/ohmlab/commit/59cec9bc65763feb449483f25e5662e01999d1cd))
* **deps:** update helm release argo-workflows to v2.0.15 ([b2f98c5](https://github.com/this-is-tobi/ohmlab/commit/b2f98c5e8f412bbe327c99d4b4abd8af5633026f))
* **deps:** update helm release backup-utils to v2.5.7 ([902a52e](https://github.com/this-is-tobi/ohmlab/commit/902a52e57643ab5391388fd3657409590bb55005))
* **deps:** update helm release coder to v2.38.5 ([94f77c0](https://github.com/this-is-tobi/ohmlab/commit/94f77c0fda731a6ed106a6700c9a7fa3acdfa02e))
* **deps:** update helm release teleport-cluster to v18.11.8 ([a69a136](https://github.com/this-is-tobi/ohmlab/commit/a69a1363d21fc20da4d6d9a3d6369684fa0d17dc))
* **ohmlab-controller:** run the 0.3.1 controller built by the renamed repository ([3e8e6f8](https://github.com/this-is-tobi/ohmlab/commit/3e8e6f8954de9717d8bc39afb3ff08487b3c1264))

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
