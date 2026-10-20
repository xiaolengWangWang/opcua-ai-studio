param([string]$Version='0.6.1',[string]$CompilerRoot='D:\tools\winlibs-gcc16\mingw64')
$ErrorActionPreference='Stop'
if ($Version -notmatch '^\d+\.\d+\.\d+$') { throw 'Version must be a numeric major.minor.patch' }
$repoRoot=Split-Path -Parent $PSScriptRoot
$guiRoot=Join-Path $repoRoot 'dist\windows-gui'
$stage=Join-Path $guiRoot "OPCUAAIStudio-$Version-Windows-x64"
$zip="$stage.zip"
$gcc=Join-Path $CompilerRoot 'bin\gcc.exe'
if (-not (Test-Path -LiteralPath $gcc)) { $command=Get-Command gcc.exe -ErrorAction SilentlyContinue; if(-not $command){throw 'MinGW x64 gcc.exe is required; pass -CompilerRoot'}; $gcc=$command.Source }
$old=@{};foreach($name in @('PATH','CC','CGO_ENABLED','GOOS','GOARCH')){$old[$name]=[Environment]::GetEnvironmentVariable($name)}
$syso=Join-Path $repoRoot 'cmd\opcua-ai\rsrc_windows_amd64.syso'
function Invoke-Native([scriptblock]$Action) { $previous=$ErrorActionPreference; $ErrorActionPreference='Continue'; & $Action 2>&1 | ForEach-Object { $_.ToString() }; $code=$LASTEXITCODE; $ErrorActionPreference=$previous; if($code -ne 0){throw "Native command failed ($code)"} }
function Write-PackageText([string]$Path, [string]$Text, [Text.Encoding]$Encoding) {
 $temporary="$Path.$PID.tmp"
 [IO.File]::WriteAllText($temporary,$Text,$Encoding)
 Move-Item -LiteralPath $temporary -Destination $Path -Force
}
Push-Location $repoRoot
try {
 New-Item -ItemType Directory -Force -Path $stage | Out-Null
 # go:embed ships files as go reads them; encrypted help would show as ciphertext.
 # Run without cgo: security software blocks freshly linked MinGW test binaries.
 $env:CGO_ENABLED='0'; Invoke-Native {go test -count=1 ./assets/help}
 $env:PATH=(Split-Path $gcc)+';'+$env:PATH; $env:CC=$gcc; $env:CGO_ENABLED='1';$env:GOOS='windows';$env:GOARCH='amd64'
 $machine=(& $gcc -dumpmachine).Trim();if($machine -ne 'x86_64-w64-mingw32'){throw "Compiler target is $machine"}
 # Patch resources after linking to avoid colliding with MinGW/Fyne manifests.
 if(Test-Path -LiteralPath $syso){Remove-Item -LiteralPath $syso}
 $exe=Join-Path $stage 'OPCUAAIStudio.exe'
 Invoke-Native {go build -trimpath -tags no_emoji -ldflags "-s -w -H windowsgui -X main.version=$Version -extldflags=-static" -o $exe ./cmd/opcua-ai}
 Invoke-Native {go run github.com/tc-hib/go-winres@v0.3.3 patch --no-backup --in assets/icon/windows/winres.json --product-version $Version --file-version "$Version.0" $exe}
 $mainExeBytes=(Get-Item -LiteralPath $exe).Length
 if($mainExeBytes -gt 30000000){throw "OPCUAAIStudio.exe exceeds 30 MB (30000000 bytes): $mainExeBytes bytes"}
 Invoke-Native {go build -trimpath -ldflags '-s -w' -o (Join-Path $stage 'opcua-cli.exe') ./cmd/opcua-cli}
 Invoke-Native {go build -trimpath -ldflags '-s -w' -o (Join-Path $stage 'opcua-sim.exe') ./cmd/opcua-sim}
 $readme=Get-Content -LiteralPath platform/windows/README-gui.template -Encoding UTF8 -Raw
 $readme=$readme.Replace('{{VERSION}}',$Version)
 if($readme.Contains('E-SafeNet') -or $readme.Contains([char]0) -or $readme.Contains('{{VERSION}}')) {throw 'Invalid package README'}
 Write-PackageText (Join-Path $stage 'README.txt') ($readme -replace '\r?\n',"`r`n") ([Text.UTF8Encoding]::new($true))
 Write-PackageText (Join-Path $stage 'VERSION.txt') "$Version`r`n" ([Text.UTF8Encoding]::new($true))
 Add-Type -AssemblyName System.IO.Compression
 Add-Type -AssemblyName System.IO.Compression.FileSystem
 function New-PackageZip([string]$Folder, [string]$Path, [string[]]$Names) {
  if(Test-Path -LiteralPath $Path){Remove-Item -LiteralPath $Path}
  $zipStream=[IO.File]::Open($Path,[IO.FileMode]::Create)
  $writer=[IO.Compression.ZipArchive]::new($zipStream,[IO.Compression.ZipArchiveMode]::Create)
  try {
   foreach($name in $Names) {
    $entry=$writer.CreateEntry("$([IO.Path]::GetFileName($Folder))/$name",[IO.Compression.CompressionLevel]::Optimal)
    $input=[IO.File]::OpenRead((Join-Path $Folder $name));$output=$entry.Open()
    try {$input.CopyTo($output)} finally {$input.Dispose();$output.Dispose()}
   }
  } finally {$writer.Dispose();$zipStream.Dispose()}
 }
 # The full package stays: installed copies update from it and need every file.
 New-PackageZip $stage $zip @('OPCUAAIStudio.exe','opcua-cli.exe','opcua-sim.exe','README.txt','VERSION.txt')
 # Separate downloads: the desktop program as one file, and the command-line tools.
 $standalone=Join-Path $guiRoot "OPCUAAIStudio-$Version-Windows-x64.exe"
 Copy-Item -LiteralPath $exe -Destination $standalone -Force
 $toolsStage=Join-Path $guiRoot "opcua-tools-$Version-Windows-x64"
 $toolsZip="$toolsStage.zip"
 New-Item -ItemType Directory -Force -Path $toolsStage | Out-Null
 foreach($name in @('opcua-cli.exe','opcua-sim.exe','VERSION.txt')){Copy-Item -LiteralPath (Join-Path $stage $name) -Destination $toolsStage -Force}
 $toolsReadme=(Get-Content -LiteralPath platform/windows/README-tools.template -Encoding UTF8 -Raw).Replace('{{VERSION}}',$Version)
 if($toolsReadme.Contains('E-SafeNet') -or $toolsReadme.Contains([char]0) -or $toolsReadme.Contains('{{VERSION}}')) {throw 'Invalid tools README'}
 Write-PackageText (Join-Path $toolsStage 'README.txt') ($toolsReadme -replace '\r?\n',"`r`n") ([Text.UTF8Encoding]::new($true))
 New-PackageZip $toolsStage $toolsZip @('opcua-cli.exe','opcua-sim.exe','README.txt','VERSION.txt')
 $tools=[IO.Compression.ZipFile]::OpenRead($toolsZip)
 try {$names=@($tools.Entries | ForEach-Object {[IO.Path]::GetFileName($_.FullName)});if(($names -join ',') -ne 'opcua-cli.exe,opcua-sim.exe,README.txt,VERSION.txt'){throw "Tools archive holds $($names -join ',')"}} finally {$tools.Dispose()}
 $archive=[IO.Compression.ZipFile]::OpenRead($zip)
 try {$readmeEntry=$archive.Entries | Where-Object {$_.FullName.Replace('\','/').EndsWith('/README.txt')};if(-not $readmeEntry){throw 'Archive README is missing'};$reader=[IO.StreamReader]::new($readmeEntry.Open(),[Text.Encoding]::UTF8);try{$packaged=$reader.ReadToEnd();if(-not $packaged.Contains($Version) -or -not $packaged.Contains('50 MB') -or $packaged.Contains('E-SafeNet') -or $packaged.Contains([string][char]0)){throw 'Archive README verification failed'}}finally{$reader.Dispose()}}finally{$archive.Dispose()}
 $hash=(Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash.ToLowerInvariant()
 $standaloneHash=(Get-FileHash -LiteralPath $standalone -Algorithm SHA256).Hash.ToLowerInvariant()
 $toolsHash=(Get-FileHash -LiteralPath $toolsZip -Algorithm SHA256).Hash.ToLowerInvariant()
 # The updater looks up its package's line, so further lines are safe for installed copies.
 $sums=@("$hash  $([IO.Path]::GetFileName($zip))","$standaloneHash  $([IO.Path]::GetFileName($standalone))","$toolsHash  $([IO.Path]::GetFileName($toolsZip))") -join "`n"
 Write-PackageText (Join-Path $guiRoot 'SHA256SUMS.txt') "$sums`n" ([Text.UTF8Encoding]::new($false))
 $sourceCommit=(git rev-parse HEAD | Out-String).Trim()
 if($LASTEXITCODE -ne 0){throw 'Cannot determine build source commit'}
 $manifest=@{version=$Version;fileVersion="$Version.0";commit=$sourceCommit;dirty= -not [string]::IsNullOrWhiteSpace((git status --porcelain | Out-String));compiler=$gcc;zip=[IO.Path]::GetFileName($zip);sha256=$hash;exeSha256=(Get-FileHash -LiteralPath $exe -Algorithm SHA256).Hash.ToLowerInvariant();exeBytes=$mainExeBytes;exeLimitBytes=30000000;bytes=(Get-Item -LiteralPath $zip).Length;standaloneExe=[IO.Path]::GetFileName($standalone);standaloneSha256=$standaloneHash;toolsZip=[IO.Path]::GetFileName($toolsZip);toolsSha256=$toolsHash;toolsBytes=(Get-Item -LiteralPath $toolsZip).Length}
 [IO.File]::WriteAllText((Join-Path $guiRoot 'build-manifest.json'),($manifest | ConvertTo-Json),[Text.UTF8Encoding]::new($false))
 Write-Output $zip; Write-Output "SHA256 $hash"; Write-Output $standalone; Write-Output $toolsZip
} finally {
 if(Test-Path -LiteralPath $syso){Remove-Item -LiteralPath $syso}
 foreach($name in $old.Keys){[Environment]::SetEnvironmentVariable($name,$old[$name])};Pop-Location
}
