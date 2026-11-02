<#
  VideoEncoder - installer complet pentru Windows (Intel QSV / AMD AMF / NVIDIA NVENC)
  https://github.com/maxvideostar/VideoEncoder

  Rulare:
    - dublu-click pe Install-VideoEncoder.bat
    - sau din PowerShell (ca Administrator):
        irm https://raw.githubusercontent.com/maxvideostar/VideoEncoder/main/install.ps1 | iex

  Parametri (optionali):
    -Dir C:\VideoEncoder   folderul de instalare
    -Port 8090             portul interfetei web
    -Password parola       parola pentru accesul extern (utilizator: admin)
    -NoService             nu instala serviciul Windows
    -Silent                fara intrebari (valori implicite)
    -Uninstall             dezinstaleaza
#>
param(
    [string]$Dir = "C:\VideoEncoder",
    [int]$Port = 8090,
    [string]$Password = "",
    [switch]$NoService,
    [switch]$Silent,
    [switch]$Uninstall
)

$ErrorActionPreference = "Continue"   # PS 5.1: stderr-ul programelor externe nu trebuie sa opreasca scriptul
$ProgressPreference = "SilentlyContinue"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$RepoRaw   = "https://raw.githubusercontent.com/maxvideostar/VideoEncoder/main"
$ScriptUrl = "$RepoRaw/install.ps1"
$FFBuilds  = @(
    @{ Name = "FFmpeg (ultima versiune)";   Folder = "ffmpeg";     Url = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip" },
    @{ Name = "FFmpeg 6.1 (drivere vechi)"; Folder = "ffmpeg-6.1"; Url = "https://github.com/GyanD/codexffmpeg/releases/download/6.1.1/ffmpeg-6.1.1-essentials_build.zip" },
    @{ Name = "FFmpeg 5.1 (drivere foarte vechi)"; Folder = "ffmpeg-5.1"; Url = "https://github.com/GyanD/codexffmpeg/releases/download/5.1.2/ffmpeg-5.1.2-essentials_build.zip" }
)

function Say($msg, $color = "Gray") { Write-Host $msg -ForegroundColor $color }
function Step($msg) { Write-Host ""; Write-Host "==> $msg" -ForegroundColor Cyan }
function Ok($msg)   { Write-Host "    [OK] $msg" -ForegroundColor Green }
function Warn($msg) { Write-Host "    [!]  $msg" -ForegroundColor Yellow }
function Bad($msg)  { Write-Host "    [X]  $msg" -ForegroundColor Red }
function Ask($q, $def = "D") {
    if ($Silent) { return ($def -eq "D") }
    $a = Read-Host "    $q [$(if ($def -eq 'D') {'D/n'} else {'d/N'})]"
    if ([string]::IsNullOrWhiteSpace($a)) { $a = $def }
    return ($a -match '^[dDyY]')
}

# ---------------------------------------------------------------- administrator
$isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    Say "Este nevoie de drepturi de administrator - confirma promptul UAC..." Yellow
    $argList = @()
    foreach ($k in $PSBoundParameters.Keys) {
        $v = $PSBoundParameters[$k]
        if ($v -is [switch]) { if ($v) { $argList += "-$k" } } else { $argList += "-$k `"$v`"" }
    }
    if ($PSCommandPath) {
        Start-Process powershell.exe -Verb RunAs -ArgumentList (@("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "`"$PSCommandPath`"") + $argList)
    } else {
        # rulat prin "irm | iex": descarcam scriptul intr-un fisier temporar
        $tmp = Join-Path $env:TEMP "VideoEncoder-install.ps1"
        Invoke-WebRequest -UseBasicParsing $ScriptUrl -OutFile $tmp
        Start-Process powershell.exe -Verb RunAs -ArgumentList (@("-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "`"$tmp`"") + $argList)
    }
    exit
}

$Exe = Join-Path $Dir "VideoEncoder.exe"
$Here = if ($PSScriptRoot) { $PSScriptRoot } else { (Get-Location).Path }

function Stop-VideoEncoder {
    $svc = Get-Service -Name "VideoEncoder" -ErrorAction SilentlyContinue
    if ($svc -and $svc.Status -ne "Stopped") {
        Say "    opresc serviciul VideoEncoder..."
        try { Stop-Service -Name "VideoEncoder" -Force -ErrorAction Stop } catch { & sc.exe stop VideoEncoder | Out-Null }
        try { $svc.WaitForStatus("Stopped", [TimeSpan]::FromSeconds(30)) } catch {}
    }
    Get-Process -Name "VideoEncoder" -ErrorAction SilentlyContinue | Stop-Process -Force -ErrorAction SilentlyContinue
    Get-Process -Name "ffmpeg", "ffprobe" -ErrorAction SilentlyContinue |
        Where-Object { $_.Path -and $_.Path.StartsWith($Dir, [StringComparison]::OrdinalIgnoreCase) } |
        Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Milliseconds 800
}

# ---------------------------------------------------------------- dezinstalare
if ($Uninstall) {
    Step "Dezinstalare VideoEncoder"
    Stop-VideoEncoder
    if (Test-Path $Exe) { & $Exe -uninstall 2>$null | Out-Null }
    & sc.exe delete VideoEncoder 2>$null | Out-Null
    & netsh advfirewall firewall delete rule name="VideoEncoder" 2>$null | Out-Null
    Remove-Item "$env:PUBLIC\Desktop\VideoEncoder.lnk", "$env:ProgramData\Microsoft\Windows\Start Menu\Programs\VideoEncoder.lnk" -ErrorAction SilentlyContinue
    Remove-Item "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\VideoEncoder" -Recurse -ErrorAction SilentlyContinue
    Ok "serviciu, firewall, scurtaturi eliminate"
    if (Ask "Stergi si folderul $Dir (setari, coada, fisiere iesire)?" "N") {
        Remove-Item $Dir -Recurse -Force -ErrorAction SilentlyContinue
        Ok "folder sters"
    } else { Say "    folderul $Dir a fost pastrat" }
    if (-not $Silent) { Read-Host "Apasa Enter pentru a inchide" }
    exit
}

Clear-Host
Say "=====================================================" Cyan
Say "   VideoEncoder - instalare (Intel / AMD / NVIDIA)" Cyan
Say "=====================================================" Cyan
Say "   Folder: $Dir    Port: $Port"

# ---------------------------------------------------------------- 1. placa video
Step "1. Detectez placa video"
$gpus = @(Get-CimInstance Win32_VideoController | Where-Object { $_.Name -notmatch "Remote|Mirror|Virtual|Parsec|Citrix" })
$targets = @()
$needDriver = $false
foreach ($g in $gpus) {
    $pnp = "$($g.PNPDeviceID)"
    $vendor = if ($pnp -match "VEN_8086") { "Intel" } elseif ($pnp -match "VEN_1002|VEN_1022") { "AMD" } elseif ($pnp -match "VEN_10DE") { "NVIDIA" } else { "Altul" }
    $drvDate = if ($g.DriverDate) { ([datetime]$g.DriverDate).ToString("yyyy-MM-dd") } else { "?" }
    Say ("    {0}  |  driver {1} ({2})" -f $g.Name, $g.DriverVersion, $drvDate)
    $basic = ($g.Name -match "Microsoft Basic|Standard VGA|Basic Display") -or ("$($g.AdapterCompatibility)" -match "^Microsoft")
    if ($basic) {
        Bad "nu are driver de placa video instalat (Microsoft Basic Display) - encodarea hardware NU va merge"
        $needDriver = $true
        if ($pnp -match "VEN_8086") { $vendor = "Intel" } elseif ($pnp -match "VEN_1002") { $vendor = "AMD" }
    }
    switch ($vendor) {
        "Intel"  { $targets += @{ Vendor = "Intel";  Enc = "h264_qsv";   Gpu = $g.Name } }
        "AMD"    { $targets += @{ Vendor = "AMD";    Enc = "h264_amf";   Gpu = $g.Name } }
        "NVIDIA" { $targets += @{ Vendor = "NVIDIA"; Enc = "h264_nvenc"; Gpu = $g.Name } }
    }
}
if ($targets.Count -eq 0) { Warn "nu am gasit o placa Intel / AMD / NVIDIA - se va encoda pe procesor (libx264)" }
foreach ($t in $targets) { Ok ("{0} -> encoder hardware {1}" -f $t.Vendor, $t.Enc) }

$cpu = (Get-CimInstance Win32_Processor | Select-Object -First 1)
$ramGB = [math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB, 1)
Say ("    Procesor: {0} ({1} nuclee / {2} fire)   RAM: {3} GB" -f $cpu.Name.Trim(), $cpu.NumberOfCores, $cpu.NumberOfLogicalProcessors, $ramGB)

function Open-DriverPage($vendor) {
    switch ($vendor) {
        "Intel"  { Start-Process "https://www.intel.com/content/www/us/en/support/detect.html" }
        "AMD"    { Start-Process "https://www.amd.com/en/support/download/drivers.html" }
        "NVIDIA" { Start-Process "https://www.nvidia.com/en-us/drivers/" }
    }
}

# ---------------------------------------------------------------- 2. fisiere aplicatie
Step "2. Instalez VideoEncoder in $Dir"
New-Item -ItemType Directory -Force -Path $Dir | Out-Null
$wasService = [bool](Get-Service -Name "VideoEncoder" -ErrorAction SilentlyContinue)
Stop-VideoEncoder

$localExe = Join-Path $Here "VideoEncoder.exe"
if ((Test-Path $localExe) -and ((Resolve-Path $localExe).Path -ne (Resolve-Path $Exe -ErrorAction SilentlyContinue).Path)) {
    Copy-Item $localExe $Exe -Force
    Ok "copiat VideoEncoder.exe din $Here"
} elseif (Test-Path $localExe) {
    Ok "VideoEncoder.exe deja in folder"
} else {
    Say "    descarc VideoEncoder.exe de pe GitHub..."
    Invoke-WebRequest -UseBasicParsing "$RepoRaw/VideoEncoder.exe" -OutFile "$Exe.new"
    Move-Item "$Exe.new" $Exe -Force
    Ok "descarcat VideoEncoder.exe"
}
foreach ($f in "service-install.bat", "service-uninstall.bat", "firewall-allow.bat", "README.md") {
    $src = Join-Path $Here $f
    if ((Test-Path $src) -and ($Here -ne $Dir)) { Copy-Item $src $Dir -Force -ErrorAction SilentlyContinue }
}
# copie a installerului (pentru dezinstalare / reinstalare)
try {
    if ($PSCommandPath -and ($PSCommandPath -ne (Join-Path $Dir "install.ps1"))) { Copy-Item $PSCommandPath (Join-Path $Dir "install.ps1") -Force }
    elseif (-not $PSCommandPath) { Invoke-WebRequest -UseBasicParsing $ScriptUrl -OutFile (Join-Path $Dir "install.ps1") }
} catch {}
$ver = (& $Exe -version 2>$null | Select-Object -First 1)
Ok "versiune $ver"

# ---------------------------------------------------------------- 3. FFmpeg + test encoder hardware
Step "3. FFmpeg si test encoder hardware"

function Get-FFmpeg($build) {
    $dest = Join-Path $Dir $build.Folder
    $ff = Join-Path $dest "ffmpeg.exe"
    if (Test-Path $ff) { return $ff }
    Say ("    descarc {0}..." -f $build.Name)
    $zip = Join-Path $env:TEMP ("ve-" + $build.Folder + ".zip")
    try {
        Invoke-WebRequest -UseBasicParsing $build.Url -OutFile $zip
    } catch { Bad "descarcare esuata: $($_.Exception.Message)"; return $null }
    $tmp = Join-Path $env:TEMP ("ve-" + $build.Folder)
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
    Expand-Archive $zip $tmp -Force
    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    foreach ($n in "ffmpeg.exe", "ffprobe.exe") {
        $f = Get-ChildItem $tmp -Recurse -Filter $n | Select-Object -First 1
        if ($f) { Copy-Item $f.FullName $dest -Force }
    }
    Remove-Item $zip, $tmp -Recurse -Force -ErrorAction SilentlyContinue
    if (Test-Path $ff) { return $ff } else { Bad "arhiva nu contine ffmpeg.exe"; return $null }
}

function Test-Encoder($ff, $enc) {
    $err = Join-Path $env:TEMP "ve-enctest.txt"
    $ffArgs = "-hide_banner -v error -f lavfi -i color=c=black:s=1280x720:r=25 -t 2 -pix_fmt nv12 -c:v $enc -b:v 2000k -f null -"
    $p = Start-Process -FilePath $ff -ArgumentList $ffArgs -NoNewWindow -Wait -PassThru -RedirectStandardError $err
    $msg = (Get-Content $err -ErrorAction SilentlyContinue | Select-Object -Last 2) -join " "
    Remove-Item $err -ErrorAction SilentlyContinue
    return @{ Ok = ($p.ExitCode -eq 0); Msg = $msg }
}

$chosenFF = $null
$results = @()
if ($targets.Count -eq 0) {
    $chosenFF = Get-FFmpeg $FFBuilds[0]
} else {
    # fara driver nu are rost sa incercam versiuni vechi
    $builds = if ($needDriver) { @($FFBuilds[0]) } else { $FFBuilds }
    $bestOk = -1
    foreach ($b in $builds) {
        $ff = Get-FFmpeg $b
        if (-not $ff) { continue }
        $res = @()
        $okCount = 0
        foreach ($t in $targets) {
            $r = Test-Encoder $ff $t.Enc
            $res += @{ T = $t; R = $r }
            if ($r.Ok) { $okCount++; Ok ("{0}: {1} functioneaza cu {2}" -f $t.Vendor, $t.Enc, $b.Name) }
            else { Warn ("{0}: {1} NU merge cu {2} ({3})" -f $t.Vendor, $t.Enc, $b.Name, $r.Msg) }
        }
        if ($okCount -gt $bestOk) { $bestOk = $okCount; $chosenFF = $ff; $results = $res }
        if ($okCount -eq $targets.Count) { break }
        if ($b -ne $builds[-1]) { Say "    incerc o versiune FFmpeg mai veche (compatibila cu drivere vechi)..." }
    }
    if ($bestOk -le 0) {
        $chosenFF = Join-Path $Dir "ffmpeg\ffmpeg.exe"
        Bad "encoderul hardware nu functioneaza cu nicio versiune FFmpeg"
        if ($needDriver) { Warn "cauza: lipseste driverul placii video" } else { Warn "cauza probabila: driver prea vechi sau lipsa componente media" }
        foreach ($t in $targets) {
            if (Ask ("Deschid pagina de drivere {0}?" -f $t.Vendor) "D") { Open-DriverPage $t.Vendor }
        }
        Warn "VideoEncoder va merge oricum, dar encodarea va fi pe procesor (mai lent). Reruleaza installerul dupa instalarea driverului."
    }
}
if ($chosenFF) {
    & $Exe -set-ffmpeg $chosenFF | Out-Null
    Ok "FFmpeg folosit: $chosenFF"
}

# ---------------------------------------------------------------- 4. setari
Step "4. Setari acces"
& $Exe -set-port $Port | Out-Null
Ok "port $Port"
if (-not $Password -and -not $Silent) {
    $Password = Read-Host "    Parola pentru acces extern (utilizator 'admin'; Enter = pastreaza parola actuala)"
}
if ($Password) {
    & $Exe -set-user admin -set-password $Password | Out-Null
    Ok "parola setata (utilizator: admin)"
} else {
    $cfgFile = Join-Path $Dir "config.json"
    if ((Test-Path $cfgFile) -and ((Get-Content $cfgFile -Raw) -match '"default_pass":\s*true')) {
        Warn "parola externa este inca 'admin' - schimb-o din Setari inainte de a deschide portul in router"
    }
}

# ---------------------------------------------------------------- 5. firewall
Step "5. Firewall Windows"
& netsh advfirewall firewall delete rule name="VideoEncoder" 2>$null | Out-Null
& netsh advfirewall firewall add rule name="VideoEncoder" dir=in action=allow protocol=TCP localport=$Port | Out-Null
& netsh advfirewall firewall add rule name="VideoEncoder" dir=in action=allow program="$Exe" enable=yes | Out-Null
Ok "portul $Port deschis"

# ---------------------------------------------------------------- 6. alimentare (stream 24/7)
Step "6. Alimentare (pentru stream 24/7)"
if (Ask "Dezactivez sleep / hibernare cand PC-ul e la priza (recomandat pentru stream)?" "D") {
    & powercfg /change standby-timeout-ac 0 | Out-Null
    & powercfg /change hibernate-timeout-ac 0 | Out-Null
    & powercfg /change disk-timeout-ac 0 | Out-Null
    & powercfg /hibernate off 2>$null | Out-Null
    Ok "sleep si hibernare dezactivate"
}

# ---------------------------------------------------------------- 7. serviciu
Step "7. Serviciu Windows"
$installSvc = -not $NoService
if (-not $NoService -and -not $Silent -and -not $wasService) {
    $installSvc = Ask "Instalez ca serviciu (porneste cu Windows, inainte de logare, stream-urile repornesc singure)?" "D"
}
if ($installSvc) {
    & $Exe -uninstall 2>$null | Out-Null
    & sc.exe delete VideoEncoder 2>$null | Out-Null
    Start-Sleep -Seconds 1
    & $Exe -install
    if ($LASTEXITCODE -eq 0) { Ok "serviciul VideoEncoder instalat si pornit" } else { Bad "instalarea serviciului a esuat" }
} else {
    Say "    fara serviciu - porneste VideoEncoder din scurtatura"
}

# ---------------------------------------------------------------- 8. scurtaturi + Programs and Features
Step "8. Scurtaturi"
$ws = New-Object -ComObject WScript.Shell
foreach ($lnkPath in "$env:PUBLIC\Desktop\VideoEncoder.lnk", "$env:ProgramData\Microsoft\Windows\Start Menu\Programs\VideoEncoder.lnk") {
    $s = $ws.CreateShortcut($lnkPath)
    $s.TargetPath = $Exe
    $s.WorkingDirectory = $Dir
    $s.IconLocation = "$Exe,0"
    $s.Description = "VideoEncoder - interfata FFmpeg"
    $s.Save()
}
Ok "Desktop + Start Menu"

$uk = "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\VideoEncoder"
New-Item $uk -Force | Out-Null
Set-ItemProperty $uk -Name DisplayName -Value "VideoEncoder"
Set-ItemProperty $uk -Name DisplayVersion -Value "$ver"
Set-ItemProperty $uk -Name Publisher -Value "maxvideostar"
Set-ItemProperty $uk -Name InstallLocation -Value $Dir
Set-ItemProperty $uk -Name DisplayIcon -Value $Exe
Set-ItemProperty $uk -Name URLInfoAbout -Value "https://github.com/maxvideostar/VideoEncoder"
Set-ItemProperty $uk -Name UninstallString -Value ("powershell.exe -NoProfile -ExecutionPolicy Bypass -File `"{0}`" -Uninstall -Dir `"{1}`"" -f (Join-Path $Dir "install.ps1"), $Dir)
Set-ItemProperty $uk -Name NoModify -Value 1 -Type DWord
Set-ItemProperty $uk -Name NoRepair -Value 1 -Type DWord
Ok "inregistrat in Aplicatii instalate (dezinstalare de acolo)"

# ---------------------------------------------------------------- gata
Step "Gata!"
$ips = @(Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue | Where-Object { $_.IPAddress -notmatch '^(127\.|169\.254\.)' } | Select-Object -ExpandProperty IPAddress)
Say "    Local:   http://127.0.0.1:$Port/" Green
foreach ($ip in $ips) { Say "    Retea:   http://${ip}:$Port/" Green }
foreach ($r in $results) {
    if ($r.R.Ok) {
        $preset = switch ($r.T.Vendor) { "Intel" { "Stream RTMP - Intel QuickSync 720p / SD 576p" } "AMD" { "Stream RTMP - AMD AMF 720p / SD 576p" } "NVIDIA" { "H.264 NVENC" } }
        Say ("    Presetare recomandata ({0}): {1}" -f $r.T.Vendor, $preset) Green
    }
}
Start-Sleep -Seconds 2
Start-Process $Exe -WorkingDirectory $Dir
if (-not $Silent) { Read-Host "Apasa Enter pentru a inchide" }
