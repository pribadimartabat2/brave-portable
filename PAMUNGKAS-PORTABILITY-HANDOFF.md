# Brave Portable — PAMUNGKAS Portability Handoff

## START-HERE

- Lifecycle: `EXISTING_PRODUCT_MAINTENANCE`
- Owner goal: Brave tetap native (tanpa VM), mendukung extension Chromium/Chrome Web Store, dan profil/cookie/session dapat dibawa ke Windows lain.
- Current working branch: `pamungkas/portable-session-root-cause`
- Protected base: `master` tidak diubah langsung.
- Release status: `NO-GO` sampai Portapps CI, patched Brave-core build/test, dan cross-machine Owner runtime gate lulus.

## CURRENT-SNAPSHOT

Portapps launcher sudah menggunakan:

- `--user-data-dir=<portable data>`
- `--disable-machine-id`
- `--disable-encryption-win`

`--disable-machine-id` masih mempunyai consumer aktif di Brave modern.
`--disable-encryption-win` masih dideklarasikan sebagai switch Brave, tetapi consumer lamanya sudah hilang setelah migrasi Chromium ke OSCryptAsync.

## ROOT-CAUSE CHAIN

1. Brave portable memakai custom `--user-data-dir`.
2. Chromium App-Bound Encryption tidak menjadi provider enkripsi normal ketika user data dir bukan default.
3. OSCryptAsync masih mempunyai DPAPI provider sebagai fallback.
4. DPAPI provider mengambil `os_crypt.encrypted_key`, lalu membuka profile AES key dengan Windows DPAPI.
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

### KEEP + HARDEN UNTUK EXISTING PROFILE

Reference sederhana mengasumsikan profile AES key dibuat sejak awal dalam mode portable. Existing profile Owner membutuhkan migrasi eksplisit:

1. Jika `--disable-encryption-win` aktif dan payload stored key sudah merupakan raw AES-256 key, gunakan langsung.
2. Jika payload masih DPAPI-wrapped, coba unwrap dengan DPAPI pada Windows asal.
3. Jika berhasil, tulis ulang **key yang sama** dalam format portable sebelum profil dipindahkan ke Windows lain.
4. Jangan membuat key baru jika key lama masih bisa dibuka.
5. Jika legacy key tidak bisa dibuka, fail safely; jangan clear, rotate, truncate, atau replace key secara diam-diam.

Ini adalah hard gate non-regression agar aktivasi mode portable tidak menghancurkan decryptability data lama.

### REJECT

- VM: tidak sesuai Owner karena overhead dan hardware tidak dipakai semaksimal native.
- Hanya `--user-data-dir`: tidak menyelesaikan DPAPI binding.
- Menyalin DPAPI/machine secret dari satu Windows ke Windows lain: ditolak; intrusive, berisiko, dan bukan mekanik portable browser yang bersih.
- Menghidupkan ulang backend OSCrypt sync yang telah dihapus: ditolak; melawan arsitektur Chromium modern.
- Membaca/mengekspor nilai cookie, password, token, atau encryption key untuk diagnostik: ditolak.
- Mengganti existing key dengan random key baru ketika migrasi gagal: ditolak karena dapat membuat data lama tidak dapat didekripsi.

## IMPLEMENTATION TARGET

Smallest safe compliant change berada di `brave-core`, bukan di launcher Portapps saja.

Target patch modern:

1. Pertahankan switch Brave existing `--disable-encryption-win` untuk kompatibilitas dengan Portapps.
2. Tambahkan hook switch itu ke `components/os_crypt/async/browser/os_crypt_win.cc`:
   - fresh profile: jangan DPAPI-wrap profile AES key;
   - portable raw key: baca langsung;
   - existing DPAPI key: unwrap sekali pada Windows asal lalu rewrite portable dengan key yang sama;
   - gagal unwrap: jangan rotate/delete diam-diam.
3. Tambahkan hook kompatibel di `components/os_crypt/async/browser/dpapi_key_provider.cc` agar raw profile AES key portable dapat dipakai oleh provider `v10` yang sudah ada.
4. Jangan menghapus DPAPI/App-Bound provider; data legacy tetap harus punya jalur decrypt sesuai formatnya.
5. Jangan mengubah Brave Shields, extensions, sync, updater, atau UX lain di luar blast radius.

## PORTAPPS BRANCH WORK

Sudah ditambahkan:

- Windows CI terpisah: `go test`, `go vet`, `go build`.
- Privacy-safe profile diagnostic:
  - hanya cek keberadaan Cookie DB, Extensions dir, Local State;
  - hanya cek keberadaan metadata `encrypted_key` / `app_bound_encrypted_key`;
  - tidak membaca isi Cookie DB;
  - tidak mengekspor key value.
- Opt-in launcher diagnostic switch: `--pamungkas-portability-diagnostic`.
  - switch dikonsumsi launcher dan tidak diteruskan ke Brave;
  - browser tidak dibuka dalam mode ini;
  - report ditulis ke `<portable-root>/diagnostics/portable-session.json`.

## ACCEPTANCE GATES

### Static / CI

- Portapps `go test ./...`: PASS required.
- Portapps `go vet ./...`: PASS required.
- Portapps Windows launcher build: PASS required.
- Standard Portapps packaging workflow: PASS required.
- Brave-core affected unit tests/build: PASS required after core patch exists.

### Core migration tests

1. Fresh portable mode menyimpan raw AES-256 profile key tanpa machine-bound DPAPI wrapping.
2. Portable raw key dapat dibaca tanpa Windows DPAPI unwrap.
3. Existing DPAPI-wrapped key dapat di-unlock pada Windows asal dan ditulis ulang menggunakan **raw key yang sama**.
4. Migration failure tidak boleh rotate, clear, truncate, atau replace stored key.
5. Existing `v10` encrypted data tetap dapat didekripsi setelah migrasi.

### Owner runtime cross-machine

Gunakan fresh test profile terlebih dahulu. Existing profile hanya diuji setelah safety copy tersedia.

PC A:

1. Start patched portable Brave dari removable SSD.
2. Install minimal satu Chrome Web Store extension.
3. Login ke non-sensitive test account/test site.
4. Close Brave cleanly.
5. Jalankan `brave-portable.exe --pamungkas-portability-diagnostic` dan simpan report.

PC B / Windows berbeda:

1. Attach SSD yang sama.
2. Start Brave/profile yang sama.
3. Extension harus tetap installed/enabled.
4. Cookie/session harus tetap readable dan test site tetap login kecuali server situs sendiri membatalkan session.
5. Close browser dan jalankan diagnostic mode lagi.
6. Kembali ke PC A untuk membuktikan portability dua arah.

Google/GitHub/ChatGPT baru dipakai sebagai secondary real-world verification karena layanan tersebut dapat meminta verifikasi ulang berdasarkan IP/risk dari sisi server.

## WHAT-NEXT

Required repository untuk root fix sebenarnya: writable fork `brave/brave-core` pada akun Owner.

Target: `pribadimartabat2/brave-core`.

Setelah tersedia:

1. buat branch `pamungkas/portable-oscrypt-async`;
2. tulis failing Brave-core tests untuk fresh portable key + existing DPAPI-key migration;
3. adapt proven modern async mechanism dengan switch Brave existing;
4. jalankan affected Windows tests/build;
5. hasilkan custom Brave test binary;
6. arahkan Portapps test pipeline ke custom binary hanya pada governed test branch;
7. jalankan PC A -> PC B -> PC A Owner gate;
8. baru pertimbangkan merge/release;
9. opsional: upstream focused regression fix agar jangka panjang Portapps dapat kembali memakai official Brave binary.
