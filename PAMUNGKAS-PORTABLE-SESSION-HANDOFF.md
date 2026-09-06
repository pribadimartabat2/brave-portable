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
- portable initialization state is bound to the key via PBS2 fingerprint state;
- minimal Chromium hook rather than copying BrowserProcessImpl logic.

Portable key contracts:

- `Portable Encryption Key`: `PBK1` + 32-byte key = exactly 36 bytes;
- `Portable Encryption Key.state`: `PBS2` + 8-byte fingerprint = exactly 12 bytes.

The PBS2 state makes accidental same-size key replacement detectable by the engine. The launcher checks only metadata; the engine remains authority for fingerprint validation.

This `brp1` provider is the authority for **new encrypted data** in portable mode.

### Legacy compatibility bridge — launcher

`internal/portablecrypto` in `brave-portable` remains only as a bounded v10/DPAPI bridge for legacy profiles:

- captures the old Chromium v10 AES master key on a Windows context that can still decrypt it;
- re-wraps the same legacy key with DPAPI for the current Windows context;
- never becomes the authority for new encrypted data;
- does not solve App-Bound v20 by itself.

This layer exists to improve legacy migration continuity while `brp1` handles new writes.

## FAIL-CLOSED RUNTIME CONTRACT

The launcher performs a postflight after Brave exits.

`validatePortableRuntimePostflight()` requires:

1. `Local State` exists;
2. `Portable Encryption Key` exists;
3. key path is a regular 36-byte PBK1 file;
4. `Portable Encryption Key.state` exists;
5. state path is a regular 12-byte PBS2 file.

The launcher diagnostic checks metadata only. It does not open/read the key or state fingerprint contents.

If postflight fails:

- launcher reports patched portable OSCrypt runtime as NO-GO;
- legacy key capture is skipped;
- the run is not treated as proof that portable encryption is active.

This catches accidental packaging of stock Brave because stock Brave does not create the PBK1/PBS2 artifacts expected from the patched engine.

## SAFE DIAGNOSTIC

`brave-portable.exe --pamungkas-portability-diagnostic`

writes `diagnostics/portable-session.json` with presence-only metadata:

- Local State present;
- cookie database present;
- extension directory present;
- legacy DPAPI/App-Bound metadata present;
- portable key present/size-valid;
- portable state present/size-valid.

It does not serialize cookie values, password values, DPAPI/App-Bound key values, portable key contents, or fingerprint contents.

## TEST EVIDENCE

Brave Core targeted Windows evidence:

- portable key creation/reuse contract: PASS;
- malformed existing key rejection: PASS;
- missing initialized key fail-closed: PASS;
- corrected hidden-file mutation test proves presence-only state is insufficient: RED at core commit `7c94d8150b83a8cd07c00a37657c93ba777ec78e`;
- PBS2 fingerprint implementation has previously passed the corrected mutation test; current restored-head CI must remain PASS before release claims;
- Chromium hook applies to pinned Chromium `153.0.8010.28`;
- static provider precedence/wrapper/build contract is checked.

Brave Portable TDD evidence:

- state-required postflight RED: commit `460449fd21a89697c87fcb69d95a3789c64cc795`, expected failure because a 36-byte key without state was still accepted;
- state-required postflight implementation: commit `4ff6176159a10c914e26ab0eb082b2d461ca26f2`;
- syntax defect isolated and fixed at commit `1e4259c5f52d08c741a491998ef3244119813148`;
- targeted `pamungkas-portable-session` CI on `1e4259c5...`: PASS;
- diagnostic presence-only behavior: PASS;
- v10 legacy portablecrypto module: PASS.

An earlier upstream-fork `Compare Chromium versions` failure was proven unrelated to the browser code: versions matched exactly, but label removal failed when the label did not exist. Branch workflow cleanup is now idempotent.

## SECURITY CONTRACT

The portable key intentionally travels with the profile. Therefore possession of an unlocked portable drive materially weakens machine-bound theft resistance.

Required production posture:

- use encrypted removable storage/full-volume encryption;
- never log/export/upload the portable key;
- never place key or fingerprint bytes in diagnostics;
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

1. current-head targeted CI PASS in both repos;
2. full patched Brave Windows `Release` compile PASS;
3. packaged launcher consumes patched binary, not stock installer;
4. fresh profile creates valid PBK1 + PBS2 artifacts and launches normally;
5. launcher postflight PASS;
6. Chrome Web Store extension installs and remains enabled;
7. test sessions survive clean shutdown and PC A -> PC B;
8. return PC B -> PC A remains valid;
9. existing-profile migration test on original decrypt-capable PC then PC B;
10. corrupt/missing/replaced-key behavior is explicit and non-destructive;
11. no cookie/password/key/fingerprint contents appear in logs or diagnostics;
12. FULLPACK/release artifact and checksums produced only after the gates above.

## WHAT-NEXT

1. Observe current restored PBS2 core CI PASS.
2. Use `tools/pamungkas/windows-build.ps1 -CheckOnly` in a real Windows build workspace with sufficient disk.
3. Run the first full patched Brave Windows `Release` build.
4. Change Portapps packaging to consume that binary with a hard anti-stock gate.
5. Execute PC A -> PC B -> PC A test matrix.
6. Only then prepare release artifacts.
