# Changelog

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
