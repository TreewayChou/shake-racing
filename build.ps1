# Runs the tests, then cross-compiles the three self-contained binaries:
#   racing.exe (windows/amd64), racing-linux-amd64, racing-linux-arm64
# Usage (from the repo root):  powershell -ExecutionPolicy Bypass -File .\build.ps1
$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

$targets = @(
    @{ GOOS = 'windows'; GOARCH = 'amd64'; Out = 'racing.exe' },
    @{ GOOS = 'linux';   GOARCH = 'amd64'; Out = 'racing-linux-amd64' },
    @{ GOOS = 'linux';   GOARCH = 'arm64'; Out = 'racing-linux-arm64' }
)

# Remember the caller's environment so the script leaves no GOOS/GOARCH behind.
$saved = @{}
foreach ($name in 'CGO_ENABLED', 'GOOS', 'GOARCH') {
    $saved[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

try {
    Write-Host '==> go test ./...'
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "go test failed (exit $LASTEXITCODE)" }

    $env:CGO_ENABLED = '0'
    foreach ($t in $targets) {
        $env:GOOS = $t.GOOS
        $env:GOARCH = $t.GOARCH
        Write-Host "==> $($t.GOOS)/$($t.GOARCH) -> $($t.Out)"
        go build -trimpath -ldflags='-s -w' -o $t.Out .
        if ($LASTEXITCODE -ne 0) { throw "go build $($t.GOOS)/$($t.GOARCH) failed (exit $LASTEXITCODE)" }
    }
}
finally {
    foreach ($name in $saved.Keys) {
        [Environment]::SetEnvironmentVariable($name, $saved[$name], 'Process')
    }
}

foreach ($t in $targets) {
    $size = (Get-Item -LiteralPath $t.Out).Length
    '{0,-20} {1,6:N1} MB' -f $t.Out, ($size / 1MB)
}
