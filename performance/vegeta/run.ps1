[CmdletBinding()]
param(
    [string]$BaseUrl = "http://127.0.0.1:8080",
    [Parameter(Mandatory = $true)]
    [string]$AuthToken,
    [int]$Rate = 10000,
    [string]$RampDuration = "10m",
    [string]$SustainDuration = "30m",
    [string]$OutputDir = "performance/results/vegeta",
    [string]$Vegeta = "vegeta",
    [bool]$Treat401AsExpected = $true
)

$ErrorActionPreference = "Stop"

function Require-Command {
    param([string]$Name)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "Required command not found: $Name"
    }
}

function Parse-RampSteps {
    param([string]$Duration)
    if ($Duration -notmatch "^(\d+)m$") {
        throw "RampDuration must be an integer minute value (for example: 10m)."
    }
    return [int]$Matches[1]
}

function New-WeightedTargets {
    param(
        [string]$NormalizedBaseUrl,
        [string]$Token,
        [string]$ParseDurationBodyFile,
        [string]$Path
    )

    $routes = @(
        @{ Weight = 35; Method = "GET";  Path = "/healthz";               Headers = @{};                                        BodyFile = $null },
        @{ Weight = 25; Method = "GET";  Path = "/readyz";                Headers = @{};                                        BodyFile = $null },
        @{ Weight = 20; Method = "POST"; Path = "/system/parse-duration"; Headers = @{ "Content-Type" = "application/json" };  BodyFile = $ParseDurationBodyFile },
        @{ Weight = 20; Method = "GET";  Path = "/api/v1/system/whoami";  Headers = @{ "Authorization" = "Bearer $Token" };     BodyFile = $null }
    )

    $builder = New-Object System.Text.StringBuilder

    foreach ($route in $routes) {
        for ($i = 0; $i -lt [int]$route.Weight; $i++) {
            [void]$builder.AppendLine("$($route.Method) $NormalizedBaseUrl$($route.Path)")
            foreach ($headerName in $route.Headers.Keys) {
                [void]$builder.AppendLine("${headerName}: $($route.Headers[$headerName])")
            }
            if ($null -ne $route.BodyFile) {
                [void]$builder.AppendLine("@$($route.BodyFile)")
            }
            [void]$builder.AppendLine("")
        }
    }

    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $builder.ToString(), $utf8NoBom)
}

Require-Command -Name $Vegeta

if ([string]::IsNullOrWhiteSpace($AuthToken)) {
    throw "AuthToken is required for authenticated /api/v1/system/whoami traffic."
}

if ($Rate -le 0) {
    throw "Rate must be > 0"
}

$rampSteps = Parse-RampSteps -Duration $RampDuration
$normalizedBaseUrl = $BaseUrl.TrimEnd("/")

New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
$targetsPath = Join-Path $OutputDir "targets.txt"
$parseDurationBodyPath = Join-Path $OutputDir "parse-duration-body.json"
Set-Content -Path $parseDurationBodyPath -Value '{"duration":"250ms"}' -Encoding ascii
New-WeightedTargets -NormalizedBaseUrl $normalizedBaseUrl -Token $AuthToken -ParseDurationBodyFile $parseDurationBodyPath -Path $targetsPath

$resultFiles = @()

for ($step = 1; $step -le $rampSteps; $step++) {
    $stepRate = [Math]::Max(1, [int][Math]::Round(($Rate * $step) / $rampSteps))
    $stepFile = Join-Path $OutputDir ("ramp-step-{0:D2}.bin" -f $step)

    Write-Host "[vegeta] Ramp step $step/$rampSteps @ $stepRate rps for 1m"
    & $Vegeta attack -targets="$targetsPath" -rate="$stepRate/1s" -duration="1m" -output="$stepFile"
    if ($LASTEXITCODE -ne 0) {
        throw "vegeta attack failed during ramp step $step"
    }

    $resultFiles += $stepFile
}

$sustainFile = Join-Path $OutputDir "sustain.bin"
Write-Host "[vegeta] Sustain @ $Rate rps for $SustainDuration"
& $Vegeta attack -targets="$targetsPath" -rate="$Rate/1s" -duration="$SustainDuration" -output="$sustainFile"
if ($LASTEXITCODE -ne 0) {
    throw "vegeta attack failed during sustain"
}
$resultFiles += $sustainFile

$textReportPath = Join-Path $OutputDir "report.txt"
$jsonReportPath = Join-Path $OutputDir "report.json"
$histReportPath = Join-Path $OutputDir "histogram.txt"
$plotPath = Join-Path $OutputDir "plot.html"

Write-Host "[vegeta] Writing reports"
& $Vegeta report -type=text @resultFiles | Tee-Object -FilePath $textReportPath
& $Vegeta report -type=json @resultFiles | Set-Content -Path $jsonReportPath -Encoding utf8
& $Vegeta report -type='hist[0,50ms,100ms,250ms,500ms,1s,2s]' @resultFiles | Set-Content -Path $histReportPath -Encoding utf8
& $Vegeta plot @resultFiles | Set-Content -Path $plotPath -Encoding utf8

$jsonReport = Get-Content $jsonReportPath -Raw | ConvertFrom-Json
$statusCodes = @{}
foreach ($prop in $jsonReport.status_codes.PSObject.Properties) {
    $statusCodes[$prop.Name] = [int]$prop.Value
}
$total = [int]$jsonReport.requests
$status200 = if ($statusCodes.ContainsKey("200")) { $statusCodes["200"] } else { 0 }
$status401 = if ($statusCodes.ContainsKey("401")) { $statusCodes["401"] } else { 0 }
$status0 = if ($statusCodes.ContainsKey("0")) { $statusCodes["0"] } else { 0 }

$adjustedOk = $status200
if ($Treat401AsExpected) {
    $adjustedOk += $status401
}
$adjustedSuccess = if ($total -gt 0) { [double]$adjustedOk / [double]$total } else { 0 }

Write-Host ("[vegeta] Summary: total=" + $total + " status200=" + $status200 + " status401=" + $status401 + " status0=" + $status0)
Write-Host ("[vegeta] AdjustedSuccess(" + ($(if ($Treat401AsExpected) {"200+401"} else {"200 only"})) + ")=" + [math]::Round($adjustedSuccess * 100, 2) + "%")

Write-Host "[vegeta] Completed. Results in $OutputDir"
