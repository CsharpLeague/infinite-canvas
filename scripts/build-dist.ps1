param(
    [ValidateSet("amd64", "arm64")]
    [string]$Arch = "amd64"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$deploymentRoot = [IO.Path]::GetFullPath((Join-Path $root "deployment"))
$dist = [IO.Path]::GetFullPath((Join-Path $deploymentRoot "infinite-canvas-dist"))
$go = Join-Path $root ".tools\go\bin\go.exe"

if (-not $dist.StartsWith($deploymentRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
    throw "Dist path must stay inside the deployment directory"
}

if (-not (Test-Path $go)) {
    $go = (Get-Command go -ErrorAction Stop).Source
}

Remove-Item -LiteralPath $dist -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path (Join-Path $dist "bin") | Out-Null

$previousGoos = $env:GOOS
$previousGoarch = $env:GOARCH
$previousCgo = $env:CGO_ENABLED
$previousGocache = $env:GOCACHE
$env:GOOS = "linux"
$env:GOARCH = $Arch
$env:CGO_ENABLED = "0"
$env:GOCACHE = Join-Path $root ".cache\go-build"
try {
    & $go build -trimpath -ldflags="-s -w" -o (Join-Path $dist "bin\infinite-canvas-api") .
    if ($LASTEXITCODE -ne 0) { throw "Go backend build failed" }
} finally {
    $env:GOOS = $previousGoos
    $env:GOARCH = $previousGoarch
    $env:CGO_ENABLED = $previousCgo
    $env:GOCACHE = $previousGocache
}

Push-Location (Join-Path $root "web")
try {
    $bun = Get-Command bun -ErrorAction SilentlyContinue
    if ($bun) {
        & $bun.Source install --frozen-lockfile
        if ($LASTEXITCODE -ne 0) { throw "Frontend dependency installation failed" }
        & $bun.Source run build
    } elseif (Test-Path "node_modules") {
        & "C:\Program Files\nodejs\npm.cmd" run build
    } else {
        throw "Bun was not found and web/node_modules does not exist"
    }
    if ($LASTEXITCODE -ne 0) { throw "Frontend build failed" }
} finally {
    Pop-Location
}

$standalone = Join-Path $root "web\.next\standalone"
$webDist = Join-Path $dist "web\.next\standalone"
Copy-Item -LiteralPath $standalone -Destination $webDist -Recurse -Force
New-Item -ItemType Directory -Force -Path (Join-Path $webDist ".next") | Out-Null
Copy-Item -LiteralPath (Join-Path $root "web\public") -Destination (Join-Path $webDist "public") -Recurse -Force
Copy-Item -LiteralPath (Join-Path $root "web\.next\static") -Destination (Join-Path $webDist ".next\static") -Recurse -Force
Copy-Item -LiteralPath (Join-Path $root "ecosystem.config.cjs") -Destination $dist -Force
Copy-Item -LiteralPath (Join-Path $root "scripts\start-dist.sh") -Destination (Join-Path $dist "start.sh") -Force

$archive = Join-Path $deploymentRoot "infinite-canvas-linux-$Arch.tar.gz"
Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue
& tar.exe -czf $archive -C $dist .
if ($LASTEXITCODE -ne 0) { throw "Dist archive creation failed" }

Write-Host "Linux $Arch dist created: $dist"
Write-Host "Archive created: $archive"
Write-Host "Upload all files to the existing server directory, keep its .env, then run: chmod +x start.sh && ./start.sh"
