# Build release binaries without installing or enabling anything.
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
$oldOS=$env:GOOS; $oldArch=$env:GOARCH; $oldCGO=$env:CGO_ENABLED
try {
    $env:CGO_ENABLED='0'; $env:GOOS='windows'; $env:GOARCH='amd64'
    go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -manifest cmd/nyist/app.manifest -ico cmd/nyist/app.ico -o cmd/nyist/rsrc_windows_amd64.syso
    if($LASTEXITCODE -ne 0){throw 'Resource build failed'}
    go test ./...
    if($LASTEXITCODE -ne 0){throw 'Tests failed'}
    go build -trimpath -ldflags '-H windowsgui -s -w' -o dist/nyist-connect.exe ./cmd/nyist
    if($LASTEXITCODE -ne 0){throw 'Windows build failed'}
    $env:GOOS='linux'
    foreach($arch in @('amd64','arm64')) {
        $env:GOARCH=$arch
        go build -trimpath -ldflags '-s -w' -o "dist/nyist-connect-linux-$arch" ./cmd/nyist
        if($LASTEXITCODE -ne 0){throw "Linux $arch build failed"}
    }
    Write-Host 'Built Windows amd64 and Linux amd64/arm64 in dist/.'
} finally {
    $env:GOOS=$oldOS; $env:GOARCH=$oldArch; $env:CGO_ENABLED=$oldCGO
    Pop-Location
}
