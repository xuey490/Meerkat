function Test-PlaceholderAgentId([string]$Id) {
  $Id = if ($null -eq $Id) { "" } else { $Id.Trim() }
  return ($Id -eq "" -or $Id -eq "replace-with-stable-ulid" -or $Id -eq "local-windows-002" -or $Id -eq "local-linux-001")
}
function Read-AgentIdentity([string]$Path) {
  $result = @{ agent_id = ""; environment = ""; site = ""; role = "" }
  if (-not (Test-Path -LiteralPath $Path)) { return $result }
  $inLabels = $false
  foreach ($line in [System.IO.File]::ReadAllLines($Path)) {
    $trim = $line.Trim()
    if ($trim -eq "" -or $trim.StartsWith("#")) { continue }
    if ($line -match '^agent_id:\s*"?([^"#]+)"?\s*$') {
      $result.agent_id = $Matches[1].Trim()
      $inLabels = $false
      continue
    }
    if ($line -match '^labels:\s*$') { $inLabels = $true; continue }
    if ($inLabels -and $line -match '^  (environment|site|role):\s*"?([^"#]*)"?\s*$') {
      $result[$Matches[1]] = $Matches[2].Trim()
      continue
    }
    if ($inLabels -and $line -match '^\S') { $inLabels = $false }
  }
  return $result
}
$pkg = if ($env:MONITOR_ROOT) { $env:MONITOR_ROOT } else { (Resolve-Path (Join-Path $PSScriptRoot "..")).Path }
$agentYaml = Join-Path $pkg "client\configs\agent.yaml"
if (-not (Test-Path -LiteralPath $agentYaml)) {
  foreach ($alt in @(
      (Join-Path $pkg "client\configs\agent.windows.yaml"),
      (Join-Path $pkg "client\configs\agent.example.yaml")
    )) {
    if (Test-Path -LiteralPath $alt) { $agentYaml = $alt; break }
  }
}
$ident = Read-AgentIdentity $agentYaml
if (Test-PlaceholderAgentId $env:MONITOR_AGENT_ID) {
  if (-not (Test-PlaceholderAgentId $ident.agent_id)) { $env:MONITOR_AGENT_ID = $ident.agent_id }
}
if (-not $env:MONITOR_ENVIRONMENT) {
  if ($ident.environment) { $env:MONITOR_ENVIRONMENT = $ident.environment } else { $env:MONITOR_ENVIRONMENT = "test" }
}
if (-not $env:MONITOR_SITE) {
  if ($ident.site) { $env:MONITOR_SITE = $ident.site } else { $env:MONITOR_SITE = "local" }
}
if (-not $env:MONITOR_ROLE) {
  if ($ident.role) { $env:MONITOR_ROLE = $ident.role } else { $env:MONITOR_ROLE = "windows" }
}
if (Test-PlaceholderAgentId $env:MONITOR_AGENT_ID) {
  throw "agent_id missing in $agentYaml. Start monitor-agent once so it writes a stable id."
}
if (-not $env:MONITOR_MYSQL_DSN) { $env:MONITOR_MYSQL_DSN = "root:root@tcp(127.0.0.1:3306)/?parseTime=true" }
if (-not $env:MONITOR_REDIS_URL) { $env:MONITOR_REDIS_URL = "tcp://127.0.0.1:6379" }
if (-not $env:MONITOR_POSTGRES_ADDRESS) { $env:MONITOR_POSTGRES_ADDRESS = "postgres://postgres:123456@127.0.0.1:5432/postgres?sslmode=disable" }
$env:AGENT_ID = $env:MONITOR_AGENT_ID
$env:ENVIRONMENT = $env:MONITOR_ENVIRONMENT
$env:SITE = $env:MONITOR_SITE
$env:ROLE = $env:MONITOR_ROLE
$sampleOut = Join-Path $env:LOCALAPPDATA "Temp\telegraf-sample.out"
Remove-Item -ErrorAction SilentlyContinue -LiteralPath $sampleOut
Write-Host "SAMPLE_OUT=$sampleOut"
& (Join-Path $pkg "Telegraf\telegraf.exe") `
  --config (Join-Path $pkg "Telegraf\telegraf.conf") `
  --config (Join-Path $pkg "Telegraf\sample-file-output.conf") `
  --non-strict-env-handling `
  --once `
  --output-filter file
$ok = Test-Path -LiteralPath $sampleOut
Write-Host ("FILE_EXISTS=" + $ok)
if ($ok) {
  Write-Host "---HEAD---"
  Get-Content -LiteralPath $sampleOut -TotalCount 260
} else {
  Write-Host "---NOFILE---"
}
