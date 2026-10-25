# SPDX-License-Identifier: MPL-2.0
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$GoExe,
    [Parameter(Mandatory = $true)][string]$TerraformExe,
    [switch]$Check
)

$ErrorActionPreference = 'Stop'
$taskRepo = [System.IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskCache = Join-Path $taskRepo '.cache'
$taskWork = Join-Path $taskCache 'docs-work'
$taskBin = Join-Path $taskCache 'bin'
$taskSuffix = if ([Environment]::OSVersion.Platform -eq [PlatformID]::Win32NT) { '.exe' } else { '' }
$GoExe = (Resolve-Path -LiteralPath $GoExe).Path
$TerraformExe = (Resolve-Path -LiteralPath $TerraformExe).Path
$taskUtf8 = [System.Text.UTF8Encoding]::new($false)
$taskPrevious = @{}
foreach ($taskKey in @('GOTOOLCHAIN','GOCACHE','GOMODCACHE','GOPATH','CGO_ENABLED','IRU_API_URL','IRU_API_TOKEN','CHECKPOINT_DISABLE')) {
    $taskPrevious[$taskKey] = [Environment]::GetEnvironmentVariable($taskKey, 'Process')
}
$taskTerraformEnv = @{}
Get-ChildItem Env: | Where-Object { $_.Name -like 'TF_*' } | ForEach-Object { $taskTerraformEnv[$_.Name] = $_.Value }
$taskBefore = @{}
if ($Check) {
    Get-ChildItem -LiteralPath (Join-Path $taskRepo 'docs') -File -Recurse -ErrorAction SilentlyContinue | ForEach-Object {
        $taskBefore[$_.FullName] = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash
    }
}
Push-Location -LiteralPath $taskRepo
try {
    New-Item -ItemType Directory -Force -Path $taskWork,$taskBin | Out-Null
    $env:GOTOOLCHAIN = 'local'
    if (!$env:GOCACHE) { $env:GOCACHE = Join-Path $taskCache 'go-build' }
    if (!$env:GOMODCACHE) { $env:GOMODCACHE = Join-Path $taskCache 'gomod' }
    if (!$env:GOPATH) { $env:GOPATH = Join-Path $taskCache 'gopath' }
    $env:CGO_ENABLED = '0'
    & $GoExe build -trimpath -o (Join-Path $taskBin ('terraform-provider-iru' + $taskSuffix)) .
    if ($LASTEXITCODE -ne 0) { throw 'Documentation provider build failed.' }
    Get-ChildItem Env: | Where-Object { $_.Name -like 'TF_*' } | ForEach-Object { Remove-Item -LiteralPath ('Env:' + $_.Name) }
    $env:TF_IN_AUTOMATION = '1'
    $env:TF_INPUT = '0'
    $env:CHECKPOINT_DISABLE = '1'
    $env:IRU_API_URL = 'https://synthetic.api.iru.com'
    $env:IRU_API_TOKEN = 'synthetic-docs-token'
    $taskCliPath = Join-Path $taskWork 'terraform.tfrc'
    $taskCli = 'provider_installation { dev_overrides { "nicholas-r-king/iru" = "' + $taskBin.Replace('\','/') + '" } }'
    [System.IO.File]::WriteAllText($taskCliPath, $taskCli + "`n", $taskUtf8)
    $env:TF_CLI_CONFIG_FILE = $taskCliPath
    [System.IO.File]::WriteAllText((Join-Path $taskWork 'main.tf'), "terraform {`n required_providers {`n  iru = { source = `"nicholas-r-king/iru`" }`n }`n}`nprovider `"iru`" {}`n", $taskUtf8)
    $taskSchemaText = & $TerraformExe "-chdir=$taskWork" providers schema -json
    if ($LASTEXITCODE -ne 0) { throw 'Provider schema export failed.' }
    $taskRawSchema = Join-Path $taskWork 'schema.json'
    [System.IO.File]::WriteAllText($taskRawSchema, ($taskSchemaText -join "`n") + "`n", $taskUtf8)
    # tfplugindocs v0.25.0 accepts a short name or hashicorp namespace in
    # supplied schemas. Give it a short-name alias without changing the schema.
    $taskSchemaObject = ($taskSchemaText -join "`n") | ConvertFrom-Json
    $taskProviderSchema = $taskSchemaObject.provider_schemas.'registry.terraform.io/nicholas-r-king/iru'
    if (!$taskProviderSchema) { throw 'Expected Iru provider schema is missing.' }
    $taskSchemaObject.provider_schemas = [pscustomobject]@{ iru = $taskProviderSchema }
    $taskSchema = Join-Path $taskWork 'tfplugindocs-schema.json'
    [System.IO.File]::WriteAllText($taskSchema, ($taskSchemaObject | ConvertTo-Json -Depth 100) + "`n", $taskUtf8)
    # Pinned official Go tool, fetched into the module cache; no global install.
    # Supplying a schema skips tfplugindocs' provider build and Terraform download.
    & $GoExe run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 generate --provider-name iru --providers-schema $taskSchema --website-temp-dir (Join-Path $taskCache 'tfplugindocs-temp')
    if ($LASTEXITCODE -ne 0) { throw 'Documentation generation failed.' }
    & $GoExe run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@v0.25.0 validate --provider-name iru --providers-schema $taskSchema
    if ($LASTEXITCODE -ne 0) { throw 'Documentation validation failed.' }
    if ($Check) {
        $taskAfter = @{}
        Get-ChildItem -LiteralPath (Join-Path $taskRepo 'docs') -File -Recurse | ForEach-Object {
            $taskAfter[$_.FullName] = (Get-FileHash -Algorithm SHA256 -LiteralPath $_.FullName).Hash
        }
        if ($taskBefore.Count -ne $taskAfter.Count) { throw 'Generated documentation file set changed; review and commit generated output.' }
        foreach ($taskPath in $taskAfter.Keys) { if ($taskBefore[$taskPath] -ne $taskAfter[$taskPath]) { throw 'Generated documentation changed; review and commit generated output.' } }
    }
}
finally {
    Pop-Location
    Get-ChildItem Env: | Where-Object { $_.Name -like 'TF_*' } | ForEach-Object { Remove-Item -LiteralPath ('Env:' + $_.Name) }
    foreach ($taskKey in $taskTerraformEnv.Keys) { [Environment]::SetEnvironmentVariable($taskKey, $taskTerraformEnv[$taskKey], 'Process') }
    foreach ($taskKey in $taskPrevious.Keys) { [Environment]::SetEnvironmentVariable($taskKey, $taskPrevious[$taskKey], 'Process') }
}
