# PAMUNGKAS Brave Portable — Portable Session Handoff

## START-HERE

Lifecycle: `EXISTING_PRODUCT_MAINTENANCE`

Repositories:
- launcher/integration: `pribadimartabat2/brave-portable`
- Brave engine: `pribadimartabat2/brave-core`

Working branches:
- `brave-portable`: `pamungkas/portable-session-root-cause`
- `brave-core`: `pamungkas/portable-oscrypt-poc`

Goal: native Brave, Chrome Web Store extensions, Brave Shields, and one portable profile whose locally encrypted sessions can move between Windows computers without a VM.

Status: **NO-GO for release** until full patched-Brave build and real PC A -> PC B -> PC A evidence pass.

## ROOT CAUSE

Portapps already launches Brave with portable `--user-data-dir`, `--disable-machine-id`, and historical `--disable-encryption-win`.

Modern Brave/Chromium uses OSCryptAsync. The historical synchronous implementation that made `--disable-encryption-win` portable was removed, so carrying the flag no longer proves that new cookie/session secrets are protected by a machine-independent key.

A profile can therefore move correctly while secret material remains tied to the Windows context that created it.

## CANONICAL FIX

### Authority for new encryption — Brave Core

`pribadimartabat2/brave-core`, branch `pamungkas/portable-oscrypt-poc` adds:

- custom OSCryptAsync provider prefix `brp1`;
- 32-byte portable AES-256-GCM key;
- key file inside the explicit portable user-data directory;
- provider precedence 20;
- DPAPI v10 and App-Bound v20 retained for legacy decryption only while portable mode is active;
- malformed/missing existing portable key fails closed instead of silently generating over corruption;
- minimal Chromium hook rather than copying BrowserProcessImpl logic.

Portable key file contract:

`PBK1` + 32-byte key = exactly 36 bytes.

This `brp1` provider is the authority for **new encrypted data** in portable mode.

### Legacy compatibility bridge — launcher

`internal/portablecrypto` in `brave-portable` remains only as a bounded v10/DPAPI bridge for legacy profiles:

- captures the old Chromium v10 AES master key on a Windows context that can still decrypt it;
- re-wraps the same legacy key with DPAPI for the current Windows context;
- never becomes the authority for new encrypted data;
- does not solve App-Bound v20 by itself.

This layer exists to improve legacy migration continuity while `brp1` handles new writes.

## FAIL-CLOSED RUNTIME CONTRACT

The launcher now performs a postflight after Brave exits.

`validatePortableRuntimePostflight()` requires:

1. `Local State` exists;
2. `Portable Encryption Key` exists;
3. the key path is a regular file;
4. file size is exactly 36 bytes.

The diagnostic uses metadata only. It never opens or reads the portable key file.

If postflight fails:

- launcher logs `NO-GO: patched Brave portable OSCrypt runtime was not verified`;
- legacy key capture is skipped;
- the run is not treated as proof that portable encryption is active.

This specifically catches a package that accidentally still contains stock Brave: stock Brave will not create the `PBK1` portable key expected from the patched engine.

## SAFE DIAGNOSTIC

`brave-portable.exe --pamungkas-portability-diagnostic`

writes `diagnostics/portable-session.json` with presence-only metadata:

- Local State present;
- cookie database present;
- extension directory present;
- legacy DPAPI/App-Bound metadata present;
- portable key present;
- portable key size-valid.

It does not serialize cookie values, password values, DPAPI/App-Bound key values, or the portable key contents.

## TEST EVIDENCE

Brave Core targeted Windows evidence:

- portable key creation/reuse contract: PASS;
- malformed existing key rejection: PASS;
- PBK1 + 32-byte exact format contract: PASS;
- Chromium hook applies to pinned Chromium `153.0.8010.28`: PASS;
- static provider precedence/wrapper/build contract: PASS.

Brave Portable TDD evidence:

- diagnostic presence-only behavior: PASS;
- portable key metadata test RED then GREEN;
- stock/broken-engine postflight test RED then GREEN;
- postflight does not read portable key contents.

GitHub CI may still show unrelated upstream-fork workflow noise. A previous `Compare Chromium versions` failure was proven to occur after reporting an exact Chromium version match because the workflow attempted to remove a label that did not exist. The fork workflow was hardened so label removal is idempotent.

## SECURITY CONTRACT

The portable key intentionally travels with the profile. Therefore possession of an unlocked portable drive materially weakens machine-bound theft resistance.

Required production posture:

- use encrypted removable storage/full-volume encryption;
- never log/export/upload the portable key;
- never place key bytes in diagnostics;
- later passphrase-wrapped key storage may be added, but not before the basic cross-PC portability path is proven.

## MIGRATION CONTRACT

Fresh profile:
- new encrypted secrets should use `brp1` immediately.

Existing profile:
- v10/v20 providers remain available for decryption on a context that can unlock the old data;
- data decrypted by a non-current provider is expected to be eligible for re-encryption to the current provider;
- store-wide migration of cookies/passwords must be proven, not assumed;
- already machine-bound secrets cannot be promised recoverable if the original decrypt-capable context is gone.

## PACKAGING TRUTH

Current Portapps `build.properties` still downloads the official Brave installer from Brave's S3 URL.

Therefore current Portapps packaging **cannot yet be the final portable release**, because it would package stock Brave rather than the patched `brp1` engine.

The final packaging path must consume a Windows binary built from the patched Brave Core branch and must reject accidental fallback to the stock installer.

## RELEASE GATES

Status: `NO-GO`.

Required before merge/release:

1. full patched Brave Windows compile PASS;
2. packaged launcher consumes the patched binary, not stock installer;
3. fresh profile creates `Portable Encryption Key` and launches normally;
4. postflight PASS;
5. Chrome Web Store extension installs and remains enabled;
6. test sessions survive clean shutdown and PC A -> PC B;
7. return PC B -> PC A remains valid;
8. existing-profile migration test on original decrypt-capable PC then PC B;
9. corrupt/missing-key behavior is explicit and non-destructive;
10. no cookie/password/key contents appear in logs or diagnostics;
11. FULLPACK/release artifact and checksums produced only after the gates above.

## WHAT-NEXT

1. Establish the canonical Windows build workspace for patched Brave Core.
2. Produce the first patched Brave Windows binary.
3. Change Portapps packaging to consume that binary with a hard anti-stock gate.
4. Execute PC A -> PC B -> PC A test matrix.
5. Only then prepare release artifacts.
