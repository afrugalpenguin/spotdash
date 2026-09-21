# Signing the shell APK

The release workflow signs `spotdash-shell-<tag>.apk` with one keystore held in GitHub Actions secrets. Android only updates an installed app when the new APK has the same signing key. Lose the keystore and every user has to uninstall and reinstall the shell, which is also their launcher. The keystore is never committed: `.gitignore` covers `*.keystore`, `*.jks`, `*.p12` and `keystore.properties`.

## Generate the keystore

```
keytool -genkeypair -v -keystore spotdash-release.keystore -alias spotdash -keyalg RSA -keysize 4096 -validity 10000
```

Use one password for the store and the key. A PKCS12 keystore (the default) ignores a separate key password, and signing fails if the two secrets differ.

Record the certificate digest, then keep it in the table at the end of this page:

```
keytool -list -v -keystore spotdash-release.keystore -alias spotdash
```

## Set the secrets

The keystore goes in as base64. Run these from the directory holding the keystore.

```
# PowerShell
[Convert]::ToBase64String([IO.File]::ReadAllBytes("spotdash-release.keystore")) | gh secret set SPOTDASH_KEYSTORE_B64

# bash
base64 -w0 spotdash-release.keystore | gh secret set SPOTDASH_KEYSTORE_B64
```

`gh secret set` prompts for the other three, or takes them on stdin:

| Secret                       | Value                          |
|------------------------------|--------------------------------|
| `SPOTDASH_KEYSTORE_B64`      | The keystore, base64 encoded.  |
| `SPOTDASH_KEYSTORE_PASSWORD` | The store password.            |
| `SPOTDASH_KEY_ALIAS`         | `spotdash`.                    |
| `SPOTDASH_KEY_PASSWORD`      | The key password (same as the store password). |

If any one is empty, the `apk` job fails before building and nothing is published.

## Back it up

Keep the keystore file and both passwords in two places off this machine, such as a password manager and an encrypted drive. GitHub secrets cannot be read back, so they are not a backup.

## Build locally

`assembleRelease` reads `SPOTDASH_KEYSTORE_PATH`, `SPOTDASH_KEYSTORE_PASSWORD`, `SPOTDASH_KEY_ALIAS` and `SPOTDASH_KEY_PASSWORD` from the environment and fails naming any that is unset. `assembleDebug` needs none of them. The workflow passes `-PversionName` and `-PversionCode` from the tag through `.github/scripts/apk-version.sh`.

## Check a release APK

```
apksigner verify --print-certs spotdash-shell-v0.1.0-rc1.apk
aapt2 dump badging spotdash-shell-v0.1.0-rc1.apk
```

The certificate SHA-256 digest must match the one below. `badging` should show the tag as `versionName` and no `debuggable` attribute.

## Certificate digest

`apksigner` prints the digest in lowercase without colons. `keytool` prints the same value in uppercase with colons.

| Key      | SHA-256                                                          |
|----------|------------------------------------------------------------------|
| spotdash | `3fd250efb85e9126e65540f45b99f3a9b1715bed949be8e30d50b05f58097b3f` |
