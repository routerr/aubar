[CmdletBinding()]
param(
    [string]$OutputDir = "dist/native"
)

$ErrorActionPreference = "Stop"

function Invoke-GoBuild {
    param(
        [Parameter(Mandatory = $true)]
        [string]$Name,
        [Parameter(Mandatory = $true)]
        [string]$Package,
        [Parameter(Mandatory = $true)]
        [string]$OutputPath,
        [Parameter(Mandatory = $true)]
        [string]$GoOS,
        [Parameter(Mandatory = $true)]
        [string]$GoArch
    )

    Write-Host "==> building $Name for $GoOS/$GoArch"
    & go build -trimpath -o $OutputPath $Package
    if ($LASTEXITCODE -ne 0) {
        throw "go build failed for $Name"
    }
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    throw "missing required command: go"
}

$repoRoot = Split-Path -Parent $PSCommandPath
Set-Location $repoRoot

$goos = (& go env GOHOSTOS).Trim()
$goarch = (& go env GOHOSTARCH).Trim()
$exeSuffix = if ($goos -eq "windows") { ".exe" } else { "" }

if (-not [System.IO.Path]::IsPathRooted($OutputDir)) {
    $OutputDir = Join-Path $repoRoot $OutputDir
}

$null = New-Item -ItemType Directory -Force -Path $OutputDir

$targets = @(
    @{ Name = "aubar"; Package = "./cmd/aubar" }
    @{ Name = "quota"; Package = "./cmd/quota" }
    @{ Name = "gemini-quota"; Package = "./cmd/gemini-quota" }
)

try {
    $previousGoos = $env:GOOS
    $previousGoarch = $env:GOARCH
    $hadGoos = Test-Path Env:GOOS
    $hadGoarch = Test-Path Env:GOARCH

    $env:GOOS = $goos
    $env:GOARCH = $goarch

    foreach ($target in $targets) {
        $outputPath = Join-Path $OutputDir ($target.Name + $exeSuffix)
        Invoke-GoBuild -Name $target.Name -Package $target.Package -OutputPath $outputPath -GoOS $goos -GoArch $goarch
    }
}
finally {
    if ($hadGoos) {
        $env:GOOS = $previousGoos
    } else {
        Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    }

    if ($hadGoarch) {
        $env:GOARCH = $previousGoarch
    } else {
        Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    }
}

Write-Host "built binaries in $OutputDir"
