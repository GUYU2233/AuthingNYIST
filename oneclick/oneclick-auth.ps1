# One-click NYIST authentication. Compatible with Windows PowerShell 5.1.
param(
    [string]$Config = "$PSScriptRoot\config.json",
    [switch]$KeepOnline,
    [switch]$Deauth,
    [switch]$Once
)
$ErrorActionPreference = 'Stop'
$exe = Join-Path (Split-Path $PSScriptRoot -Parent) 'auth-nyist.exe'
$tempConfig = $null
try {
    if (-not (Test-Path -LiteralPath $exe)) { throw 'Build first: go build -o auth-nyist.exe ./cli' }
    $cfg = Get-Content -LiteralPath $Config -Raw -Encoding UTF8 | ConvertFrom-Json
    if (-not $cfg.username -or -not $cfg.password) { throw 'Config requires username and password.' }
    # PowerShell variable names are case-insensitive: do not assign a bool to $KeepOnline.
    [bool]$enableKeepAlive = $KeepOnline.IsPresent -or ("$($cfg.keepOnline)" -match '^(true|1|yes|on)$')
    if ($Once -or $Deauth) { $enableKeepAlive = $false }
    $normalized = @{
        username = [string]$cfg.username
        password = [string]$cfg.password
        host = $(if ($cfg.host) { [string]$cfg.host } else { 'auth.nyist.edu.cn' })
        acId = $(if ($cfg.acId) { [string]$cfg.acId } else { '1' })
        insecure = [bool]("$($cfg.insecure)" -match '^(true|1|yes|on)$')
        keepOnline = [bool]$enableKeepAlive
    }
    $tempConfig = Join-Path $PSScriptRoot ('.auth-session-' + [guid]::NewGuid().ToString('N') + '.json')
    [System.IO.File]::WriteAllText($tempConfig, ($normalized | ConvertTo-Json -Compress), (New-Object System.Text.UTF8Encoding($false)))
    $command = if ($Deauth) { 'deauth' } else { 'auth' }
    $cliArgs = @('--config-file', $tempConfig, $command)
    Write-Host "NYIST authentication: $($cfg.username) ($command)"
    & $exe @cliArgs
    $resultCode = $LASTEXITCODE
    if ($resultCode -ne 0) { Write-Host "Authentication failed (exit $resultCode)." }
} catch {
    Write-Error $_ -ErrorAction Continue
    $resultCode = 1
} finally {
    if ($tempConfig -and (Test-Path -LiteralPath $tempConfig)) {
        # Delete only the exact unique session file created above.
        Remove-Item -LiteralPath $tempConfig -Force
    }
}
exit $resultCode
