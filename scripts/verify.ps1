# SPDX-License-Identifier: MPL-2.0
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$GoExe,
    [string]$TerraformExe,
    [switch]$SkipVet,
    [string]$VetSkipReason,
    [switch]$GenerateDocs
)

$ErrorActionPreference = 'Stop'
$taskRepo = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskCache = Join-Path $taskRepo '.cache'
$taskWindows = [Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT
$taskSuffix = if ($taskWindows) { '.exe' } else { '' }
$GoExe = (Resolve-Path -LiteralPath $GoExe).Path
if ($TerraformExe) { $TerraformExe = (Resolve-Path -LiteralPath $TerraformExe).Path }
if ($SkipVet -and [string]::IsNullOrWhiteSpace($VetSkipReason)) { throw 'Skipping vet requires -VetSkipReason; a skip is not a pass.' }
if ($GenerateDocs -and !$TerraformExe) { throw '-GenerateDocs requires -TerraformExe.' }
$taskPrevious = @{}
foreach ($taskKey in @('GOTOOLCHAIN','GOCACHE','GOMODCACHE','GOPATH','CGO_ENABLED','IRU_TEST_TERRAFORM')) {
    $taskPrevious[$taskKey] = [Environment]::GetEnvironmentVariable($taskKey, 'Process')
}
Push-Location -LiteralPath $taskRepo
try {
    New-Item -ItemType Directory -Force -Path (Join-Path $taskCache 'bin') | Out-Null
    $env:GOTOOLCHAIN = 'local'
    if (!$env:GOCACHE) { $env:GOCACHE = Join-Path $taskCache 'go-build' }
    if (!$env:GOMODCACHE) { $env:GOMODCACHE = Join-Path $taskCache 'gomod' }
    if (!$env:GOPATH) { $env:GOPATH = Join-Path $taskCache 'gopath' }
    $env:CGO_ENABLED = '0'
    $env:IRU_TEST_TERRAFORM = $TerraformExe
    $taskFmt = Join-Path ([System.IO.Path]::GetDirectoryName($GoExe)) ('gofmt' + $taskSuffix)
    $taskUnformatted = & $taskFmt -l main.go internal
    if ($LASTEXITCODE -ne 0 -or $taskUnformatted) { throw 'Go formatting check failed; run gofmt and review its diff.' }
    & $GoExe mod verify
    if ($LASTEXITCODE -ne 0) { throw 'Module verification failed.' }
    & $GoExe build -trimpath -o (Join-Path $taskCache ('bin/terraform-provider-iru' + $taskSuffix)) .
    if ($LASTEXITCODE -ne 0) { throw 'Build failed.' }
    & $GoExe test -vet=off -count=1 ./...
    if ($LASTEXITCODE -ne 0) { throw 'Unit/protocol/mock CLI tests failed.' }
    if (!$TerraformExe) { Write-Warning 'Terraform CLI tests were not enabled; report them as not run.' }
    if ($SkipVet) { Write-Warning ('Vet NOT RUN: ' + $VetSkipReason) }
    else {
        & $GoExe vet ./...
        if ($LASTEXITCODE -ne 0) { throw 'Vet did not pass.' }
    }
    if ($TerraformExe) {
        & $TerraformExe fmt -check -recursive examples
        if ($LASTEXITCODE -ne 0) { throw 'Terraform example formatting failed.' }
    }
    if ($GenerateDocs) {
        & (Join-Path $PSScriptRoot 'generate-docs.ps1') -GoExe $GoExe -TerraformExe $TerraformExe -Check
    }
    Write-Output 'Requested checks completed. Review warnings; skipped checks are not passes.'
}
finally {
    Pop-Location
    foreach ($taskKey in $taskPrevious.Keys) { [Environment]::SetEnvironmentVariable($taskKey, $taskPrevious[$taskKey], 'Process') }
}
