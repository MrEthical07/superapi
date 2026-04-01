param(
    [Parameter(Mandatory = $false)]
    [int]$Count = 500,
    [Parameter(Mandatory = $false)]
    [string]$Prefix = "loadtest",
    [Parameter(Mandatory = $false)]
    [string]$Domain = "example.com",
    [Parameter(Mandatory = $false)]
    [string]$Password = "LoadTest123!",
    [Parameter(Mandatory = $false)]
    [string]$Role = "user",
    [Parameter(Mandatory = $false)]
    [string]$AuthMode = "strict",
    [Parameter(Mandatory = $false)]
    [string]$PostgresUrl = "postgres://superapi:superapi@127.0.0.1:5432/superapi?sslmode=disable",
    [Parameter(Mandatory = $false)]
    [string]$RedisAddr = "127.0.0.1:6379"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ($Count -le 0) {
    throw "Count must be greater than zero"
}

$env:POSTGRES_ENABLED = "true"
$env:POSTGRES_URL = $PostgresUrl
$env:REDIS_ENABLED = "true"
$env:REDIS_ADDR = $RedisAddr
$env:AUTH_MODE = $AuthMode

for ($i = 1; $i -le $Count; $i++) {
    $email = "$Prefix+vu$i@$Domain"

    $result = & go run ./cmd/perftoken --email $email --password $Password --role $Role --mode $AuthMode --create-if-missing true --output json 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "Failed to seed user $email : $result"
    }

    if ($i % 50 -eq 0 -or $i -eq $Count) {
        Write-Host "SEEDED_USERS=$i"
    }
}

Write-Host "SEED_COMPLETE total=$Count"
