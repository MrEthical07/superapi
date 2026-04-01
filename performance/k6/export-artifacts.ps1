param(
    [Parameter(Mandatory = $false)]
    [string]$SummaryPath = "performance/results/k6-summary.json",
    [Parameter(Mandatory = $false)]
    [string]$OutputDir = "performance/results"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $SummaryPath)) {
    throw "Summary file not found: $SummaryPath"
}

New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null

$summary = Get-Content -LiteralPath $SummaryPath -Raw | ConvertFrom-Json
if ($null -eq $summary.metrics) {
    throw "Invalid summary format: missing metrics"
}

$metrics = $summary.metrics

function Get-Metric {
    param(
        [string]$Name
    )

    if ($metrics.PSObject.Properties.Name -contains $Name) {
        return $metrics.$Name
    }
    return $null
}

function Get-CountValue {
    param(
        [object]$Metric
    )

    if ($null -eq $Metric) { return 0 }
    if ($Metric.PSObject.Properties.Name -contains "count") {
        return [double]$Metric.count
    }
    if ($Metric.PSObject.Properties.Name -contains "value") {
        return [double]$Metric.value
    }
    return 0
}

$errorBreakdown = [ordered]@{
    generated_at_utc = (Get-Date).ToUniversalTime().ToString("o")
    total_requests = Get-CountValue (Get-Metric "http_reqs")
    total_failed_requests = [double]((Get-Metric "http_req_failed").fails)
    failed_401 = Get-CountValue (Get-Metric "fail_401_total")
    failed_5xx = Get-CountValue (Get-Metric "fail_5xx_total")
    failed_timeout = Get-CountValue (Get-Metric "fail_timeout_total")
    failed_other = Get-CountValue (Get-Metric "fail_other_total")
    auth_login_failures = Get-CountValue (Get-Metric "auth_login_failure_total")
    auth_refresh_failures = Get-CountValue (Get-Metric "auth_refresh_failure_total")
    dropped_iterations = Get-CountValue (Get-Metric "dropped_iterations")
}

$errorBreakdownPath = Join-Path $OutputDir "error-breakdown.json"
($errorBreakdown | ConvertTo-Json -Depth 8) | Set-Content -LiteralPath $errorBreakdownPath -Encoding ascii

$latencyHistogram = [ordered]@{
    generated_at_utc = (Get-Date).ToUniversalTime().ToString("o")
    bins = @(
        [ordered]@{ bucket = "<=1ms"; count = Get-CountValue (Get-Metric "latency_le_1ms_total") },
        [ordered]@{ bucket = "(1ms,2ms]"; count = Get-CountValue (Get-Metric "latency_le_2ms_total") },
        [ordered]@{ bucket = "(2ms,5ms]"; count = Get-CountValue (Get-Metric "latency_le_5ms_total") },
        [ordered]@{ bucket = "(5ms,10ms]"; count = Get-CountValue (Get-Metric "latency_le_10ms_total") },
        [ordered]@{ bucket = "(10ms,20ms]"; count = Get-CountValue (Get-Metric "latency_le_20ms_total") },
        [ordered]@{ bucket = "(20ms,50ms]"; count = Get-CountValue (Get-Metric "latency_le_50ms_total") },
        [ordered]@{ bucket = "(50ms,100ms]"; count = Get-CountValue (Get-Metric "latency_le_100ms_total") },
        [ordered]@{ bucket = "(100ms,250ms]"; count = Get-CountValue (Get-Metric "latency_le_250ms_total") },
        [ordered]@{ bucket = ">250ms"; count = Get-CountValue (Get-Metric "latency_gt_250ms_total") }
    )
}

$latencyHistogramPath = Join-Path $OutputDir "latency-histogram.json"
($latencyHistogram | ConvertTo-Json -Depth 8) | Set-Content -LiteralPath $latencyHistogramPath -Encoding ascii

$rpsRows = @()
foreach ($property in $metrics.PSObject.Properties) {
    if ($property.Name -match '^http_reqs\{scenario:(.+)\}$') {
        $scenario = $Matches[1]
        $metric = $property.Value
        $rpsRows += [pscustomobject]@{
            scenario = $scenario
            requests = if ($metric.PSObject.Properties.Name -contains "count") { [double]$metric.count } else { 0 }
            rate_per_sec = if ($metric.PSObject.Properties.Name -contains "rate") { [double]$metric.rate } else { 0 }
        }
    }
}

if ($rpsRows.Count -eq 0) {
    $globalReq = Get-Metric "http_reqs"
    $rpsRows += [pscustomobject]@{
        scenario = "all"
        requests = if ($globalReq.PSObject.Properties.Name -contains "count") { [double]$globalReq.count } else { 0 }
        rate_per_sec = if ($globalReq.PSObject.Properties.Name -contains "rate") { [double]$globalReq.rate } else { 0 }
    }
}

$rpsRows = $rpsRows | Sort-Object scenario
$rpsPath = Join-Path $OutputDir "rps-over-time.csv"
$rpsRows | Export-Csv -LiteralPath $rpsPath -NoTypeInformation -Encoding ascii

$vuRows = @()
foreach ($property in $metrics.PSObject.Properties) {
    if ($property.Name -match '^scenario_vus\{scenario:(.+)\}$') {
        $scenario = $Matches[1]
        $metric = $property.Value
        $vuRows += [pscustomobject]@{
            scenario = $scenario
            vu_min = if ($metric.PSObject.Properties.Name -contains "min") { [double]$metric.min } else { 0 }
            vu_max = if ($metric.PSObject.Properties.Name -contains "max") { [double]$metric.max } else { 0 }
            vu_last = if ($metric.PSObject.Properties.Name -contains "value") { [double]$metric.value } else { 0 }
        }
    }
}

if ($vuRows.Count -eq 0) {
    $globalVus = Get-Metric "vus"
    $vuRows += [pscustomobject]@{
        scenario = "all"
        vu_min = if ($globalVus.PSObject.Properties.Name -contains "min") { [double]$globalVus.min } else { 0 }
        vu_max = if ($globalVus.PSObject.Properties.Name -contains "max") { [double]$globalVus.max } else { 0 }
        vu_last = if ($globalVus.PSObject.Properties.Name -contains "value") { [double]$globalVus.value } else { 0 }
    }
}

$vuRows = $vuRows | Sort-Object scenario
$vuPath = Join-Path $OutputDir "vu-usage-over-time.csv"
$vuRows | Export-Csv -LiteralPath $vuPath -NoTypeInformation -Encoding ascii

Write-Host "ARTIFACT_READY summary=$SummaryPath"
Write-Host "ARTIFACT_READY error=$errorBreakdownPath"
Write-Host "ARTIFACT_READY histogram=$latencyHistogramPath"
Write-Host "ARTIFACT_READY rps=$rpsPath"
Write-Host "ARTIFACT_READY vus=$vuPath"
