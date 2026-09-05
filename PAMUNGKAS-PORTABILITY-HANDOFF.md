# Brave Portable — PAMUNGKAS Portability Handoff

## START-HERE

- Lifecycle: `EXISTING_PRODUCT_MAINTENANCE`
- Owner goal: Brave tetap native (tanpa VM), mendukung extension Chromium/Chrome Web Store, dan profil/cookie/session dapat dibawa ke Windows lain.
- Current working branch: `pamungkas/portable-session-root-cause`
- Protected base: `master` tidak diubah langsung.
- Release status: `NO-GO` sampai build + cross-machine Owner runtime gate lulus.

## CURRENT-SNAPSHOT

Portapps launcher sudah menggunakan:

- `--user-data-dir=<portable data>`
- `--disable-machine-id`
- `--disable-encryption-win`

`--disable-machine-id` masih mempunyai consumer aktif di Brave modern.
`--disable-encryption-win` masih dideklarasikan sebagai switch Brave, tetapi consumer lamanya sudah hilang setelah migrasi Chromium ke OSCryptAsync.

## ROOT-CAUSE CHAIN

1. Brave portable memakai custom `--user-data-dir`.
2. Chromium App-Bound Encryption menolak penggunaan normal ketika user data dir bukan default.
3. OSCryptAsync lalu masih mempunyai DPAPI provider sebagai fallback.
4. DPAPI provider mengambil `os_crypt.encrypted_key`, lalu membuka key dengan Windows DPAPI.
5. DPAPI tersebut terikat konteks Windows, sehingga salinan profil ke PC/Windows lain tidak dapat membuka key yang sama.
6. Pada 2019 Brave menambahkan `--disable-encryption-win` khusus portability. Implementasinya membypass DPAPI pada backend OSCrypt lama.
7. Pada 2023 OSCrypt dipindahkan ke `sync/` sebagai tahap migrasi ke async.
8. Pada 22 Mei 2026 Brave menghapus seluruh `components/os_crypt/sync`; diff tersebut juga menghapus consumer `kDisableEncryptionWin`.
9. Konstanta switch tetap ada dan Portapps masih mengirimnya, sehingga launcher terlihat benar tetapi mekanik enkripsi portable yang dahulu dibutuhkan sudah tidak aktif.

## REFERENCE EVIDENCE

### KEEP

Ungoogled Chromium Windows modern sudah mengadaptasi mekanik portability ke OSCryptAsync:

- `components/os_crypt/async/browser/os_crypt_win.cc`: bypass DPAPI wrapping/unwrapping saat mode portable aktif.
- `components/os_crypt/async/browser/dpapi_key_provider.cc`: raw profile AES key dapat dibaca lintas mesin saat mode portable aktif.
- Cookie/password tetap memakai key OSCrypt AES; yang tidak diikat ke mesin adalah pembungkus key profilnya.

Mekanik ini lebih aman dan lebih kecil blast radius daripada mengembalikan cookie/password ke plaintext.

### REJECT

- VM: tidak sesuai Owner karena overhead dan hardware tidak dipakai semaksimal native.
- Hanya `--user-data-dir`: tidak menyelesaikan DPAPI binding.
- Menyalin DPAPI/machine secret dari satu Windows ke Windows lain: ditolak; intrusive, berisiko, dan bukan mekanik portable browser yang bersih.
- Menghidupkan ulang backend OSCrypt sync yang telah dihapus: ditolak; melawan arsitektur Chromium modern.
- Membaca/mengekspor nilai cookie, password, token, atau encryption key untuk diagnostik: ditolak.

## IMPLEMENTATION TARGET

Smallest safe compliant change berada di `brave-core`, bukan di launcher Portapps saja.

Target patch modern:

1. Pertahankan switch Brave existing `--disable-encryption-win` untuk kompatibilitas dengan Portapps.
2. Tambahkan hook switch itu ke `components/os_crypt/async/browser/os_crypt_win.cc`:
   - saat membuat/storing profile AES key, jangan DPAPI-wrap key;
   - saat membaca profile AES key, jangan DPAPI-unwrap key.
3. Tambahkan hook kompatibel di `components/os_crypt/async/browser/dpapi_key_provider.cc` agar key profile portable dapat dipakai oleh provider `v10` yang sudah ada.
4. Jangan menghapus DPAPI/App-Bound provider; data lama harus tetap punya jalur decrypt apabila key lokal masih tersedia.
5. Jangan mengubah Brave Shields, extensions, sync, updater, atau UX lain di luar blast radius.

## PORTAPPS BRANCH WORK

Sudah ditambahkan:

- Windows CI terpisah: `go test`, `go vet`, `go build`.
- Privacy-safe profile diagnostic:
  - hanya cek keberadaan Cookie DB, Extensions dir, Local State;
  - hanya cek keberadaan metadata `encrypted_key` / `app_bound_encrypted_key`;
  - tidak membaca isi Cookie DB;
  - tidak mengekspor key value.

## ACCEPTANCE GATES

### Static / CI

- Portapps `go test ./...`: PASS required.
- Portapps `go vet ./...`: PASS required.
- Portapps Windows launcher build: PASS required.
- Standard Portapps packaging workflow: PASS required.
- Brave-core affected unit tests/build: PASS required after core patch exists.

### Owner runtime cross-machine

Use a fresh test profile created from the patched build.

PC A:

1. Start portable Brave from removable SSD.
2. Install at least one Chrome Web Store extension.
3. Login to non-sensitive test accounts / test sites first.
4. Close Brave cleanly.

PC B / different Windows:

1. Attach the same SSD.
2. Start the same portable Brave/profile.
3. Extension must remain installed/enabled.
4. Cookie/session test must remain readable and site should remain logged in unless the remote site itself invalidates the session.
5. Return to PC A and repeat to prove bidirectional portability.

Only after this passes should Google/GitHub/ChatGPT be used as secondary real-world verification, because those services may independently challenge a session due to IP/risk changes.

## WHAT-NEXT

Required repository for the actual root fix: a writable fork of `brave/brave-core` under the Owner account.

Recommended fork name: `pribadimartabat2/brave-core`.

Once writable:

1. create isolated branch `pamungkas/portable-oscrypt-async`;
2. add failing Brave-core tests for `--disable-encryption-win` on OSCryptAsync;
3. adapt the proven modern async mechanism with Brave's existing switch name;
4. run Windows build/tests;
5. point the Portapps build pipeline at the custom Brave binary only on a test branch;
6. run PC A -> PC B -> PC A Owner gate;
7. only then consider merge/release.
