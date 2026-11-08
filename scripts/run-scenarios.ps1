<#
.SYNOPSIS
  Runs the reservelint binary against hand-built scenarios and compares the result with what you
  would work out by hand. Synthetic data only.

.DESCRIPTION
  Each scenario writes a small reserves file (and sometimes a redemptions file), runs
  `reservelint check`, then compares the exit code and the per-rule statuses with the expected
  values listed here. Supply is the 100,000,000 from the generated "clean" data, so the base
  reserves below are exactly 100,000,000 and sit exactly on the liquidity (5%) and coverage (100%)
  limits. Prints one line per scenario and exits 1 if any scenario does not match.

.EXAMPLE
  .\scripts\run-scenarios.ps1 -Exe .\reservelint.exe
#>
param(
    [string]$Exe = ".\reservelint.exe",
    [string]$Work = "lab\scenarios"
)

if (-not (Test-Path $Exe)) { Write-Error "Cannot find $Exe. Build it first: go build -o reservelint.exe .\cmd\reservelint"; exit 2 }
$Exe = (Resolve-Path $Exe).Path

# Preflight: make sure the program can start at all, so a blocked or broken binary is reported
# once instead of as dozens of misleading mismatches.
try {
    $version = & $Exe --version 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $version) { throw "exit code $LASTEXITCODE" }
} catch {
    Write-Error "Cannot run $Exe ($($_.Exception.Message)). If Windows blocked it, run a binary your policy allows; this script does not work around that."
    exit 2
}
Write-Host "Testing: $version"

New-Item -ItemType Directory -Force -Path $Work | Out-Null
$Work = (Resolve-Path $Work).Path

& $Exe generate-synthetic --out (Join-Path $Work "data") | Out-Null
$supply = Join-Path $Work "data\clean\supply.csv"
$cleanRedemptions = Join-Path $Work "data\clean\redemptions.csv"

$header = "holding_id,asset_type,amount,counterparty,maturity_date,is_immediately_available"
$redHeader = "request_ts,onboarded_ts,transfer_initiated_ts,amount,status"

# Base reserves: 100,000,000 in total. Cash is exactly 5%; the largest bank holds 35%.
function Reserves([string[]]$rows, [string]$head = $header) { @($head) + $rows }
$base = @(
    "H1,cash_deposit,5000000,Bank A,,true",
    "H2,us_treasury_bill,35000000,Bank B,2027-03-01,false",
    "H3,us_treasury_bill,30000000,Bank C,2027-03-01,false",
    "H4,us_treasury_bill,30000000,Bank D,2027-03-01,false"
)

$allPass = [ordered]@{
    "RW-COV-001" = "pass"; "RW-LIQ-001" = "pass"; "RW-CON-001" = "pass"
    "RW-RED-001" = "pass"; "RW-RED-002" = "pass"; "RW-COMP-001" = "pass"
}
function Expect([hashtable]$changes) {
    $e = [ordered]@{}; foreach ($k in $allPass.Keys) { $e[$k] = $allPass[$k] }
    foreach ($k in $changes.Keys) { $e[$k] = $changes[$k] }
    $e
}

$cases = @()
function Case([string]$name, $reserves, $redemptions, [int]$exit, $expect) {
    $script:cases += [pscustomobject]@{ Name = $name; Reserves = $reserves; Redemptions = $redemptions; Exit = $exit; Expect = $expect }
}

# ---- reserve rules: exact limits and one unit either side ---------------------------------
Case "Base: exactly at coverage 100% and liquidity 5%"      (Reserves $base) $null 0 (Expect @{})
Case "Liquidity just under 5%"                              (Reserves @("H1,cash_deposit,4999999.99,Bank A,,true", $base[1].Replace("35000000", "35000000.01"), $base[2], $base[3])) $null 1 (Expect @{ "RW-LIQ-001" = "fail" })
Case "Concentration exactly 40%"                            (Reserves @($base[0], $base[1].Replace("35000000", "40000000"), $base[2].Replace("30000000", "25000000"), $base[3])) $null 0 (Expect @{})
Case "Concentration just over 40%"                          (Reserves @($base[0], $base[1].Replace("35000000", "40000001"), $base[2].Replace("30000000", "24999999"), $base[3])) $null 1 (Expect @{ "RW-CON-001" = "fail" })
Case "Coverage one cent short"                              (Reserves @($base[0], $base[1], $base[2], $base[3].Replace("30000000", "29999999.99"))) $null 1 (Expect @{ "RW-COV-001" = "fail" })

# ---- composition -------------------------------------------------------------------------
Case "Unknown asset type (bitcoin)"                         (Reserves @($base[0], $base[1], $base[2], $base[3].Replace("us_treasury_bill", "bitcoin"))) $null 1 (Expect @{ "RW-COMP-001" = "fail" })
Case "Treasury maturing in exactly 93 days"                 (Reserves @($base[0], $base[1].Replace("2027-03-01", "2027-05-05"), $base[2], $base[3])) $null 0 (Expect @{})
Case "Treasury maturing in 94 days"                         (Reserves @($base[0], $base[1].Replace("2027-03-01", "2027-05-06"), $base[2], $base[3])) $null 1 (Expect @{ "RW-COMP-001" = "fail" })
Case "Treasury with no maturity date"                       (Reserves @($base[0], $base[1].Replace("2027-03-01", ""), $base[2], $base[3])) $null 1 (Expect @{ "RW-COMP-001" = "fail" })

# ---- rows, ids and duplicates --------------------------------------------------------------
Case "Negative amount stops the run (exit 2)"               (Reserves @("H1,cash_deposit,-5,Bank A,,true", $base[1], $base[2], $base[3])) $null 2 $null
Case "Missing counterparty stops the run (exit 2)"          (Reserves @("H1,cash_deposit,5000000,,,true", $base[1], $base[2], $base[3])) $null 2 $null
Case "Same line repeated with the same holding_id"          (Reserves ($base + $base[0])) $null 0 (Expect @{})
Case "Same holding_id, different amount (exit 2)"           (Reserves ($base + "H1,cash_deposit,1,Bank A,,true")) $null 2 $null
Case "Two identical lines without holding_id"               (Reserves ($base + ",cash_deposit,1000,Bank A,,true" + ",cash_deposit,1000,Bank A,,true")) $null 0 (Expect @{ "DUPLICATE_ROW_WARNING" = "warning" })
Case "Identical deposits with distinct holding_ids both count" (Reserves @("H1,cash_deposit,2500000,Bank A,,true", "H1B,cash_deposit,2500000,Bank A,,true", $base[1], $base[2], $base[3])) $null 0 (Expect @{})

# ---- repo liabilities and concentration exemptions ------------------------------------------
$h2 = "$header,is_repo_liability"
Case "20M repo liability makes coverage 80%"                (Reserves (($base | ForEach-Object { "$_," }) + "H5,repo_proceeds,20000000,Dealer,,false,true") $h2) $null 1 (Expect @{ "RW-COV-001" = "fail" })
$h3 = "$header,concentration_exemption"
Case "US Government exposure of 40.000001% is exempt from the 40% limit" (Reserves @(($base[0] + ","), "H2,us_treasury_bill,40000001,US Treasury,2027-03-01,false,us_government", ($base[2] + ","), ($base[3].Replace("30000000", "24999999") + ",")) $h3) $null 0 (Expect @{})
Case "Same exposure without the exemption breaches 40%"                 (Reserves @(($base[0] + ","), "H2,us_treasury_bill,40000001,US Treasury,2027-03-01,false,", ($base[2] + ","), ($base[3].Replace("30000000", "24999999") + ",")) $h3) $null 1 (Expect @{ "RW-CON-001" = "fail" })
Case "Exemption typo stops the run (exit 2)"                (Reserves @(($base[0] + ","), "H2,us_treasury_bill,35000000,Bank B,2027-03-01,false,us_govt", ($base[2] + ","), ($base[3] + ",")) $h3) $null 2 $null

# ---- redemption timing (T+2 business days, New York dates, Federal holidays) -------------
function Redemptions([string[]]$rows) { @($redHeader) + $rows }
$old = "2026-01-01T00:00:00Z"
Case "Monday to Wednesday is 2 days: warning"               (Reserves $base) (Redemptions @("2027-01-25T15:00:00Z,$old,2027-01-27T15:00:00Z,100,completed")) 0 (Expect @{ "RW-RED-001" = "warning" })
Case "Monday to Thursday is 3 days: breach"                 (Reserves $base) (Redemptions @("2027-01-25T15:00:00Z,$old,2027-01-28T15:00:00Z,100,completed")) 1 (Expect @{ "RW-RED-001" = "fail" })
Case "Friday to Tuesday skips the weekend: warning"         (Reserves $base) (Redemptions @("2027-01-22T15:00:00Z,$old,2027-01-26T15:00:00Z,100,completed")) 0 (Expect @{ "RW-RED-001" = "warning" })
Case "MLK Day (Mon 18 Jan) does not count: Fri to Wed warns" (Reserves $base) (Redemptions @("2027-01-15T15:00:00Z,$old,2027-01-20T15:00:00Z,100,completed")) 0 (Expect @{ "RW-RED-001" = "warning" })
Case "Late evening in New York is still that day"           (Reserves $base) (Redemptions @("2027-01-25T15:00:00Z,$old,2027-01-28T04:30:00Z,100,completed")) 0 (Expect @{ "RW-RED-001" = "warning" })
Case "Pending request 5 business days old is a breach"      (Reserves $base) (Redemptions @("2027-01-25T15:00:00Z,$old,,100,pending")) 1 (Expect @{ "RW-RED-001" = "fail" })
Case "Rejected old request is excluded from timing"         (Reserves $base) (Redemptions @("2027-01-04T15:00:00Z,$old,,100,rejected", "2027-01-25T15:00:00Z,$old,2027-01-25T20:00:00Z,100,completed")) 0 (Expect @{})
Case "Clock starts at onboarding when it is later"          (Reserves $base) (Redemptions @("2027-01-25T15:00:00Z,2027-01-27T15:00:00Z,2027-01-27T20:00:00Z,100,completed")) 0 (Expect @{})
Case "Credit to the customer's account ends the clock"      (Reserves $base) (@("request_ts,onboarded_ts,credited_ts,amount,status", "2027-01-25T15:00:00Z,$old,2027-01-26T15:00:00Z,100,completed")) 0 (Expect @{})
Case "12M of requests in 24 hours: notice to OCC due"       (Reserves $base) (Redemptions @("2027-01-25T10:00:00Z,$old,2027-01-25T12:00:00Z,6000000,completed", "2027-01-25T20:00:00Z,$old,2027-01-25T22:00:00Z,6000000,completed")) 0 (Expect @{ "RW-RED-002" = "warning" })
Case "10M of requests in 24 hours is not in excess of 10%"  (Reserves $base) (Redemptions @("2027-01-25T10:00:00Z,$old,2027-01-25T12:00:00Z,5000000,completed", "2027-01-25T20:00:00Z,$old,2027-01-25T22:00:00Z,5000000,completed")) 0 (Expect @{})

# ---- run -----------------------------------------------------------------------------------
$results = @()
$n = 0
foreach ($c in $cases) {
    $n++
    $dir = Join-Path $Work ("case{0:00}" -f $n)
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    $reservesFile = Join-Path $dir "reserves.csv"
    $redemptionsFile = Join-Path $dir "redemptions.csv"
    $c.Reserves | Set-Content -Encoding ascii $reservesFile
    if ($c.Redemptions) { $c.Redemptions | Set-Content -Encoding ascii $redemptionsFile } else { $redemptionsFile = $cleanRedemptions }
    $out = Join-Path $dir "report"
    if (Test-Path $out) { Remove-Item -Recurse -Force $out }

    $null = & $Exe check --reserves $reservesFile --supply $supply --redemptions $redemptionsFile --out $out 2>$null
    $exit = $LASTEXITCODE

    $problems = @()
    if ($exit -ne $c.Exit) { $problems += "exit $exit, expected $($c.Exit)" }
    if ($c.Expect -and $exit -in 0, 1) {
        $report = Get-Content -Raw (Join-Path $out "report.json") | ConvertFrom-Json
        $actual = @{}; foreach ($f in $report.findings) { $actual[$f.rule_id] = $f.status }
        foreach ($k in $c.Expect.Keys) {
            if ($actual[$k] -ne $c.Expect[$k]) { $problems += "$k is '$($actual[$k])', expected '$($c.Expect[$k])'" }
        }
        if (-not $c.Expect.Contains("DUPLICATE_ROW_WARNING") -and $actual.ContainsKey("DUPLICATE_ROW_WARNING")) { $problems += "unexpected DUPLICATE_ROW_WARNING" }
    }
    $results += [pscustomobject]@{
        "#" = $n; Result = $(if ($problems) { "MISMATCH" } else { "ok" }); Scenario = $c.Name
        Exit = $exit; Detail = ($problems -join "; ")
    }
}

$results | Format-Table -AutoSize -Wrap
$bad = @($results | Where-Object { $_.Result -ne "ok" })
Write-Host ("{0} of {1} scenarios matched the expected result. Reports are under {2}" -f ($results.Count - $bad.Count), $results.Count, $Work)
if ($bad.Count -gt 0) { exit 1 }
