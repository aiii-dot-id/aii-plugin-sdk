# Authenticate SDK release downloads

Use the same reviewed source revision for the SDK binaries, source archive and
verifier. These steps prepare and authenticate files; publishing is separate.
Signing requires the release owner's existing signing configuration.

## 1. Freeze the downloads

Put the finished archives in one fresh directory. Include
`scripts/release/verify-sdk-release.py` as `verify-sdk-release.py` **before**
creating `SHA256SUMS`. Check the verifier's source/hash independently before
executing it. Do not include `SHA256SUMS` or `SHA256SUMS.cat` in the manifest.
Use unique simple filenames: letters, digits, dots, underscores and hyphens.

In Bash, from that directory (set the exact archive names for this release):

```bash
set -euo pipefail
assets=(aii-plugin-sdk-source.tar.gz aiisdk-linux-amd64.tar.gz
        aiisdk-linux-arm64.tar.gz aiisdk-macos-arm64.tar.gz
        aiisdk-windows-amd64.zip verify-sdk-release.py)
test ! -e SHA256SUMS
(set -o noclobber; sha256sum -- "${assets[@]}" > SHA256SUMS)
sha256sum -c SHA256SUMS
```

No asset, including the verifier, may change after this point. If one changes,
prepare a new manifest and obtain a new signature; never reuse the old catalog.

## 2. Authenticate the exact manifest on Windows

Copy the frozen files to a fresh Windows staging directory. In PowerShell,
provide full paths to the existing Windows SDK tools and approved signer
configuration. The metadata JSON is the signing owner's existing configuration;
do not create a new account, key or credential store for this procedure.

```powershell
& {
$ErrorActionPreference = 'Stop'
$stage = (Resolve-Path (Read-Host 'Staging directory')).Path
$makecat = (Resolve-Path (Read-Host 'MakeCat.exe')).Path
$signtool = (Resolve-Path (Read-Host 'SignTool.exe')).Path
$dlib = (Resolve-Path (Read-Host 'Approved signing Dlib DLL')).Path
$metadata = (Resolve-Path (Read-Host 'Existing signing metadata JSON')).Path
$signer = (Read-Host 'Independently approved signer certificate SHA-256').ToLowerInvariant()
if ($signer -notmatch '^[0-9a-f]{64}$') { throw 'Invalid signer fingerprint' }
if ($stage -match '[^\x20-\x7e]') { throw 'Use a printable ASCII staging path for MakeCat' }
Set-Location -LiteralPath $stage
if (!(Test-Path -LiteralPath 'SHA256SUMS' -PathType Leaf)) { throw 'Missing SHA256SUMS' }
New-Item -ItemType Directory -Path 'catalog-signing' -ErrorAction Stop | Out-Null
$work = (Resolve-Path 'catalog-signing').Path
Start-Transcript -LiteralPath "$work\signing-transcript.txt" -NoClobber | Out-Null
try {
$before = (Get-FileHash -LiteralPath 'SHA256SUMS' -Algorithm SHA256).Hash
$cdf = @"
[CatalogHeader]
Name=SHA256SUMS.cat
ResultDir=$work
PublicVersion=0x0000001
EncodingType=0x00010001
CatalogVersion=2
HashAlgorithms=SHA256
[CatalogFiles]
<HASH>SHA256SUMS=$stage\SHA256SUMS
<HASH>SHA256SUMSATTR1=0x10010001:Filename:SHA256SUMS
"@
[IO.File]::WriteAllText("$work\checksums.cdf", $cdf + "`r`n", [Text.Encoding]::ASCII)
& $makecat -v -r "$work\checksums.cdf"
if ($LASTEXITCODE -ne 0) { throw 'MakeCat failed' }
$catalog = "$work\SHA256SUMS.cat"
& $signtool sign /v /fd SHA256 /tr http://timestamp.acs.microsoft.com /td SHA256 /dlib $dlib /dmdf $metadata $catalog
if ($LASTEXITCODE -ne 0) { throw 'Signing failed; retain the log and consult the signing owner before retrying' }
$signature = Get-AuthenticodeSignature -LiteralPath $catalog
if ($signature.Status -ne 'Valid' -or !$signature.TimeStamperCertificate) { throw 'Signature or timestamp is invalid' }
$sha = [Security.Cryptography.SHA256]::Create()
try { $actual = ([BitConverter]::ToString($sha.ComputeHash($signature.SignerCertificate.RawData))).Replace('-', '').ToLowerInvariant() }
finally { $sha.Dispose() }
if ($actual -ne $signer) { throw 'Wrong signing certificate' }
& $signtool verify /pa /v /c $catalog "$stage\SHA256SUMS"
if ($LASTEXITCODE -ne 0) { throw 'Signature or manifest membership failed' }
$changed = "$work\changed"
New-Item -ItemType Directory -Path $changed | Out-Null
$bytes = [IO.File]::ReadAllBytes("$stage\SHA256SUMS")
[IO.File]::WriteAllBytes("$changed\SHA256SUMS", ($bytes + [byte]10))
& $signtool verify /pa /c $catalog "$changed\SHA256SUMS"
if ($LASTEXITCODE -eq 0) { throw 'Tampered manifest was accepted' }
if ((Get-FileHash -LiteralPath 'SHA256SUMS' -Algorithm SHA256).Hash -ne $before) { throw 'Manifest changed during signing' }
if (Test-Path -LiteralPath 'SHA256SUMS.cat') { throw 'Refusing to overwrite an existing catalog' }
[IO.File]::Copy($catalog, "$stage\SHA256SUMS.cat", $false)
Get-FileHash -LiteralPath 'SHA256SUMS', 'SHA256SUMS.cat' -Algorithm SHA256
} finally { Stop-Transcript | Out-Null }
}
```

Keep `catalog-signing/` and the command transcript with the retained signing
evidence, not in the uploaded assets. A failure stops the procedure; it is not
permission to skip verification or sign again automatically.

## 3. Verify before upload and after download

Install Python 3.11+, OpenSSL and osslsigncode with timestamp, revocation and leaf
fingerprint verification support. Obtain the **Microsoft Identity Verification
Root 2020** PEM independently from Microsoft's PKI repository and the approved
signer SHA-256 fingerprint independently from the signing owner. The verifier
pins the root certificate, checks the company signer, timestamp and revocation,
then authenticates the manifest's exact filename and SHA-256 catalog membership.
It finally checks every requested artifact. A plain checksum check is not
authentication.

```bash
read -r -p 'Root certificate PEM, full path: ' ca
read -r -p 'Approved signer certificate SHA-256: ' signer
python3 ./verify-sdk-release.py --ca-file "$ca" --signer-sha256 "$signer" \
  --manifest SHA256SUMS --catalog SHA256SUMS.cat "${assets[@]}"
```

Upload exactly the authenticated artifacts, `SHA256SUMS` and `SHA256SUMS.cat`.
In a fresh download directory, compare the complete filename inventory with
the approved inventory, recheck the verifier's independently approved source
hash, and repeat this command for **all** downloaded artifacts. Draft staging,
source provenance, anonymous delivery, publication approval and Go module
resolution are separate release checks; none is established by this command.

For verifier unit/rejection tests, run `make release-test` from the SDK root.
