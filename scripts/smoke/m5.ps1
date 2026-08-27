param(
    [string]$Binary = ""
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Start-Dropserve {
    param(
        [string]$BinaryPath,
        [string]$AppsRoot,
        [string]$StatePath
    )

    $start = [System.Diagnostics.ProcessStartInfo]::new()
    $start.FileName = $BinaryPath
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    foreach ($argument in @("serve", "--listen", "127.0.0.1:0", "--root", $AppsRoot, "--state", $StatePath)) {
        $start.ArgumentList.Add($argument)
    }
    $process = [System.Diagnostics.Process]::Start($start)
    $readyLine = $process.StandardOutput.ReadLineAsync()
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    while (-not $readyLine.IsCompleted -and [DateTime]::UtcNow -lt $deadline) {
        if ($process.HasExited) {
            throw "Dropserve exited before becoming ready: $($process.StandardError.ReadToEnd())"
        }
        $null = $readyLine.Wait(250)
    }
    if (-not $readyLine.IsCompleted -or $readyLine.Result -notmatch '^Dropserve is ready at (http://\S+)$') {
        if (-not $process.HasExited) {
            $process.Kill($true)
        }
        throw "Dropserve did not print a ready address within 45 seconds"
    }
    return [pscustomobject]@{ Process = $process; Address = $Matches[1] }
}

function Stop-Dropserve {
    param([System.Diagnostics.Process]$Process)

    if (-not $Process.HasExited) {
        $Process.Kill($true)
        $Process.WaitForExit()
    }
    $Process.Dispose()
}

function Get-Text {
    param([System.Net.Http.HttpClient]$Client, [string]$URL)

    $response = $Client.GetAsync($URL).GetAwaiter().GetResult()
    try {
        if (-not $response.IsSuccessStatusCode) {
            throw "$URL returned $([int]$response.StatusCode)"
        }
        return $response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    }
    finally {
        $response.Dispose()
    }
}

$repositoryRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
if ($Binary -eq "") {
    $Binary = Join-Path $repositoryRoot "bin\dropserve-cli.exe"
}
$binaryPath = (Resolve-Path -LiteralPath $Binary).Path
$fixturesRoot = (Resolve-Path -LiteralPath (Join-Path $repositoryRoot "testdata\fixtures")).Path
$temporaryRoot = [System.IO.Path]::GetTempPath()
$workDirectory = Join-Path $temporaryRoot ("dropserve-m5-smoke-" + [Guid]::NewGuid().ToString("N"))
$appsRoot = Join-Path $workDirectory "apps"
$statePath = Join-Path $workDirectory "state.json"
$null = New-Item -ItemType Directory -Path $appsRoot
Copy-Item -LiteralPath (Join-Path $fixturesRoot "absolute-paths") -Destination $appsRoot -Recurse
Copy-Item -LiteralPath (Join-Path $fixturesRoot "subpath") -Destination $appsRoot -Recurse
$handler = [System.Net.Http.HttpClientHandler]::new()
$handler.AllowAutoRedirect = $false
$client = [System.Net.Http.HttpClient]::new($handler)
$run = $null

try {
    $run = Start-Dropserve -BinaryPath $binaryPath -AppsRoot $appsRoot -StatePath $statePath
    $address = $run.Address

    $redirect = $client.GetAsync("$address/subpath/redirect").GetAwaiter().GetResult()
    try {
        $redirectLocation = if ($null -eq $redirect.Headers.Location) { "<missing>" } else { $redirect.Headers.Location.OriginalString }
        if ([int]$redirect.StatusCode -ne 302 -or $redirectLocation -ne "/subpath/login") {
            throw "Redirect rewrite was $([int]$redirect.StatusCode) $redirectLocation instead of 302 /subpath/login"
        }
    }
    finally {
        $redirect.Dispose()
    }

    $cookie = $client.GetAsync("$address/subpath/cookie").GetAwaiter().GetResult()
    try {
        $setCookie = ($cookie.Headers.GetValues("Set-Cookie") -join "; ")
        if ($setCookie -notmatch 'Path=/subpath/') {
            throw "Cookie Path was not rewritten under /subpath/"
        }
    }
    finally {
        $cookie.Dispose()
    }

    $html = Get-Text -Client $client -URL "$address/subpath/html-no-base"
    if ($html -notmatch '<head><base href="/subpath/">') {
        throw "HTML base element was not injected"
    }
    $asset = Get-Text -Client $client -URL "$address/subpath/asset.json"
    if ($asset -ne '{"markup":"<head>json</head>"}') {
        throw "Non-HTML response changed through the proxy"
    }
    $headers = (Get-Text -Client $client -URL "$address/subpath/headers") | ConvertFrom-Json
    if ($headers.prefix -ne "/subpath" -or $headers.scriptName -ne "/subpath" -or $headers.proto -ne "http") {
        throw "Forwarded subpath headers were incorrect"
    }

    $socket = [System.Net.WebSockets.ClientWebSocket]::new()
    try {
        $webSocketURL = $address.Replace("http://", "ws://") + "/subpath/ws"
        $null = $socket.ConnectAsync([Uri]$webSocketURL, [Threading.CancellationToken]::None).GetAwaiter().GetResult()
        $message = [Text.Encoding]::UTF8.GetBytes("m5 smoke echo")
        $null = $socket.SendAsync([ArraySegment[byte]]::new($message), [Net.WebSockets.WebSocketMessageType]::Text, $true, [Threading.CancellationToken]::None).GetAwaiter().GetResult()
        $buffer = [byte[]]::new(128)
        $received = $socket.ReceiveAsync([ArraySegment[byte]]::new($buffer), [Threading.CancellationToken]::None).GetAwaiter().GetResult()
        if ([Text.Encoding]::UTF8.GetString($buffer, 0, $received.Count) -ne "m5 smoke echo") {
            throw "WebSocket echo did not round-trip"
        }
    }
    finally {
        $socket.Dispose()
    }

    $apps = (Get-Text -Client $client -URL "$address/_dropserve/api/apps") | ConvertFrom-Json
    $absolute = @($apps | Where-Object { $_.slug -eq "absolute-paths" })
    if ($absolute.Count -ne 1 -or -not $absolute[0].prefers_own_port -or [string]::IsNullOrWhiteSpace($absolute[0].urls.own)) {
        throw "Absolute-path fixture did not prefer its own port"
    }
    $firstPort = [int]$absolute[0].port
    $ownBody = Get-Text -Client $client -URL $absolute[0].urls.own
    if ($ownBody -notmatch 'Absolute paths fixture') {
        throw "Own-port URL did not serve the fixture at its root"
    }
    $dashboardScript = Get-Text -Client $client -URL "$address/_dropserve/app.js"
    if ($dashboardScript -notmatch 'This app expects to live at the root' -or $dashboardScript -notmatch 'Use the short URL anyway') {
        throw "Dashboard did not ship the own-port rescue explanation and action"
    }

    Stop-Dropserve -Process $run.Process
    $run = $null
    $run = Start-Dropserve -BinaryPath $binaryPath -AppsRoot $appsRoot -StatePath $statePath
    $restartedApps = (Get-Text -Client $client -URL "$($run.Address)/_dropserve/api/apps") | ConvertFrom-Json
    $restartedAbsolute = @($restartedApps | Where-Object { $_.slug -eq "absolute-paths" })
    if ($restartedAbsolute.Count -ne 1 -or [int]$restartedAbsolute[0].port -ne $firstPort) {
        throw "Own-port assignment changed after restart"
    }

    Write-Output "M5 smoke passed: rewrites, headers, WebSocket echo, own-port rescue, and stable port $firstPort worked at $($run.Address)/"
}
finally {
    if ($null -ne $run) {
        Stop-Dropserve -Process $run.Process
    }
    $client.Dispose()
    $handler.Dispose()
    $resolvedTemporaryRoot = [System.IO.Path]::GetFullPath($temporaryRoot)
    $resolvedWorkDirectory = [System.IO.Path]::GetFullPath($workDirectory)
    if ($resolvedWorkDirectory.StartsWith($resolvedTemporaryRoot, [StringComparison]::OrdinalIgnoreCase) -and
        (Split-Path -Leaf $resolvedWorkDirectory).StartsWith("dropserve-m5-smoke-", [StringComparison]::Ordinal)) {
        Remove-Item -LiteralPath $resolvedWorkDirectory -Recurse -Force
    }
}
