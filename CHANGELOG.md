# Changelog

## [0.4.2](https://github.com/skaphos/berth/compare/v0.4.1...v0.4.2) (2026-09-08)


### Bug Fixes

* **acquire:** enforce local lease deadlines and safe restart handoffs ([#186](https://github.com/skaphos/berth/issues/186)) ([ebf96af](https://github.com/skaphos/berth/commit/ebf96aff6de4582e4a074f1d22275877ef92306b))
* **acquire:** root the startup-gate holder at the cluster identity ([#190](https://github.com/skaphos/berth/issues/190)) ([01ec5d0](https://github.com/skaphos/berth/commit/01ec5d029c16c0ae4e965a3a48b216c0a8957e42))
* **api:** fail closed when authenticator returns a nil identity ([#189](https://github.com/skaphos/berth/issues/189)) ([418fde2](https://github.com/skaphos/berth/commit/418fde2116ca8a711dde6d0a5e1f693e639dff26))
* **broker:** bound each OIDC token fetch and back off on failure ([#193](https://github.com/skaphos/berth/issues/193)) ([0935235](https://github.com/skaphos/berth/commit/0935235682b6e883a7c4fd92344d90d87b5a25e6))
* **helm:** metrics port omission, honest CRD controls, and helper pull policy ([#194](https://github.com/skaphos/berth/issues/194)) ([5255772](https://github.com/skaphos/berth/commit/525577232e19d161f02ed55a909c979a14f0aa10))
* **lease:** stop metadata-only Kubernetes conflicts from ending a lease ([#196](https://github.com/skaphos/berth/issues/196)) ([7452a24](https://github.com/skaphos/berth/commit/7452a241a2c2de46c628858e5fdd104e5ecbd40f)), closes [#121](https://github.com/skaphos/berth/issues/121) [#120](https://github.com/skaphos/berth/issues/120) [#168](https://github.com/skaphos/berth/issues/168)
* **operator:** patch target workloads with a version-guarded merge patch ([#195](https://github.com/skaphos/berth/issues/195)) ([eb54ba8](https://github.com/skaphos/berth/commit/eb54ba806ce71fa823e45c9a4e167f38e2a0914f))
* **store:** validate MySQL DSN time parameters to prevent early lease expiry ([#191](https://github.com/skaphos/berth/issues/191)) ([b99a7a0](https://github.com/skaphos/berth/commit/b99a7a003b83c815eaf0e521832ea92dea5f01f2)), closes [#107](https://github.com/skaphos/berth/issues/107)
* **store:** verify SQL schema at startup when migration is off ([#192](https://github.com/skaphos/berth/issues/192)) ([42f1c6b](https://github.com/skaphos/berth/commit/42f1c6bc2133dd3fc13083e2fa603d36e0a3a327))
* **webhook:** bound lease-loss restart windows ([#188](https://github.com/skaphos/berth/issues/188)) ([52d05dc](https://github.com/skaphos/berth/commit/52d05dc46e9ee10bd33ff199fb84f786d0340286)), closes [#110](https://github.com/skaphos/berth/issues/110) [#118](https://github.com/skaphos/berth/issues/118)

## [0.4.1](https://github.com/skaphos/berth/compare/v0.4.0...v0.4.1) (2026-09-07)


### Bug Fixes

* **acquire:** require freshness liveness in runtime signal mode ([f0f5e0e](https://github.com/skaphos/berth/commit/f0f5e0e6e26122d9105f5b8d07671d2148030d17))
* **api:** enforce lease request and holder size limits ([f0f5e0e](https://github.com/skaphos/berth/commit/f0f5e0e6e26122d9105f5b8d07671d2148030d17))
* **auth:** require flat tenant identifiers ([f0f5e0e](https://github.com/skaphos/berth/commit/f0f5e0e6e26122d9105f5b8d07671d2148030d17))
* **deps:** refresh Go, Kubernetes, CI tools and container dependencies ([d54c795](https://github.com/skaphos/berth/commit/d54c795d677180da6ce95aaebd0c2b6c0747c9c6))
* **injection:** isolate lease holders by Pod UID ([f0f5e0e](https://github.com/skaphos/berth/commit/f0f5e0e6e26122d9105f5b8d07671d2148030d17))
* **injection:** reject unsupported runtime init workloads ([f0f5e0e](https://github.com/skaphos/berth/commit/f0f5e0e6e26122d9105f5b8d07671d2148030d17))
* **metrics:** bound HTTP method metric labels ([f0f5e0e](https://github.com/skaphos/berth/commit/f0f5e0e6e26122d9105f5b8d07671d2148030d17))
* **operator:** drain managed workloads before releasing deleted leases ([5ac78d8](https://github.com/skaphos/berth/commit/5ac78d85c9fed60498903506968ccfbb30687f79))
* **operator:** stop managed workloads when ownership expires ([5ac78d8](https://github.com/skaphos/berth/commit/5ac78d85c9fed60498903506968ccfbb30687f79))
* **release:** include required security-patch upgrade instructions ([9f656be](https://github.com/skaphos/berth/commit/9f656be22f33dc6f0a403a3a27bb4b4051750def))
* **release:** publish the injected helper and align chart image tags ([9f656be](https://github.com/skaphos/berth/commit/9f656be22f33dc6f0a403a3a27bb4b4051750def))
* **release:** publish the reviewed changelog as release notes ([f0f5e0e](https://github.com/skaphos/berth/commit/f0f5e0e6e26122d9105f5b8d07671d2148030d17))
* **store:** preserve released Kubernetes leases with valid tombstones ([9f656be](https://github.com/skaphos/berth/commit/9f656be22f33dc6f0a403a3a27bb4b4051750def))

## [0.4.0](https://github.com/skaphos/berth/compare/v0.3.1...v0.4.0) (2026-08-10)


### ⚠ BREAKING CHANGES

Two changes here can stop workloads from starting. Read the
[upgrade notes](docs/operations/upgrade-state-volume-trust.md) first — they
include a `kubectl`/`jq` recipe for finding affected workloads **before** you
upgrade.

* **The `berth-state` volume is reserved.** Pods whose own containers mount it
  with write access are now refused at admission — at any mount path, in either
  enforce mode, on Pod creation and on `kubectl debug` attachments. There is no
  opt-out, and a rejected Pod is refused on every admission event, including
  re-admission after eviction or rescheduling. Read-only mounts are still
  allowed, and a writable mount at exactly the state dir is repaired to
  read-only rather than refused.

  Such a Pod used to be admitted. Because the helper copies the liveness
  probe's `check` binary into that same volume, write access let a workload
  forge its own health marker *and replace the verifier*, defeating
  at-most-once enforcement ([#96](https://github.com/skaphos/berth/issues/96)).

* **`injection.webhook.failurePolicy` now defaults to `Fail`** (previously
  `Ignore`). Installations that never overrode it inherit the change on
  upgrade.

  While the webhook is unavailable, opted-in Pods will not be created: the
  operator becomes a hard dependency for Pod creation in gated namespaces.
  This is deliberate — under `Ignore` the reserved-volume rule silently lapsed
  for the duration of any webhook outage, which is indistinguishable from not
  having it. Set it back to `Ignore` only if you would rather a gated workload
  start unprotected than not start at all.

* **gating:** reserve the state volume, add marker freshness, close the injection bypass ([#140](https://github.com/skaphos/berth/issues/140))

### Bug Fixes

* **acquire:** bound lease RPCs so a hung API cannot wedge enforcement ([#138](https://github.com/skaphos/berth/issues/138)) ([6fd0a69](https://github.com/skaphos/berth/commit/6fd0a69a18da2ac1af07c342bd8a6adb35b248b3)), closes [#97](https://github.com/skaphos/berth/issues/97)
* **gating:** reserve the state volume, add marker freshness, close the injection bypass ([#140](https://github.com/skaphos/berth/issues/140)) ([fa716df](https://github.com/skaphos/berth/commit/fa716dfc45fd3a1dd35814866b2cc2dd11f12d5c))

## [0.3.1](https://github.com/skaphos/berth/compare/v0.3.0...v0.3.1) (2026-08-08)


### Bug Fixes

* **lease:** version-CAS writes, tombstoned releases, validated keys ([#130](https://github.com/skaphos/berth/issues/130)) ([18b7064](https://github.com/skaphos/berth/commit/18b7064b506d75ad7ef359e0b36c1f5e88a52a86))
* repair the e2e suite — tenant-owned holders for operators and injected helpers ([#91](https://github.com/skaphos/berth/issues/91)) ([#125](https://github.com/skaphos/berth/issues/125)) ([0312bb5](https://github.com/skaphos/berth/commit/0312bb52bcf81f9f2b702ddb3bae021b214ae5bc))

## Changelog

This file is managed by Release Please.
