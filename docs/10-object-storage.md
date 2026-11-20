# Object storage

Apps get the object storage they need from claims. The catalog lists each app's claims, such as a bucket for its Postgres backups. An instance names its stores in `instance.yaml`, and the ohmlab controller's storage target makes each bucket, a key confined to it and the app's Vault record ([08-controller.md](08-controller.md#object-storage-s3bucket)). Backups come first: today, the Postgres clusters of the CNPG apps and Vault's snapshots.

```mermaid
flowchart LR
    inst[instance.yaml<br/>storage]
    im[instance-manager]
    claims[storage-claims<br/>S3Buckets]
    apps[apps' Applications<br/>storage values]
    ctl[ohmlab-controller<br/>storage target]
    store[(RustFS store)]
    vault[(Vault records)]

    inst --> im
    im -->|per tier| claims
    im -->|merged by name| apps
    ctl -->|watches| claims
    ctl -->|buckets, users, keys| store
    ctl -->|one record per app| vault
    apps -->|read their record| vault
    apps -->|back up to| store
```

## Stores and roles

A store is a RustFS app of the catalog, run as an entry of `core.yaml` and declared in `instance.yaml`'s `storage.stores`. Each role names the store its claims go to: `backups`, and `data` for app data. A role without a store leaves its claims out; while no role names a store, nothing of this renders.

The store runs in the same cluster as the apps it serves, on that cluster's disks. A backup there survives the loss of an app, a database or a volume, not the loss of the cluster: for that, a store can keep an encrypted copy in a cloud S3 ([cloud replica](#cloud-replica)).

The backups store is a RustFS of its own, apart from a RustFS that users reach. It is admin only: no console, no route, no SSO. Only the controller's storage target and the pods of each claim reach its S3 API.

The claims' quotas add up to at most 80 % of the store's size, so that a store never fills before its buckets reach their quotas. CI fails a render above it (`scripts/ci/check-storage-quotas.sh`), and `OhmlabStorageOvercommitted` watches it on the cluster.

## Declaring a store

The example instance carries every piece, commented or disabled ([argo-cd/instances/_example](../argo-cd/instances/_example/)):

1. An entry in `core.yaml`, after the controller: `app: rustfs-backups`, `chart: rustfs`, `releaseName: rustfs`, `namespace: rustfs-backups`.
2. Its values, `values/core/rustfs-backups.yaml`: `store.enabled: true` (below), TLS on, the console off, no route, its own Vault role and record (`rustfs-backups`), no OIDC secret, its network policy, and the labels that put its volumes in Longhorn's `trim` group, out of the daily snapshots.
3. Its root keys, seeded by the `vault-operator` values at `<ohmlab mount>/platforms/<env>/core/<app>`, key `admin` (`accessKey`, `secretKey`). The store reads them, and so does the storage target, which needs no copy in a Secret.
4. The store in `instance.yaml`:

   ```yaml
   storage:
     stores:
       rustfs-backups:
         app: rustfs-backups          # its entry in core.yaml
         namespace: rustfs-backups
         endpoint: https://rustfs-svc.rustfs-backups.svc:9000
         region: us-east-1            # default; set on the store as well
         size: 50Gi
         storageClass: ""             # empty: the app's own
     backups: rustfs-backups
   ```

   From it, the instance-manager sizes the store's volume and sets its region, gives the controller's storage target the store, and gives `vault-operator` the policy and role of that target.

   The store's Application can appear, and sync, a few seconds before its ApplicationSet carries that wiring, and a volume's class and size cannot change once created. With `store.enabled: true`, the store's chart does not render until the wiring arrives (`store.wired`), so its first sync waits for it instead of creating the volume from the chart's defaults. The instance checks render each store a second time without its wiring, as that first sync would, and fail when it still renders (`scripts/ci/render-instance.sh`).
5. The internal CA in `ohmlab-system`: trust-manager's `targetNamespaces` lists it, since the storage target verifies the store with that CA.
6. The store's namespace in Kyverno's `exceptionNamespaces`, under `rustfs-init-step-limits`, as for any RustFS outside its default namespace ([security policies](05-services.md#security-policies)).

## Claims

The catalog's registry is `storage.claims` in the [instance-manager's values](../argo-cd/apps/instance-manager/values.yaml): per app, each claim's role, quota, wiring and client pods, and whether those pods get an egress policy to the store (`networkPolicy`, below). Today:

| App | Claim | Quota | Wiring |
| --- | --- | --- | --- |
| crowdsec | `pg` | 5Gi | `cnpg` |
| mattermost | `pg` | 4Gi | `cnpg` |
| coder, gitea, harbor, keycloak, mlflow, outline, prometheus-stack, sonarqube, vaultwarden | `pg` | 2Gi | `cnpg` |
| vault-operator | `vault` | 3Gi | `backup-utils` |

`instance.yaml` overrides any field of a claim, and `enabled: false` turns it off:

```yaml
storage:
  claims:
    gitea:
      pg: {quota: 3Gi}
    sonarqube:
      pg: {enabled: false}
```

`storage.claimDefaults` holds what a claim takes unless it sets it: `access: readWrite`, versioning on, noncurrent versions kept 7 days.

For each tier, the instance-manager renders an Application `<instance>-storage-<tier>` of the [storage-claims chart](../argo-cd/apps/storage-claims/). It holds one `S3Bucket` per claim of the tier's enabled apps, in `ohmlab-system`, with `deletionPolicy: Retain`: disabling an app leaves its bucket, its data and its record. A bucket is named `<mount>-<app>-<claim>`, where the mount is the tier's: `ohmlab` for the core tier and the instance's (`names.vaultMount`) for the tenant tier. It holds 3 to 63 characters, and the render fails on a longer one: shorten the instance's name or set `names.vaultMount`. The name leaves the record's env and scope out, so two platforms on one mount and one store would want the same bucket; the older claim keeps it ([08-controller.md](08-controller.md#object-storage-s3bucket)).

An enabled claim gives its app, in the Application's inline values:

- `global.ohmlab.storage.<claim>`: `enabled`, `bucket`, `endpoint`, `region` and `pathStyle`;
- its wiring: `cnpg` turns the CNPG cluster's backups on with the store's endpoint, the bucket and the store's CA, and its monitoring, whose PodMonitor the backup alerts read (the Prometheus Operator's CRDs must exist); `backup-utils` turns the Vault snapshot job on.

The app's key is in its record, `<mount>/platforms/<env>/<scope>/<app>/s3`, one key per claim holding `accessKey`, `secretKey`, `endpoint`, `region`, `pathStyle`, `bucket` and `ca`. The storage target writes it, and the app's VaultStaticSecret reads it.

The claims chart also renders the store's NetworkPolicy, `ohmlab-storage-<tier>` in the store's namespace. It admits each claim's pods and, from the core tier, the storage target, on the S3 port: a store with no policy of its own admits no one else. With `storage.networkPolicies: true`, it renders as well the egress policies from each claim's pods to the store, for an instance whose apps deny egress by default (`networkPolicy.enabled`). On a pod that no other egress policy selects, the claim's would deny the rest of its egress, DNS and the API server included: it is off by default, and CI fails a render where a claim's egress policy would be the only one of its pods (`scripts/ci/check-storage-egress.sh`). A claim whose app keeps its namespace's egress open needs no such policy, its pods reach the store anyway: `networkPolicy: false` on the claim leaves it out. The registry sets it for prometheus-stack, whose chart never denies egress.

## Rolling out

A consumer never comes before its record: the barman-cloud plugin's pre-reconcile hook blocks a Postgres cluster whose backup Secret is empty. Three steps, each checked before the next:

1. The records. The store, its seed, `storage` in `instance.yaml` with `wireConsumers: false`, and the controller's storage target with `targets.storage.observeOnly: true` in the `ohmlab-controller` values. The target needs egress to Vault, like the vault target. Each `S3Bucket` reports `Drifted` (reason `ObserveOnly`), and `ohmlab_planned_changes` counts what a live run would do.
2. Live. `observeOnly: false`: each `S3Bucket` goes `Ready`, and its bucket, user and record exist.
3. The consumers. `wireConsumers: true`, and any `cnpg.backups` of the apps' own values removed: the claims set them now. A Postgres cluster created with its backups wired starts with the barman-cloud sidecar. One that already runs restarts each instance to add it, its primary in place: CNPG cannot switch over first, since demoting the primary archives its WAL through the sidecar it still lacks. Its database then refuses new connections while its clients keep theirs open, up to the cluster's `smartShutdownTimeout` (180 s by default), and is down until the new pod has its volume and starts: several minutes on slow disks. The base backup its ScheduledBackup takes at once (the cluster chart always sets `immediate`) fails while the instances roll, and a failed backup is not retried: the first one is the daily one, and `OhmlabPostgresBackupStale` goes pending, then fires an hour later. Take one once the cluster is healthy: `kubectl cnpg backup <cluster> -n <namespace> --method plugin --plugin-name barman-cloud.cloudnative-pg.io`.

## Backups

- **Postgres.** Each CNPG app backs up with the barman-cloud plugin: WAL continuously, and a base backup every day, five minutes apart from 20:00 UTC (crowdsec 20:00, gitea 20:05, keycloak 20:10, mattermost 20:15, prometheus-stack 20:20, outline 20:25, harbor 20:30, sonarqube 20:35, mlflow 20:40, vaultwarden 20:45, coder 20:50). They are kept 30 days, gzip-compressed. The ObjectStore syncs before the cluster. Base backups run on the primary (`cnpg.backups.target: primary`): one taken on a standby is restorable only once the primary switches its WAL segment, which a quiet database may not do for hours. An app with a large database can set `prefer-standby` to keep the base backup's reads off its primary; its newest backup then becomes restorable only at that switch.
- **Vault.** A raft snapshot every day at 22:00 UTC (CronJob `backup-vault-snapshot` in vault-operator's namespace), kept 30 days. The job logs in as the ServiceAccount `vault-backup`, whose role reads the snapshot and nothing else, with a projected token, and verifies the TLS of Vault and of the store.

A noncurrent object version expires after the claim's `noncurrentDays`. RustFS, as S3, rounds a due time up to the next midnight UTC, so a version goes between N and N+1 days.

## Alerts

With the storage target on:

| Alert | Fires when | Look at |
| --- | --- | --- |
| `OhmlabPostgresBackupStale` | a cluster's newest base backup is more than 26 hours old, for 1h | its ObjectStore's status, the `plugin-barman-cloud` sidecar of its pods |
| `OhmlabWALArchivingFailing` | a cluster's last WAL archive attempt failed after its last success, for 15m | the same |
| `OhmlabVaultSnapshotStale` | the snapshot job has not succeeded in 26 hours (counted from its creation if it never did), for 30m | the logs of its last Job |

The store's own alerts (`OhmlabStoreFilling`, `OhmlabStoreFull`, `OhmlabBucketNearQuota`, `OhmlabStorageOvercommitted`) are in [08-controller.md](08-controller.md#metrics-and-alerts).

## Restoring

Neither runbook has been drilled on a cluster yet; CI renders the Postgres one (`scripts/ci/test-cnpg-backups.sh`).

**Postgres.** A cluster restores into a new cluster of the same name, from its claim's ObjectStore: keep the claim on. The old cluster must be gone first, lost with its volumes or deleted by hand for a point-in-time restore (deleting a CNPG Cluster deletes its volumes). Then, in the app's values, point the cluster at the backups and sync:

```yaml
cnpg:
  mode: recovery
  recovery:
    method: plugin
    clusterName: <app>-pg-cluster                # the server name of the backups
    pluginConfiguration:
      name: barman-cloud.cloudnative-pg.io
      parameters:
        barmanObjectName: <app>-pg-cluster-backups   # the claim's ObjectStore
    objectStore:
      create: false                              # use that ObjectStore as it is
    secret:
      create: false                              # and its credentials
    pitrTarget:
      time: ""                                   # RFC 3339; empty: replay every WAL
  cluster:
    plugins:
    - name: barman-cloud.cloudnative-pg.io
      parameters:
        serverName: <app>-pg-cluster-2           # no archive in the bucket under it yet
```

The new cluster starts from the newest base backup before the target and replays the WAL after it. It archives under a server name of its own, so its WAL never mixes with the history it came from. Keep both in the values: the bootstrap runs only when the cluster is created, and a later restore takes the last server name as `clusterName`.

**Vault.** A snapshot opens only with the unseal keys of the Vault that took it: keep them outside the cluster, never in the store. Take the newest snapshot from the `vault` claim's bucket with its record's key, then, on that Vault with a root token, `vault operator raft snapshot restore -force <snapshot>`, and unseal with those keys.

## Cloud replica

A store can keep a copy of its buckets in a cloud S3, so that its backups outlive the cluster. Every hour but 01:40-04:40 UTC (the volume snapshots and the k3s upgrade window), a job copies each bucket whose claim has `replicate: true` (every backups claim of the catalog) into `<bucket>/<instance>/` of one cloud bucket. It goes through rclone's `crypt` remote: the provider holds encrypted contents and encrypted names only, and the passwords never leave the cluster and the [break-glass bundle](#break-glass-bundle).

```mermaid
flowchart LR
    ctl[storage target]
    rec[(the store's record<br/>key replicator)]
    job[replica job<br/>rclone + crypt]
    store[(RustFS store)]
    cloud[(cloud bucket<br/>versioned)]

    ctl -->|read-only user over<br/>the replicated buckets| store
    ctl -->|its key, the bucket list| rec
    rec --> job
    store -->|read| job
    job -->|ciphertext| cloud
```

The storage target keeps one read-only user per store, `ohmlab-replicator-<store>`, over the buckets that replicate, and writes its key and the bucket list into the store's own record ([08-controller.md](08-controller.md#the-replicator)). The job reads that key, the cloud key and the crypt passwords from Vault. A bucket that lists nothing in the store while the replica holds some of it fails the run instead of copying: a store rebuilt empty never empties its replica. A bucket missing from the store is skipped, and any failed bucket fails the run while the others still copy.

### Turning it on

1. **The cloud side**, at the provider, by the owner. Any S3 works. The same five rules apply everywhere:
   1. One bucket for the instance, with versioning on. Its name is global at most providers.
   2. A lifecycle rule that expires noncurrent versions after 30 days, and incomplete multipart uploads after a day.
   3. A key of its own for the job, which can list, read, write and delete objects, and nothing else: no deleting a version, no lifecycle, versioning or policy change. A cluster that is taken over then cannot erase its own history faster than the lifecycle does.
   4. The owner's own access kept, to manage the bucket.
   5. The key written into Vault, at `<ohmlab mount>/platforms/<env>/core/<store app>/replica`, as `accessKey` and `secretKey`.

   On Scaleway, a bucket policy allows only what it lists ("Deny statements are therefore useless", version `2023-04-17`), and the job's IAM application needs Object Storage read and write in the project as well. The bucket policy gives the owner's user everything and the application exactly eight actions:

   ```json
   {
     "Version": "2023-04-17",
     "Statement": [
       {"Sid": "owner", "Effect": "Allow", "Principal": {"SCW": "user_id:<owner user id>"},
        "Action": "*", "Resource": ["<bucket>", "<bucket>/*"]},
       {"Sid": "replica", "Effect": "Allow", "Principal": {"SCW": "application_id:<application id>"},
        "Action": ["s3:ListBucket", "s3:ListBucketMultipartUploads", "s3:GetBucketLocation",
                   "s3:GetObject", "s3:PutObject", "s3:DeleteObject",
                   "s3:AbortMultipartUpload", "s3:ListMultipartUploadParts"],
        "Resource": ["<bucket>", "<bucket>/*"]}
     ]
   }
   ```

   Check it with the application's key: deleting an object version must answer `AccessDenied`. On AWS S3 the same rules are an IAM policy for the key plus a bucket policy that denies `s3:DeleteObjectVersion`, `s3:PutLifecycleConfiguration`, `s3:PutBucketVersioning` and `s3:PutBucketPolicy` to it.
2. **The store's `replica`** in `instance.yaml`, first as a dry run:

   ```yaml
   storage:
     stores:
       rustfs-backups:
         # ...
         replica:
           endpoint: https://s3.fr-par.scw.cloud
           region: fr-par
           bucket: <bucket>
           provider: Scaleway       # rclone's S3 provider; Other by default
           storageClass: ""         # empty: the provider's default
           dryRun: true             # lists what each run would copy, copies nothing
           bwlimit: 4M              # rclone's --bwlimit, bytes per second
           extraArgs: []            # more rclone flags, for example --size-only
   ```

   From it, the instance-manager gives the storage target the store's replicator, `vault-operator` the VaultSeed that generates the two crypt passwords once (`…/<store app>/replica-crypt`), and the store's app the job. Its log lists, per bucket, what a live run would copy, which also proves the store's TLS, the cloud's TLS and the listing.
3. **Live**: remove `dryRun`. The first run copies every replicated bucket; if it takes longer than the job's 50 minutes, the next runs carry on where it stopped. Then [make the break-glass bundle](#break-glass-bundle).

The job's settings in the store's values: `replica.schedule`, `transfers`, `checkers`, `resources` and `egress` (TCP 443 to public addresses, rendered where the namespace denies egress). The job's ConfigMap, `<release>-replica`, holds its script and the settings a restore needs; it carries the label `ohmlab.fr/replica: "true"`.

| Alert | Fires when | Look at |
| --- | --- | --- |
| `OhmlabReplicaStale` | the replica job has not succeeded in 6 hours (counted from its creation if it never did), for 15m; the job runs at most 5 hours apart | the logs of its last Job: a bucket it refused to empty, a missing Secret, the cloud's answer |

### Break-glass bundle

A rebuild needs a few secrets the cluster cannot keep for itself, since they are what open its backups. They never go to a store, encrypted or not:

- each replica's crypt passwords: without them the replica cannot be read;
- each replica's cloud key;
- Vault's unseal keys and root token: a Vault snapshot opens only with the unseal keys of the Vault that took it;
- the k3s server token, on a self-hosted k3s cluster: an etcd snapshot restores only with it. k3s's `secrets-encryption` is off, so there is no encryption config to keep.

`ohmlab storage break-glass` collects them into one YAML document, for a password manager ([07-cheat-sheet.md](07-cheat-sheet.md#ohmlab-cli)): `--output` writes a new file only you can read; delete it once stored. Each replica comes with the shell lines that set its rclone remotes, `replica` (the cloud bucket) and `crypt` (its decrypted view). Make the bundle again after a change of the crypt passwords, the cloud key or Vault's unseal keys.

### Restore from the replica

Not drilled yet. On a new cluster, with the [break-glass bundle](#break-glass-bundle):

1. Suspend the replica job of the new store (`kubectl -n <store namespace> patch cronjob <release>-replica -p '{"spec":{"suspend":true}}'`) until the restore is done, so it never syncs a store that is still empty.
2. Copy each bucket back, with the bundle's rclone remotes: `rclone copy crypt:<bucket> store:<bucket>`.
3. Restore Vault, then each Postgres cluster, as in [Restoring](#restoring).
4. Resume the job.

## Not there yet

- Stores other than an in-cluster RustFS: a cloud S3 as a store.
- Claims of apps on an instance that reads another instance's Vault (`vault.hub`): the render refuses them.
- Backups of Longhorn volumes, of etcd, and of apps' data other than Postgres.
