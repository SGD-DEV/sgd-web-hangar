# build-installer.ps1
# One-shot build script for Devour. Produces:
#   build/bin/devour.exe
#   build/bin/DevourSetup.exe   (NSIS installer - share this with the team)
#
# Bootstraps Go and Wails CLI into .build-tools/ if they're missing, so you
# don't need anything pre-installed except: Node.js (npm) and PowerShell.
# NSIS (makensis.exe) is bundled by Wails when you pass -nsis.
#
# Usage (from the repo root):
#   powershell -ExecutionPolicy Bypass -File .\build-installer.ps1

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$tools = Join-Path $root ".build-tools"
New-Item -ItemType Directory -Force -Path $tools | Out-Null

function Have-Cmd($name) {
    return ($null -ne (Get-Command $name -ErrorAction SilentlyContinue))
}

# --- Go ----------------------------------------------------------------------
$goVersion = "1.24.1"
$goZip = "go$goVersion.windows-amd64.zip"
$goUrl = "https://go.dev/dl/$goZip"
$goRoot = Join-Path $tools "go"
$goBin  = Join-Path $goRoot "bin\go.exe"

if (Have-Cmd "go") {
    $goBin = (Get-Command go).Path
    $goRoot = Split-Path -Parent (Split-Path -Parent $goBin)
    Write-Host "Using existing go at $goBin"
} elseif (-not (Test-Path $goBin)) {
    $zipPath = Join-Path $tools $goZip
    Write-Host "Downloading Go $goVersion (~190 MB) from go.dev..."
    Invoke-WebRequest -Uri $goUrl -OutFile $zipPath -UseBasicParsing
    Write-Host "Extracting Go..."
    Expand-Archive -Path $zipPath -DestinationPath $tools -Force
    Remove-Item $zipPath
    if (-not (Test-Path $goBin)) {
        throw "Go extraction failed: $goBin not found after extracting"
    }
} else {
    Write-Host "Go already bootstrapped at $goBin"
}

$env:GOROOT = $goRoot
$env:PATH = "$goRoot\bin;$env:PATH"

# Per-build GOPATH so we don't pollute the user's profile
$gopath = Join-Path $tools "gopath"
New-Item -ItemType Directory -Force -Path $gopath | Out-Null
$env:GOPATH = $gopath
$env:PATH = "$gopath\bin;$env:PATH"

Write-Host ("go version: " + (& go version))

# --- Wails CLI ---------------------------------------------------------------
$wailsBin = Join-Path $gopath "bin\wails.exe"
if (-not (Test-Path $wailsBin)) {
    Write-Host "Installing Wails CLI..."
    & go install "github.com/wailsapp/wails/v2/cmd/wails@v2.11.0"
    if (-not (Test-Path $wailsBin)) {
        throw "Wails install failed: $wailsBin not found"
    }
} else {
    Write-Host "Wails already bootstrapped at $wailsBin"
}
Write-Host ("wails version: " + (& $wailsBin version))

# --- Frontend deps -----------------------------------------------------------
$frontend = Join-Path $root "frontend"
if (-not (Test-Path (Join-Path $frontend "node_modules"))) {
    Write-Host "Installing frontend dependencies..."
    Push-Location $frontend
    try {
        & npm install
        if ($LASTEXITCODE -ne 0) { throw "npm install failed" }
    } finally {
        Pop-Location
    }
} else {
    Write-Host "frontend/node_modules present, skipping npm install"
}

# --- go mod tidy (drops the // indirect on x/sys now that we import it) -----
Write-Host "Tidying Go modules..."
Push-Location $root
try {
    & go mod tidy
    if ($LASTEXITCODE -ne 0) { throw "go mod tidy failed" }
} finally {
    Pop-Location
}

# --- NSIS (makensis) on PATH ------------------------------------------------
# Wails calls `makensis` to produce DevourSetup.exe. winget-installed NSIS
# updates the registry PATH but NOT the current process - this loop checks
# common install paths and prepends to $env:PATH so Wails finds it without
# needing a fresh PowerShell session.
if (-not (Have-Cmd "makensis")) {
    $nsisPaths = @(
        (Join-Path $env:ProgramFiles "NSIS"),
        "${env:ProgramFiles(x86)}\NSIS"
    )
    foreach ($p in $nsisPaths) {
        if ($p -and (Test-Path (Join-Path $p "makensis.exe"))) {
            Write-Host "Adding NSIS to PATH: $p"
            $env:PATH = "$p;$env:PATH"
            break
        }
    }
    if (-not (Have-Cmd "makensis")) {
        Write-Warning "makensis not found - installer step will be skipped. Install NSIS via 'winget install NSIS.NSIS'."
    }
}

# --- Build -------------------------------------------------------------------
Write-Host "Building Devour and NSIS installer (this may take 1-2 min)..."
Push-Location $root
try {
    & $wailsBin build -platform windows/amd64 -nsis -clean
    if ($LASTEXITCODE -ne 0) { throw "wails build failed" }
} finally {
    Pop-Location
}

# --- Verify ------------------------------------------------------------------
# Wails -nsis names the installer "<projectname>-<arch>-installer.exe" based
# on wails.json's "outputfilename" / "name". After the Devour -> Hangar
# rename the artifacts are hangar.exe and hangar-amd64-installer.exe.
$exe = Join-Path $root "build\bin\hangar.exe"
$installer = Join-Path $root "build\bin\hangar-amd64-installer.exe"
# Fall back to the old names so this script keeps working on commits before
# the rename (useful for git bisect, or if you rebuild from an older branch).
if (-not (Test-Path $exe)) { $exe = Join-Path $root "build\bin\devour.exe" }
if (-not (Test-Path $installer)) { $installer = Join-Path $root "build\bin\devour-amd64-installer.exe" }

Write-Host ""
Write-Host "=========================================="
Write-Host "Build complete."
if (Test-Path $exe) {
    $exeSize = [math]::Round((Get-Item $exe).Length / 1MB, 1)
    Write-Host ("  exe:       " + $exe + " (" + $exeSize + " MB)")
}
if (Test-Path $installer) {
    $instSize = [math]::Round((Get-Item $installer).Length / 1MB, 1)
    Write-Host ("  installer: " + $installer + " (" + $instSize + " MB)")
} else {
    Write-Warning "Installer NOT found at $installer - check the wails build output above for NSIS errors."
}
Write-Host "=========================================="
Write-Host ""
if (Test-Path $installer) {
    Write-Host "Share $installer with your team."
} else {
    Write-Host "Share $exe with your team (installer build failed)."
}
