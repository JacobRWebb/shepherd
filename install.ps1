# Install the latest Shepherd release binary on Windows.
#   irm https://raw.githubusercontent.com/JacobRWebb/shepherd/main/install.ps1 | iex
$ErrorActionPreference = 'Stop'

$repo = 'JacobRWebb/shepherd'
$bin  = 'shepherd'

$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    'ARM64' { 'arm64' }
    default { 'amd64' }
}

$rel = Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest"
$tag = $rel.tag_name
if (-not $tag) { throw "Could not determine the latest release; is the repo published?" }
$ver = $tag.TrimStart('v')

$url  = "https://github.com/$repo/releases/download/$tag/${bin}_${ver}_windows_${arch}.zip"
$dest = Join-Path $env:LOCALAPPDATA 'Programs\shepherd'
New-Item -ItemType Directory -Force -Path $dest | Out-Null

$zip = Join-Path $env:TEMP "shepherd-$ver.zip"
Write-Host "Downloading $url"
Invoke-WebRequest -Uri $url -OutFile $zip
Expand-Archive -Path $zip -DestinationPath $dest -Force
Remove-Item $zip -Force

Write-Host "Installed $bin $tag to $dest"
if (($env:Path -split ';') -notcontains $dest) {
    Write-Host "Add '$dest' to your PATH to run 'shepherd'." -ForegroundColor Yellow
}
