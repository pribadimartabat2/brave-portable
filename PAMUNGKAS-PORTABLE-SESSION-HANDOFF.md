# PAMUNGKAS Brave Portable — Portable Session Handoff

## START-HERE

Lifecycle: `EXISTING_PRODUCT_MAINTENANCE`

Repositories:
- launcher/integration: `pribadimartabat2/brave-portable`
- Brave engine/build authority: `pribadimartabat2/brave-core`

Working branches:
- `brave-portable`: `pamungkas/portable-session-root-cause`
- `brave-core`: `pamungkas/portable-oscrypt-poc`

Goal: native Brave + Brave Shields + Chrome Web Store extensions + one owner-controlled portable profile whose locally encrypted sessions can move between Windows computers without a VM browser runtime.

Status: **NO-GO for release** until full patched-Brave build and real PC A -> PC B -> PC A evidence pass.

## ROOT CAUSE

Portapps already launches Brave with portable `--user-data-dir`, `--disable-machine-id`, and historical `--disable-encryption-win`.

Modern Brave/Chromium uses OSCryptAsync. The historical synchronous implementation that made `--disable-encryption-win` portable was removed, so carrying the flag no longer proves that new cookie/session secrets are protected by a machine-independent key.

## ENGINE AUTHORITY — BRAVE CORE

`pribadimartabat2/brave-core`, branch `pamungkas/portable-oscrypt-poc` implements:

- custom OSCryptAsync prefix `brp1`;
- AES-256-GCM portable key;
- provider precedence 20;
- DPAPI v10 and App-Bound v20 retained for legacy decryption only in portable mode;
- `Portable Encryption Key`: `PBK1` + 32-byte key = 36 bytes;
- `Portable Encryption Key.state`: `PBS2` + 8-byte fingerprint = 12 bytes;
- missing initialized key fails closed;
- malformed or same-size replaced key fails closed;
- raw temporary key buffers are wiped;
- minimal Chromium hook instead of copying BrowserProcessImpl logic.

The engine, not the launcher, is authority for PBS2 fingerprint validation.

## LAUNCHER RUNTIME CONTRACT

`validatePortableRuntimePostflight()` requires:

1. `Local State` exists;
2. `Portable Encryption Key` exists;
3. key is a regular 36-byte file;
4. `Portable Encryption Key.state` exists;
5. state is a regular 12-byte file.

The launcher checks metadata only and does not read portable key/fingerprint contents.

If postflight fails, portability is NO-GO and legacy key capture is skipped.

## SAFE DIAGNOSTIC

`brave-portable.exe --pamungkas-portability-diagnostic`

reports presence-only metadata for:

- Local State;
- cookie database;
- extension directory;
- legacy DPAPI/App-Bound metadata;
- portable key present/size-valid;
- PBS2 state present/size-valid.

It never serializes cookie values, passwords, DPAPI/App-Bound key values, portable key bytes, or fingerprint bytes.

## LEGACY COMPATIBILITY

`internal/portablecrypto` remains only a bounded v10/DPAPI migration bridge. It is not authority for new encryption and does not solve App-Bound v20 by itself.

Existing v10/v20 data may only be migrated while a Windows context capable of decrypting it is still available. Store-wide migration must be proven, not assumed.

## PACKAGING HARD GATE

Portapps originally used:

`https://brave-browser-downloads.s3.brave.com/latest/brave_installer-x64.exe`

That is stock Brave and cannot contain `brp1`.

`.github/workflows/build.yml` now has `PAMUNGKAS patched-engine authority` before the Portapps reusable build.

Packaging is allowed only when:

- `atf.win64.url` starts with `https://github.com/pribadimartabat2/brave-core/releases/download/`;
- stock Brave S3 is absent;
- `pamungkas.engine.sha256` is a valid 64-character SHA-256;
- the downloaded candidate exactly matches that SHA-256.

Current stock configuration intentionally produces:

`PAMUNGKAS PACKAGING NO-GO`

and the actual Portapps build job is SKIPPED. This is correct evidence, not a regression.

## GOVERNED PATCHED-ENGINE CONFIGURATION

### Direct URL + SHA helper

`tools/pamungkas/set-patched-engine.ps1`

- rejects stock/non-governed URLs before mutation;
- rejects invalid SHA-256 before mutation;
- requires exactly one `atf.win64.url` entry;
- writes the governed URL + `pamungkas.engine.sha256` atomically in the same directory;
- rejected inputs leave `build.properties` unchanged.

TDD:
- RED: workflow commit `8049c75816cc5aa855c0bb1495d42fad7c88e43a` because helper did not exist;
- GREEN implementation: `5cf138f3938f144134148ee65378c0fefb2a4115`;
- `pamungkas-portable-session`: PASS;
- build workflow remains intentionally NO-GO on stock URL.

### Dist-evidence bridge

`tools/pamungkas/set-patched-engine-from-evidence.ps1`

Purpose: avoid manually copying a hash from a completed core build.

It requires:

- `windows-dist-result.json`;
- evidence status exactly `DIST_PASS`;
- installer record exactly `brave_installer.exe`;
- positive installer byte size;
- valid 64-character installer SHA-256;
- safe candidate release tag (`A-Z`, `a-z`, digits, `.`, `_`, `-`).

It derives:

`https://github.com/pribadimartabat2/brave-core/releases/download/<tag>/brave_installer.exe`

and passes that URL + evidence SHA to the governed direct setter.

TDD:
- RED: `a21925c7a40bdf7e2a4e7b13fd6e87ced563ae22`, all existing tests PASS and evidence-bridge fails because helper is absent;
- first implementation: `cdacaffd9d23023b34b81cf3789a5e264e69b73d`;
- root-cause of first GREEN failure: `$LASTEXITCODE` was incorrectly inspected after invoking a PowerShell script, despite the setter itself succeeding;
- corrected implementation: `92853600c6e91ec8607d750455b0b4fb09d6e547`;
- current-head GREEN must be observed before this bridge is counted as completed evidence.

## BRAVE CORE BUILD/DIST PIPELINE

Core harness:

`tools/pamungkas/windows-build.ps1`

supports:

- `-CheckOnly`;
- `-Initialize`;
- `-CreateDist` for Release;
- minimum free-disk gate, default 120 GB.

A successful build/dist must create:

- `windows-build-preflight.json`;
- `windows-build-result.json`;
- `windows-dist-result.json`;
- `src/out/Release/brave_installer.exe`;
- one or more `src/out/Release/dist/*.zip`;
- SHA-256 evidence for produced artifacts.

`create_dist` uses Brave's official target with `--skip_signing` for the internal PoC candidate.

## CURRENT BUILD-MACHINE AUTHORITY

The branch contains a future manual GitHub self-hosted workflow, but `workflow_dispatch` only becomes triggerable when that workflow file is on the default branch. The PoC must not be merged merely to enable a build button.

Therefore the current build authority is **direct execution of `windows-build.ps1` on a disposable Windows VM/build machine**.

Because these forks are public, do not attach a borrowed/personal computer as a persistent GitHub self-hosted runner. Preferred posture is a disposable VM with no personal accounts/secrets, destroyed after candidate artifacts/evidence are copied out.

## CORE TEST EVIDENCE

Verified targeted core checks include:

- key creation/reuse;
- malformed key rejection;
- missing initialized key fail-closed;
- same-size key replacement rejection via PBS2;
- C++ helper compile/runtime;
- Chromium `153.0.8010.28` patch application;
- provider/wrapper/build contract;
- build harness parser;
- create_dist / unsigned PoC / SHA-256 evidence contract.

Correct same-size replacement TDD:
- RED: `7c94d8150b83a8cd07c00a37657c93ba777ec78e`;
- PBS2 restored and targeted CI PASS afterward.

## SECURITY CONTRACT

The portable key intentionally travels with the portable profile. Therefore unlocked portable media is sensitive.

Required production posture:

- encrypted removable/full-volume storage;
- never log/export/upload portable key bytes;
- never expose fingerprint contents in diagnostics;
- build machine contains no personal browser sessions or unrelated credentials;
- passphrase-wrapped portable key may be considered later after basic portability is proven.

## RELEASE GATES

Status: `NO-GO`.

Required before merge/release:

1. current-head targeted CI PASS in both repos;
2. full patched Brave Windows `Release` compile PASS on a suitable disposable build machine;
3. `create_dist` PASS + SHA-256 evidence;
4. candidate installer published through governed candidate path with matching SHA;
5. Portapps packaging consumes patched candidate and anti-stock gate PASS;
6. fresh profile creates valid PBK1 + PBS2 and launches normally;
7. launcher postflight PASS;
8. Chrome Web Store extension installs and remains enabled;
9. controlled own-account sessions survive clean shutdown and PC A -> PC B;
10. return PC B -> PC A remains valid;
11. existing-profile migration test on original decrypt-capable PC then PC B;
12. corrupt/missing/replaced-key behavior remains non-destructive;
13. no cookie/password/key/fingerprint contents appear in logs or diagnostics;
14. final release/FULLPACK artifacts only after every gate above.

## WHAT-NEXT

1. Observe GREEN for corrected dist-evidence bridge commit `92853600...`.
2. Obtain a disposable Windows build VM/machine with at least 120 GB free and Brave build prerequisites.
3. Run core preflight, then full `Release -CreateDist` build.
4. Publish only the resulting candidate installer through a governed candidate release path after checking evidence.
5. Configure Portapps from `windows-dist-result.json` with the evidence bridge.
6. Build the integrated portable candidate.
7. Execute PC A -> PC B -> PC A runtime matrix.
8. Only then prepare final release artifacts.
