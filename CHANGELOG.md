# Changelog

## [0.10.13](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.12...0.10.13) (2026-09-05)


### Bug Fixes

* **go:** update module github.com/prometheus/client_model (v0.6.2 → v0.6.3) ([#325](https://github.com/home-operations/external-dns-unifi-webhook/issues/325)) ([57743fe](https://github.com/home-operations/external-dns-unifi-webhook/commit/57743fe5158b03be6f097cac38114ff2fb0db572))


### Documentation

* **agents:** derive Go version from go.mod ([981761a](https://github.com/home-operations/external-dns-unifi-webhook/commit/981761a0cb25e18fd15c13a21958e845778b712a))


### Miscellaneous Chores

* **github-action:** update action jdx/mise-action (v4.2.5 → v4.3.0) ([#322](https://github.com/home-operations/external-dns-unifi-webhook/issues/322)) ([a41193d](https://github.com/home-operations/external-dns-unifi-webhook/commit/a41193de601c7df2392d8457728a4055ea36d675))
* **mise:** update tool go (1.27.0 → 1.27.1) ([#328](https://github.com/home-operations/external-dns-unifi-webhook/issues/328)) ([893f8a8](https://github.com/home-operations/external-dns-unifi-webhook/commit/893f8a8ac4a373f7216990dc42725097e9d75d12))
* **mise:** update tool golangci-lint (2.13.1 → 2.13.2) ([#323](https://github.com/home-operations/external-dns-unifi-webhook/issues/323)) ([dada750](https://github.com/home-operations/external-dns-unifi-webhook/commit/dada7506af636dd83a662ac68ba461ae1eb01b50))
* **mise:** update tool lefthook (2.1.11 → 2.1.12) ([#324](https://github.com/home-operations/external-dns-unifi-webhook/issues/324)) ([a54cf89](https://github.com/home-operations/external-dns-unifi-webhook/commit/a54cf89782a4d4266b29cb7f588c8029c304a883))
* **mise:** update tool oxfmt (0.64.0 → 0.65.0) ([#321](https://github.com/home-operations/external-dns-unifi-webhook/issues/321)) ([9385344](https://github.com/home-operations/external-dns-unifi-webhook/commit/9385344c126c6a48c4dfdb7388632c677efc436a))
* **mise:** update tool oxfmt (0.65.0 → 0.66.0) ([#327](https://github.com/home-operations/external-dns-unifi-webhook/issues/327)) ([fe05a81](https://github.com/home-operations/external-dns-unifi-webhook/commit/fe05a81445bb100a3df4f93c652672f17229ac64))
* **mise:** update tool zizmor (1.29.0 → 1.30.0) ([#326](https://github.com/home-operations/external-dns-unifi-webhook/issues/326)) ([a0cd687](https://github.com/home-operations/external-dns-unifi-webhook/commit/a0cd687a2e19c47b9ed2a4067d96455c462a9739))

## [0.10.12](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.11...0.10.12) (2026-08-27)


### Bug Fixes

* drop UPX compression, packed binaries segfault (upx/upx[#18902](https://github.com/home-operations/external-dns-unifi-webhook/issues/18902)) ([#319](https://github.com/home-operations/external-dns-unifi-webhook/issues/319)) ([26464ed](https://github.com/home-operations/external-dns-unifi-webhook/commit/26464ed9515a6f9c9eacb57912ff8364e33f9de4))


### Miscellaneous Chores

* **github-action:** update action docker/github-builder (v1.16.0 → v1.17.0) ([#318](https://github.com/home-operations/external-dns-unifi-webhook/issues/318)) ([1495e6a](https://github.com/home-operations/external-dns-unifi-webhook/commit/1495e6af8ded30bd5c1b740828f090e1aa2e9516))
* **mise:** update mise tools ([#317](https://github.com/home-operations/external-dns-unifi-webhook/issues/317)) ([12e06ec](https://github.com/home-operations/external-dns-unifi-webhook/commit/12e06eccbb4104ada1890dd3fb57de62c65ae427))
* **mise:** update tool oxfmt (0.63.0 → 0.64.0) ([#315](https://github.com/home-operations/external-dns-unifi-webhook/issues/315)) ([3db5214](https://github.com/home-operations/external-dns-unifi-webhook/commit/3db521499841f7f7f9e3f0eba3b3923d0896fd5e))

## [0.10.11](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.10...0.10.11) (2026-08-21)


### Bug Fixes

* **go:** update to go 1.27.0 ([#313](https://github.com/home-operations/external-dns-unifi-webhook/issues/313)) ([931e211](https://github.com/home-operations/external-dns-unifi-webhook/commit/931e2116d51dded40a65344fea63c79c7679118d))

## [0.10.10](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.9...0.10.10) (2026-08-21)


### Features

* **deps:** update module github.com/prometheus/client_golang (v1.23.2 → v1.24.0) ([#276](https://github.com/home-operations/external-dns-unifi-webhook/issues/276)) ([cc63630](https://github.com/home-operations/external-dns-unifi-webhook/commit/cc636303d24f3be85543d29e00655cdfef91ed95))
* **deps:** update module golang.org/x/sync (v0.21.0 → v0.22.0) ([#271](https://github.com/home-operations/external-dns-unifi-webhook/issues/271)) ([45f002d](https://github.com/home-operations/external-dns-unifi-webhook/commit/45f002d3f228e37b8e4944b98990b3a8c52327a0))
* **go:** update module sigs.k8s.io/external-dns (v0.21.0 → v0.22.0) ([#312](https://github.com/home-operations/external-dns-unifi-webhook/issues/312)) ([ada3935](https://github.com/home-operations/external-dns-unifi-webhook/commit/ada393510b0c6628e1055c0d395e0be28d5dcc57))


### Bug Fixes

* **ci:** fail the merge gate on cancelled jobs, and key the lint cache on the toolchain ([#293](https://github.com/home-operations/external-dns-unifi-webhook/issues/293)) ([0d0abca](https://github.com/home-operations/external-dns-unifi-webhook/commit/0d0abca6f032a4f2cb0f804844e59876f61e562f))
* **deps:** update module github.com/prometheus/client_golang (v1.24.0 → v1.24.1) ([#281](https://github.com/home-operations/external-dns-unifi-webhook/issues/281)) ([b5ca23b](https://github.com/home-operations/external-dns-unifi-webhook/commit/b5ca23b9aeec76b9f3ba37befa353c64c1ab4844))
* **go:** update module go (1.26.4 → 1.26.5) ([#302](https://github.com/home-operations/external-dns-unifi-webhook/issues/302)) ([ed85692](https://github.com/home-operations/external-dns-unifi-webhook/commit/ed856929fbdddbbec4ef93808577d85e4411c9ef))
* **go:** update module go (1.26.5 → 1.26.6) ([#307](https://github.com/home-operations/external-dns-unifi-webhook/issues/307)) ([c3a0c92](https://github.com/home-operations/external-dns-unifi-webhook/commit/c3a0c92e71f8a2fe0f5b3b6ee2b00f4ece2f2272))


### Documentation

* add AGENTS.md with Go conventions ([#296](https://github.com/home-operations/external-dns-unifi-webhook/issues/296)) ([47d1025](https://github.com/home-operations/external-dns-unifi-webhook/commit/47d1025e5c8d3f2cc2e616c79546f157e3c227a8))
* point the CI badge at ci.yaml ([#285](https://github.com/home-operations/external-dns-unifi-webhook/issues/285)) ([ac12f51](https://github.com/home-operations/external-dns-unifi-webhook/commit/ac12f51255de2623137151b201c378fc54233fd8))


### Styles

* indent markdown at 2 to match embedded yaml ([#277](https://github.com/home-operations/external-dns-unifi-webhook/issues/277)) ([0194a09](https://github.com/home-operations/external-dns-unifi-webhook/commit/0194a094a280b3109d3eb15082f391e8dab3b987))


### Build System

* **mise:** add actionlint and refresh the lockfile ([#286](https://github.com/home-operations/external-dns-unifi-webhook/issues/286)) ([a9a633a](https://github.com/home-operations/external-dns-unifi-webhook/commit/a9a633ad84449b169b65a48fd382db46be8d8f75))


### Continuous Integration

* gate pull requests on a single Build Success check ([#284](https://github.com/home-operations/external-dns-unifi-webhook/issues/284)) ([f9244d6](https://github.com/home-operations/external-dns-unifi-webhook/commit/f9244d6b2facba7e38c2f12897b5bbc9660977b3))
* **github-action:** Update action actions/checkout (v7.0.0 → v7.0.1) ([7d0f63a](https://github.com/home-operations/external-dns-unifi-webhook/commit/7d0f63a297fc704f3021dafa167761d922e7caec))
* **github-action:** Update action actions/stale (v10.3.0 → v10.4.0) ([fed963e](https://github.com/home-operations/external-dns-unifi-webhook/commit/fed963ed01d740bf029b35fee015c87f45a59047))
* **github-action:** Update action actions/stale (v10.4.0 → v11.0.0) ([#294](https://github.com/home-operations/external-dns-unifi-webhook/issues/294)) ([39338f0](https://github.com/home-operations/external-dns-unifi-webhook/commit/39338f0b41b88f300577f335e025922000050d93))
* **github-action:** Update action docker/github-builder (v1.12.0 → v1.13.0) ([7aaa300](https://github.com/home-operations/external-dns-unifi-webhook/commit/7aaa300a9fd291bf4ace5d8f6c2882787306f84e))
* **github-action:** Update action docker/github-builder (v1.13.0 → v1.14.0) ([dddd3a9](https://github.com/home-operations/external-dns-unifi-webhook/commit/dddd3a9577c1efe85aa09119a70e386dc0ca29a5))
* **github-action:** Update action docker/github-builder (v1.14.0 → v1.15.0) ([#292](https://github.com/home-operations/external-dns-unifi-webhook/issues/292)) ([fc10a57](https://github.com/home-operations/external-dns-unifi-webhook/commit/fc10a57851f8b48561fe4edafb8f1574d5b53440))
* **github-action:** Update action docker/github-builder (v1.15.0 → v1.16.0) ([#305](https://github.com/home-operations/external-dns-unifi-webhook/issues/305)) ([a05131f](https://github.com/home-operations/external-dns-unifi-webhook/commit/a05131fbcb86174368c34ede332f9530f4bbcfd9))
* **github-action:** Update action home-operations/.github/actions/workflow-lint (v1.0.2 → v1.0.3) ([#301](https://github.com/home-operations/external-dns-unifi-webhook/issues/301)) ([cd22d30](https://github.com/home-operations/external-dns-unifi-webhook/commit/cd22d3051f68e759fe9398621e1e62d634615a3f))
* **github-action:** Update action jdx/mise-action (v4.2.0 → v4.2.1) ([39bb0fb](https://github.com/home-operations/external-dns-unifi-webhook/commit/39bb0fb6eab32a81c3e7b39ea929ed4710b02baf))
* **github-action:** Update action jdx/mise-action (v4.2.1 → v4.2.2) ([#288](https://github.com/home-operations/external-dns-unifi-webhook/issues/288)) ([b371250](https://github.com/home-operations/external-dns-unifi-webhook/commit/b3712503e89299c47b91a3b5926e455cc135f776))
* **github-action:** Update action jdx/mise-action (v4.2.2 → v4.2.3) ([#290](https://github.com/home-operations/external-dns-unifi-webhook/issues/290)) ([af33171](https://github.com/home-operations/external-dns-unifi-webhook/commit/af33171612a6bbf5f370259c060c292b58682721))
* **github-action:** Update action jdx/mise-action (v4.2.3 → v4.2.4) ([#303](https://github.com/home-operations/external-dns-unifi-webhook/issues/303)) ([f9e4782](https://github.com/home-operations/external-dns-unifi-webhook/commit/f9e4782351819ddfa2b532168569d35b701e27d6))
* **github-action:** update workflow-lint action (1.0.0 → v1.0.2) ([#298](https://github.com/home-operations/external-dns-unifi-webhook/issues/298)) ([b36f7e7](https://github.com/home-operations/external-dns-unifi-webhook/commit/b36f7e75ff7f052cb6d15d18d66676cbb2346055))
* lint workflows with the shared composite action ([#287](https://github.com/home-operations/external-dns-unifi-webhook/issues/287)) ([24556e2](https://github.com/home-operations/external-dns-unifi-webhook/commit/24556e24ec4046ccba73d679f5623e9fdcd4d813))
* **renovate:** reactive dashboard + config runs in one workflow ([#282](https://github.com/home-operations/external-dns-unifi-webhook/issues/282)) ([73dc821](https://github.com/home-operations/external-dns-unifi-webhook/commit/73dc821759d454d8399fb9ebfc515b2fc9ca9938))
* skip release-please churn and stop cancelling main runs ([#275](https://github.com/home-operations/external-dns-unifi-webhook/issues/275)) ([9608dc2](https://github.com/home-operations/external-dns-unifi-webhook/commit/9608dc2a96d34229f6cf55e7cbe60580c03f53bf))
* skip release-please version-bump PRs in checks ([#283](https://github.com/home-operations/external-dns-unifi-webhook/issues/283)) ([45c7f27](https://github.com/home-operations/external-dns-unifi-webhook/commit/45c7f27b3f29e49f5a3a410d67c5e4dde5cf7d72))
* wire govulncheck into mise and CI ([#300](https://github.com/home-operations/external-dns-unifi-webhook/issues/300)) ([8ea7966](https://github.com/home-operations/external-dns-unifi-webhook/commit/8ea7966c7dc831fed74625493e9df101a6a1af31))


### Miscellaneous Chores

* **github-action:** update action jdx/mise-action (v4.2.4 → v4.2.5) ([#309](https://github.com/home-operations/external-dns-unifi-webhook/issues/309)) ([6291830](https://github.com/home-operations/external-dns-unifi-webhook/commit/6291830e0039c132d4eabfe1ac392325bc0fc509))
* **go:** pin go directive to 1.26.1 ([#310](https://github.com/home-operations/external-dns-unifi-webhook/issues/310)) ([0674b98](https://github.com/home-operations/external-dns-unifi-webhook/commit/0674b98e967522cdfa159735449c55d583bc22b5))
* **mise:** Lock file maintenance tool ([#280](https://github.com/home-operations/external-dns-unifi-webhook/issues/280)) ([a140d90](https://github.com/home-operations/external-dns-unifi-webhook/commit/a140d9019552a6187f62f7f8f39ec6adcf134c93))
* **mise:** prune lockfile to used platforms ([#299](https://github.com/home-operations/external-dns-unifi-webhook/issues/299)) ([d8cd350](https://github.com/home-operations/external-dns-unifi-webhook/commit/d8cd350ff4efb78838acc9e1c30c0eb7a87cc9f9))
* **mise:** Update tool go (1.26.4 → 1.26.5) ([#272](https://github.com/home-operations/external-dns-unifi-webhook/issues/272)) ([4e08b20](https://github.com/home-operations/external-dns-unifi-webhook/commit/4e08b20705d200d553cc6cc3ebbe6e803234c3a0))
* **mise:** update tool go (1.26.5 → 1.26.6) ([#311](https://github.com/home-operations/external-dns-unifi-webhook/issues/311)) ([25f566e](https://github.com/home-operations/external-dns-unifi-webhook/commit/25f566e3098a9fcbc97e6efa67e68c667d08c2cd))
* **mise:** update tool go:golang.org/x/vuln/cmd/govulncheck (1.6.0 → v1.7.0) ([#308](https://github.com/home-operations/external-dns-unifi-webhook/issues/308)) ([0874d3f](https://github.com/home-operations/external-dns-unifi-webhook/commit/0874d3f30848c0c66704817b85955a883e18550b))
* **mise:** Update tool lefthook (2.1.9 → 2.1.10) ([#270](https://github.com/home-operations/external-dns-unifi-webhook/issues/270)) ([94a9f77](https://github.com/home-operations/external-dns-unifi-webhook/commit/94a9f773263f0374121ade541b4d3f4fbc36027d))
* **mise:** Update tool oxfmt (0.57.0 → 0.58.0) ([#268](https://github.com/home-operations/external-dns-unifi-webhook/issues/268)) ([18cdddf](https://github.com/home-operations/external-dns-unifi-webhook/commit/18cdddf154a154f45ac9fef10102c23fd90e1f1a))
* **mise:** Update tool oxfmt (0.58.0 → 0.59.0) ([#273](https://github.com/home-operations/external-dns-unifi-webhook/issues/273)) ([1d1cf5b](https://github.com/home-operations/external-dns-unifi-webhook/commit/1d1cf5baf70529e77884d5755893c4dae5759e1f))
* **mise:** Update tool oxfmt (0.59.0 → 0.60.0) ([#279](https://github.com/home-operations/external-dns-unifi-webhook/issues/279)) ([ddcdf65](https://github.com/home-operations/external-dns-unifi-webhook/commit/ddcdf6500397c13130ed1451f36581afde8cd88e))
* **mise:** Update tool oxfmt (0.60.0 → 0.61.0) ([#289](https://github.com/home-operations/external-dns-unifi-webhook/issues/289)) ([ebbeed6](https://github.com/home-operations/external-dns-unifi-webhook/commit/ebbeed6c5153be3b827d300591910214b9afe301))
* **mise:** Update tool oxfmt (0.61.0 → 0.62.0) ([#304](https://github.com/home-operations/external-dns-unifi-webhook/issues/304)) ([b71b144](https://github.com/home-operations/external-dns-unifi-webhook/commit/b71b1447d6add4a97da8a8896e3e2367fec3b688))
* **mise:** Update tool oxfmt (0.62.0 → 0.63.0) ([#306](https://github.com/home-operations/external-dns-unifi-webhook/issues/306)) ([6a018b0](https://github.com/home-operations/external-dns-unifi-webhook/commit/6a018b064335723230d51ce9d5bb2c7961cc328b))
* **mise:** Update tool zizmor (1.26.1 → 1.27.0) ([#274](https://github.com/home-operations/external-dns-unifi-webhook/issues/274)) ([573fa64](https://github.com/home-operations/external-dns-unifi-webhook/commit/573fa648250173bbcec13957a2587edb41912cf0))
* **mise:** Update tool zizmor (1.27.0 → 1.28.0) ([#278](https://github.com/home-operations/external-dns-unifi-webhook/issues/278)) ([2f39159](https://github.com/home-operations/external-dns-unifi-webhook/commit/2f3915908967b1c840d61b010e053378e32d0a8c))
* **mise:** Update tool zizmor (1.28.0 → 1.29.0) ([#297](https://github.com/home-operations/external-dns-unifi-webhook/issues/297)) ([3173103](https://github.com/home-operations/external-dns-unifi-webhook/commit/31731039d3ef14fc7b4fdef146adcf9447c86d2b))
* **release-please:** standardize the release pull request title pattern ([#295](https://github.com/home-operations/external-dns-unifi-webhook/issues/295)) ([5f825de](https://github.com/home-operations/external-dns-unifi-webhook/commit/5f825dea0f634242d722bbcf08050bca986360b3))
* standardize release-please changelog sections ([#291](https://github.com/home-operations/external-dns-unifi-webhook/issues/291)) ([5f0866a](https://github.com/home-operations/external-dns-unifi-webhook/commit/5f0866ac9e3ddd4567820d3cc06e8246a54bb9ab))

## [0.10.9](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.8...0.10.9) (2026-07-04)


### Bug Fixes

* bind the health server dual-stack by default ([#266](https://github.com/home-operations/external-dns-unifi-webhook/issues/266)) ([bf1dcd3](https://github.com/home-operations/external-dns-unifi-webhook/commit/bf1dcd34b094477553fda4930445844c12fd5c19))

## [0.10.8](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.7...0.10.8) (2026-07-03)


### Bug Fixes

* **unifi:** don't treat a CNAME update as a conflict; count final-attempt 429s ([#265](https://github.com/home-operations/external-dns-unifi-webhook/issues/265)) ([0b31441](https://github.com/home-operations/external-dns-unifi-webhook/commit/0b314417af10f6ba837be4bb0f7069dbf463ab74))


### Miscellaneous Chores

* **mise:** Update tool oxfmt (0.56.0 → 0.57.0) ([#263](https://github.com/home-operations/external-dns-unifi-webhook/issues/263)) ([ee3403b](https://github.com/home-operations/external-dns-unifi-webhook/commit/ee3403b069a16143480ee801c4f4c702d0f849b9))

## [0.10.7](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.6...0.10.7) (2026-06-29)


### Bug Fixes

* **unifi:** drop redundant instrumented HTTP client causing unbounded path-cardinality metric ([#262](https://github.com/home-operations/external-dns-unifi-webhook/issues/262)) ([eb75811](https://github.com/home-operations/external-dns-unifi-webhook/commit/eb75811918f7b2753d6f1c81ab3f64c36988e245))


### Miscellaneous Chores

* add minimumGroupSize to Go toolchain configuration ([84d0d97](https://github.com/home-operations/external-dns-unifi-webhook/commit/84d0d97f3585a042e9d8e8a49067701d5ec2047a))
* **mise:** Update tool oxfmt (0.55.0 → 0.56.0) ([#259](https://github.com/home-operations/external-dns-unifi-webhook/issues/259)) ([9c9aeed](https://github.com/home-operations/external-dns-unifi-webhook/commit/9c9aeed11d48fa4c5b64ab738eea4e8a09463e4f))
* **mise:** Update tool zizmor (1.25.2 → 1.26.1) ([#257](https://github.com/home-operations/external-dns-unifi-webhook/issues/257)) ([d7dab15](https://github.com/home-operations/external-dns-unifi-webhook/commit/d7dab154f08be80b4e4fa12886f6c3e852d4c035))
* **renovate:** inherit shared toolchain group preset ([#261](https://github.com/home-operations/external-dns-unifi-webhook/issues/261)) ([f4881e7](https://github.com/home-operations/external-dns-unifi-webhook/commit/f4881e765682547c169c612462a2a28d654ab44f))

## [0.10.6](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.5...0.10.6) (2026-06-18)


### Features

* revert move the metrics/health server to 8081 ([b66dc41](https://github.com/home-operations/external-dns-unifi-webhook/commit/b66dc41d3ecfdd97df2d5c80ffff1af066414d79))

## [0.10.5](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.4...0.10.5) (2026-06-18)


### Features

* move the metrics/health server to 8081 ([#254](https://github.com/home-operations/external-dns-unifi-webhook/issues/254)) ([b840e51](https://github.com/home-operations/external-dns-unifi-webhook/commit/b840e517bb0212f58dddf800c187651ff2f62983))


### Miscellaneous Chores

* **mise:** update tool oxfmt (0.54.0 → 0.55.0) ([#251](https://github.com/home-operations/external-dns-unifi-webhook/issues/251)) ([10e847e](https://github.com/home-operations/external-dns-unifi-webhook/commit/10e847e6bed37da5e0f64e672b9d2c5c41d94659))

## [0.10.4](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.3...0.10.4) (2026-06-10)


### Bug Fixes

* **metrics:** bound the HTTP endpoint label to matched routes ([#244](https://github.com/home-operations/external-dns-unifi-webhook/issues/244)) ([f29edcb](https://github.com/home-operations/external-dns-unifi-webhook/commit/f29edcba85a5c64a24d7d6a73edfc0f53f1fc1a0))
* **metrics:** reset stale per-type records gauge after a type's records are deleted ([#235](https://github.com/home-operations/external-dns-unifi-webhook/issues/235)) ([1e3fab7](https://github.com/home-operations/external-dns-unifi-webhook/commit/1e3fab7d1b41f9fc577535c15270fb04d0ac2ad1))
* **metrics:** track consecutive_errors/last_success per operation ([#236](https://github.com/home-operations/external-dns-unifi-webhook/issues/236)) ([9fee152](https://github.com/home-operations/external-dns-unifi-webhook/commit/9fee152ffbb488c2635284fa1a0ee723d720fd30))
* **server:** run the readiness probe detached from the caller's request context ([#232](https://github.com/home-operations/external-dns-unifi-webhook/issues/232)) ([ec3e3b8](https://github.com/home-operations/external-dns-unifi-webhook/commit/ec3e3b81f70502555ed02ca8e0fa80502b2913f8))
* **server:** stop /readyz from leaking upstream detail to anonymous callers ([#242](https://github.com/home-operations/external-dns-unifi-webhook/issues/242)) ([a65595b](https://github.com/home-operations/external-dns-unifi-webhook/commit/a65595bc5b088a6c3d505fb5b80ddcf79fe23fae))
* **unifi:** account for the create-endpoint response body size ([#246](https://github.com/home-operations/external-dns-unifi-webhook/issues/246)) ([ba72eb2](https://github.com/home-operations/external-dns-unifi-webhook/commit/ba72eb23171db86e3a2acf9a538b1f10bb83f9b8))
* **unifi:** bound apply workers, retry attempts, and the connection pool ([#234](https://github.com/home-operations/external-dns-unifi-webhook/issues/234)) ([d1cde15](https://github.com/home-operations/external-dns-unifi-webhook/commit/d1cde15cb179be4e3573f5480ea7a2197a9e117a))
* **unifi:** cap the buffered JSON response read ([#238](https://github.com/home-operations/external-dns-unifi-webhook/issues/238)) ([27ba5a7](https://github.com/home-operations/external-dns-unifi-webhook/commit/27ba5a7f47fa1474813008185cfc47081f38ecc4))
* **unifi:** clear controller-managed TTL on TXT/MX/SRV to stop reconcile churn ([#243](https://github.com/home-operations/external-dns-unifi-webhook/issues/243)) ([47d5f1a](https://github.com/home-operations/external-dns-unifi-webhook/commit/47d5f1a807a0137305184866281e67bfca26a5c5))
* **unifi:** don't retry non-idempotent POST creates on 5xx/network errors ([#233](https://github.com/home-operations/external-dns-unifi-webhook/issues/233)) ([0b8178c](https://github.com/home-operations/external-dns-unifi-webhook/commit/0b8178c2854e68d8244772064a50ad36807d9798))
* **unifi:** guard backoff jitter against a panicking divisor ([#245](https://github.com/home-operations/external-dns-unifi-webhook/issues/245)) ([b07a6be](https://github.com/home-operations/external-dns-unifi-webhook/commit/b07a6be82d2d1126bcef66294ab397f8f12a9acb))
* **unifi:** key record groups by a struct, not concatenated name+type ([#239](https://github.com/home-operations/external-dns-unifi-webhook/issues/239)) ([ad65d42](https://github.com/home-operations/external-dns-unifi-webhook/commit/ad65d422543f154b3efa6a3ac04aeabd2decc2b9))
* **unifi:** size the startup site-resolution timeout to the retry budget ([#240](https://github.com/home-operations/external-dns-unifi-webhook/issues/240)) ([bd6f975](https://github.com/home-operations/external-dns-unifi-webhook/commit/bd6f9750217ec5a2b2b9aa4e8617cf8f3a582d22))
* **unifi:** skip disabled UniFi policies instead of reporting them as live ([#241](https://github.com/home-operations/external-dns-unifi-webhook/issues/241)) ([8ff327c](https://github.com/home-operations/external-dns-unifi-webhook/commit/8ff327c543bc456bd1ec3e0ea210be9915cfa57d))
* **webhook:** log AdjustEndpoints errors; drop no-op Negotiate WriteHeader ([#247](https://github.com/home-operations/external-dns-unifi-webhook/issues/247)) ([af8e136](https://github.com/home-operations/external-dns-unifi-webhook/commit/af8e136a4d6724f200f688ea4d776cc0bfa68fb8)), closes [#227](https://github.com/home-operations/external-dns-unifi-webhook/issues/227) [#228](https://github.com/home-operations/external-dns-unifi-webhook/issues/228)

## [0.10.3](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.2...0.10.3) (2026-06-10)


### Features

* **deps:** update module golang.org/x/sync (v0.20.0 → v0.21.0) ([#210](https://github.com/home-operations/external-dns-unifi-webhook/issues/210)) ([d2d80c3](https://github.com/home-operations/external-dns-unifi-webhook/commit/d2d80c3620ee4a8bea6462de25a6470f22e681fd))
* **mise:** update tool oxfmt (0.53.0 → 0.54.0) ([#211](https://github.com/home-operations/external-dns-unifi-webhook/issues/211)) ([8403ccc](https://github.com/home-operations/external-dns-unifi-webhook/commit/8403cccfc77671270743227bac7d1ef42c923dec))


### Bug Fixes

* **mise:** update tool go (1.26.3 → 1.26.4) ([38b8819](https://github.com/home-operations/external-dns-unifi-webhook/commit/38b881966e6a28e6b32cc52557b9a1baba9631ee))


### Miscellaneous Chores

* move mise to mise folder ([45e9f2f](https://github.com/home-operations/external-dns-unifi-webhook/commit/45e9f2f6abd011ee22d4b84fccf8a2f724d77179))
* update release-please-config.json to remove paths ([cf07cb9](https://github.com/home-operations/external-dns-unifi-webhook/commit/cf07cb9e46487a636c7f5c25e7a98175d65b8a34))
* update rlspls workflow name ([9f96b22](https://github.com/home-operations/external-dns-unifi-webhook/commit/9f96b220970727ed6e0cb3841bc9cd9a2cf4d12f))

## [0.10.2](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.1...0.10.2) (2026-06-02)


### Features

* **mise:** update tool oxfmt (0.52.0 → 0.53.0) ([0a5c909](https://github.com/home-operations/external-dns-unifi-webhook/commit/0a5c9098ad81768bcabeb70d71fd7e781482cd55))


### Bug Fixes

* **mise:** update tool lefthook (2.1.8 → 2.1.9) ([ee5439f](https://github.com/home-operations/external-dns-unifi-webhook/commit/ee5439f0645f16734f3479c22316e2380ef5747b))


### Miscellaneous Chores

* Delete TESTING.md ([f1b4c05](https://github.com/home-operations/external-dns-unifi-webhook/commit/f1b4c05ff27500622bd009048864b8f1bf6370d4))
* mise lock ([3c8e388](https://github.com/home-operations/external-dns-unifi-webhook/commit/3c8e3886578ab78116d4b624615595ef9467c04b))
* remove 'Contents' section from README ([06d5007](https://github.com/home-operations/external-dns-unifi-webhook/commit/06d50075eb86e5778201e225edc019c3a923c314))
* update UniFi OS minimum version to 5.x ([bea273e](https://github.com/home-operations/external-dns-unifi-webhook/commit/bea273e7f5a18e9df96b2bc0974356317aa728dc))


### Code Refactoring

* webhook/provider generics + unset API key from env after load ([#207](https://github.com/home-operations/external-dns-unifi-webhook/issues/207)) ([90a4723](https://github.com/home-operations/external-dns-unifi-webhook/commit/90a4723737baa061495790fc45cc4a3a1f38fe70))

## [0.10.1](https://github.com/home-operations/external-dns-unifi-webhook/compare/0.10.0...0.10.1) (2026-05-31)


### Features

* support the api.ui.com cloud connector ([#203](https://github.com/home-operations/external-dns-unifi-webhook/issues/203)) ([4f804bb](https://github.com/home-operations/external-dns-unifi-webhook/commit/4f804bbb948110923bff0f99164010a9ec3ade8e))


### Code Refactoring

* code cleanliness, logging & Go 1.26 idioms ([#200](https://github.com/home-operations/external-dns-unifi-webhook/issues/200)) ([08b1415](https://github.com/home-operations/external-dns-unifi-webhook/commit/08b141552677bd570ecf76b633787176005ad697))

## [0.10.0](https://github.com/home-operations/external-dns-unifi-webhook/compare/v0.9.0...0.10.0) (2026-05-31)


### ⚠ BREAKING CHANGES

* code quality and Go 1.26 modernization pass ([#199](https://github.com/home-operations/external-dns-unifi-webhook/issues/199))

### Features

* migration to home-operations org ([570fab4](https://github.com/home-operations/external-dns-unifi-webhook/commit/570fab47be5d2f0b680dd2d9507d43abbf31c83e))


### Code Refactoring

* code quality and Go 1.26 modernization pass ([#199](https://github.com/home-operations/external-dns-unifi-webhook/issues/199)) ([9ecf2d3](https://github.com/home-operations/external-dns-unifi-webhook/commit/9ecf2d3e49b7361b82429890c70b34a886fae5f9))
