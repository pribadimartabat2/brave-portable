# PAMUNGKAS Brave Portable — Portable Session Handoff

## START-HERE

Lifecycle: `EXISTING_PRODUCT_MAINTENANCE`

Repository: `pribadimartabat2/brave-portable`

Working branch: `pamungkas/portable-session-root-cause`

Goal: Brave remains native Windows software, keeps Chrome-extension compatibility, and carries the same browser profile/session across different Windows computers without relying on VM hardware virtualization.

This branch is diagnostic/engineering work only. It is **not release-ready** and must not be merged as a claim that cross-machine sessions are fixed.

## CURRENT-SNAPSHOT

The Portapps launcher already passes:

- `--user-data-dir=<portable data path>`
- `--disable-machine-id`
- `--disable-encryption-win`

The launcher therefore already expresses the historical Brave portability contract. Adding the same switches again is not a fix.

A safe diagnostic command is available:

```text
brave-portable.exe --pamungkas-portability-diagnostic
```

It writes `diagnostics/portable-session.json` containing presence-only metadata. It does not serialize cookie contents, password contents, or encryption-key values.

## ROOT CAUSE EVIDENCE

### Historical known-good mechanic

Brave PR `brave/brave-core#795` added `disable-machine-id` and `disable-encryption-win` for portability. Its Windows test plan explicitly required copying browser data to a second computer and remaining logged in with credentials intact.

The historical implementation of `disable-encryption-win` intercepted synchronous Windows OSCrypt encryption/decryption in `components/os_crypt/os_crypt_win.cc` and returned the input unchanged when the switch was active.

### Architecture transition

In 2023 Chromium moved synchronous OSCrypt into `components/os_crypt/sync` as preparation for the new asynchronous OSCrypt implementation.

On 22 May 2026 Brave commit `b8aa9d78ba1ceca5a122273a47fd1d8ce3ec000d` removed all remnants of `components/os_crypt/sync` because Brave had fully transitioned to `OSCryptAsync`.

That deletion removed Brave's Windows override containing the `kDisableEncryptionWin` handling.

### Current mechanic

Current Chromium Windows encryption uses `OSCryptAsync` key providers:

- `DPAPIKeyProvider`, tag `v10`, whose stored key is decrypted through Windows DPAPI.
- `AppBoundEncryptionProviderWin`, tag `v20`, which uses application/path-bound protection and has higher encryption precedence than the DPAPI provider when supported.

Current Brave still defines the `disable-encryption-win` switch constant, while current source no longer contains the old synchronous override that implemented its portable-encryption behavior.

Therefore the current Portapps launcher can pass a syntactically valid historical switch while current cookie/password encryption follows a different backend.

## ROOT-CAUSE CONCLUSION

The cross-machine logout problem cannot be reliably repaired in the Portapps launcher alone.

The smallest compliant fix now crosses the boundary into `brave-core`: restore the *intent* of Brave's proven portable mechanic on top of `OSCryptAsync`, rather than reviving the deleted synchronous backend.

## TARGET DESIGN

Implement a Windows-only `PortableKeyProvider` in a fork of `brave/brave-core`.

Activation must be explicit and tied to the existing `--disable-encryption-win` portability contract so ordinary Brave behavior is unchanged.

Expected provider behavior:

1. Enabled only when the portability switch is present.
2. Uses one persistent 256-bit portable key stored with the portable profile, not Windows DPAPI/App-Bound machine identity.
3. Uses a unique OSCrypt tag that does not overlap Chromium tags such as `v10` or `v20`.
4. Has encryption precedence above DPAPI and App-Bound only in explicit portable mode.
5. Keeps DPAPI/App-Bound providers available for decryption of legacy data where their keys remain available; portable mode must never silently destroy an existing profile.
6. A newly created portable profile must encrypt new cookie/password data with the portable provider.
7. Existing Portapps `--disable-machine-id` behavior remains unchanged for extension/settings portability.

Security contract: because the portable key must travel with the profile, the portable disk/profile must be protected by owner-controlled full-volume encryption. Do not pretend that Windows machine-bound protection can coexist with transparent cross-machine portability.

## MIGRATION RULE

Do **not** silently convert an existing machine-bound profile in place.

Initial proof must use a fresh test profile. Existing-profile migration requires its own backup, validation, rollback and evidence phase after the new provider proves cross-machine operation.

## HARD TEST GATE

No PASS/release-ready claim until all are observed on real distinct Windows machines.

### Machine A

1. Start a fresh portable profile.
2. Confirm portable mode is active.
3. Install at least one Chrome Web Store extension.
4. Login with dedicated test accounts to two ordinary test sites that use persistent cookies.
5. Close Brave normally.
6. Verify no Brave process remains and the profile is flushed.

### Machine B

1. Move the same portable folder/drive to a physically different Windows PC/user profile.
2. Start the same portable launcher.
3. Confirm installed extension remains enabled.
4. Confirm both test-site sessions remain authenticated without re-login.
5. Close and return the drive to Machine A.

### Machine A return

1. Confirm sessions still work.
2. Confirm extension/settings remain intact.
3. Confirm no Local State/profile corruption.

Google, GitHub and ChatGPT may additionally perform server-side device/risk verification. They are useful secondary tests, but release evidence must first prove that local cookie decryption survives Machine A -> Machine B -> Machine A using controlled test sites.

## NON-REGRESSION

Must preserve:

- normal Brave launch when portable switch is absent;
- Brave Shields;
- Chrome Web Store extensions;
- portable relative `user-data-dir`;
- existing Portapps registry import/export behavior;
- normal shutdown and profile flush;
- upstream Portapps packaging/build path unless source-fork packaging evidence requires a bounded change.

## CURRENT EVIDENCE

- Portapps Windows packaging/build workflow: PASS on the diagnostic branch.
- Zizmor workflow: PASS.
- Portable diagnostic module: implemented; dedicated CI gate is being isolated from Portapps launcher `init()` so tests do not hang by initializing the full launcher runtime.
- Real A -> B cross-machine session retention: NOT TESTED / NO-GO.

## WHAT-NEXT

Required repository for implementation: fork `brave/brave-core` into the Owner GitHub account, preferably as:

`pribadimartabat2/brave-core`

Then create a governed feature branch in that fork and implement the Windows `OSCryptAsync PortableKeyProvider` with unit tests before wiring the Portapps build to the custom Brave binary.

Do not merge this Portapps PR as a completed portability fix before the `brave-core` implementation and the real two-machine test gate pass.
