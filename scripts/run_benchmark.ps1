# MS2M Benchmark Runner PowerShell Script
Write-Host "Running MS2M Kubernetes Microservice Migration Benchmark..." -ForegroundColor Cyan

$env:GOCACHE = "$PSScriptRoot\..\.gocache"
go run "$PSScriptRoot\benchmark.go"

if ($LASTEXITCODE -eq 0) {
    Write-Host "`nBenchmark completed successfully! Results written to benchmark_results.csv and benchmark_results.json" -ForegroundColor Green
} else {
    Write-Host "`nBenchmark failed with exit code $LASTEXITCODE" -ForegroundColor Red
}
