# Build adalmao on Windows without make.
param([string]$Version = "0.1.0")
$ErrorActionPreference = "Stop"
New-Item -ItemType Directory -Force dist | Out-Null
$ld = "-s -w -X github.com/adalmao/adalmao/internal/cli.Version=$Version"
go vet ./...
go test ./...
go build -ldflags $ld -o dist/adalmao.exe ./cmd/adalmao
Write-Host "Built dist/adalmao.exe"
