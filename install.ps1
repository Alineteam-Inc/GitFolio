# GitFolio installer for Windows — https://github.com/Alineteam-Inc/GitFolio
#
#   irm https://raw.githubusercontent.com/Alineteam-Inc/GitFolio/main/install.ps1 | iex
#
# Downloads the release for this CPU over HTTPS, verifies its SHA-256 checksum against the release's
# checksums.txt and refuses to install on any mismatch. Installs to %LOCALAPPDATA%\Programs\GitFolio and
# adds that folder to your user PATH.
#
# Options (environment variables):
#   GITFOLIO_VERSION      release tag to install, e.g. v0.1.0 (default: latest)
#   GITFOLIO_INSTALL_DIR  where to put gitfolio.exe (default: %LOCALAPPDATA%\Programs\GitFolio)
#   GITFOLIO_BASE_URL     download location, for mirrors or testing (default: GitHub Releases)

function Install-GitFolio {
    $ErrorActionPreference = 'Stop'
    $repo = 'Alineteam-Inc/GitFolio'
    $cpu = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
    $arch = switch ($cpu) {
        'AMD64' { 'amd64' }
        'ARM64' { 'arm64' }
        default { throw "gitfolio install: unsupported CPU: $cpu" }
    }
    $base = if ($env:GITFOLIO_BASE_URL) { $env:GITFOLIO_BASE_URL }
            elseif ($env:GITFOLIO_VERSION) { "https://github.com/$repo/releases/download/$env:GITFOLIO_VERSION" }
            else { "https://github.com/$repo/releases/latest/download" }
    if ($base -notmatch '^(https|file)://') { throw "gitfolio install: refusing to download over an insecure connection: $base" }
    $base = $base.TrimEnd('/')

    $archive = "gitfolio_windows_$arch.zip"
    $tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "Downloading $archive ..."
        Get-GitFolioFile "$base/$archive" (Join-Path $tmp $archive)
        Get-GitFolioFile "$base/checksums.txt" (Join-Path $tmp 'checksums.txt')

        $expected = Get-Content (Join-Path $tmp 'checksums.txt') |
            ForEach-Object { $f = $_ -split '\s+'; if ($f[1] -eq $archive) { $f[0] } } | Select-Object -First 1
        if (-not $expected) { throw "gitfolio install: $archive is not listed in checksums.txt; not installing" }
        $actual = (Get-FileHash (Join-Path $tmp $archive) -Algorithm SHA256).Hash.ToLower()
        if ($expected -ne $actual) { throw "gitfolio install: checksum mismatch for $archive (expected $expected, got $actual); not installing" }
        Write-Host 'Checksum verified (SHA-256).'

        Expand-Archive -Path (Join-Path $tmp $archive) -DestinationPath (Join-Path $tmp 'x')
        $dir = if ($env:GITFOLIO_INSTALL_DIR) { $env:GITFOLIO_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\GitFolio' }
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        Copy-Item (Join-Path $tmp 'x\gitfolio.exe') (Join-Path $dir 'gitfolio.exe') -Force
        Write-Host "Installed to $dir\gitfolio.exe"

        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        if (-not (($userPath -split ';') -contains $dir)) {
            $newPath = if ($userPath) { $userPath.TrimEnd(';') + ";$dir" } else { $dir }
            [Environment]::SetEnvironmentVariable('Path', $newPath, 'User')
            Write-Host "Added $dir to your user PATH. Open a new terminal to use gitfolio."
        }
        Write-Host ''
        Write-Host 'Next: run  gitfolio init  to choose the repositories to collect.'
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}

function Get-GitFolioFile([string]$url, [string]$out) {
    if ($url -like 'file://*') {
        Copy-Item ([Uri]$url).LocalPath $out
    } else {
        [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12
        Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $out
    }
}

# Everything runs from here, so a download cut short in `irm | iex` executes nothing.
Install-GitFolio
