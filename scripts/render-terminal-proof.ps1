param(
  [Parameter(Mandatory = $true)]
  [string]$InputPath,

  [Parameter(Mandatory = $true)]
  [string]$OutputPath
)

$ErrorActionPreference = "Stop"

Add-Type -AssemblyName System.Drawing

$raw = Get-Content -LiteralPath $InputPath -Raw
$lines = $raw -split "`r?`n"

$font = New-Object System.Drawing.Font("Consolas", 14, [System.Drawing.FontStyle]::Regular, [System.Drawing.GraphicsUnit]::Pixel)
$padding = 18
$lineHeight = [int][Math]::Ceiling($font.GetHeight()) + 4
$maxChars = 120
$wrapped = New-Object System.Collections.Generic.List[string]

foreach ($line in $lines) {
  if ($line.Length -eq 0) {
    $wrapped.Add("")
    continue
  }
  for ($i = 0; $i -lt $line.Length; $i += $maxChars) {
    $len = [Math]::Min($maxChars, $line.Length - $i)
    $wrapped.Add($line.Substring($i, $len))
  }
}

$width = 1280
$height = [Math]::Max(360, ($wrapped.Count * $lineHeight) + ($padding * 2))
$bitmap = New-Object System.Drawing.Bitmap($width, $height)
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
$graphics.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit

$background = [System.Drawing.Color]::FromArgb(255, 18, 22, 28)
$foreground = [System.Drawing.Color]::FromArgb(255, 230, 236, 244)
$prompt = [System.Drawing.Color]::FromArgb(255, 114, 214, 167)
$muted = [System.Drawing.Color]::FromArgb(255, 151, 162, 176)

$graphics.Clear($background)
$brush = New-Object System.Drawing.SolidBrush($foreground)
$promptBrush = New-Object System.Drawing.SolidBrush($prompt)
$mutedBrush = New-Object System.Drawing.SolidBrush($muted)

$y = $padding
foreach ($line in $wrapped) {
  if ($line.StartsWith("PS>")) {
    $graphics.DrawString($line, $font, $promptBrush, $padding, $y)
  } elseif ($line.Trim().Length -eq 0) {
    $graphics.DrawString($line, $font, $mutedBrush, $padding, $y)
  } else {
    $graphics.DrawString($line, $font, $brush, $padding, $y)
  }
  $y += $lineHeight
}

$outDir = Split-Path -Parent $OutputPath
if ($outDir) {
  New-Item -ItemType Directory -Force $outDir | Out-Null
}

$bitmap.Save($OutputPath, [System.Drawing.Imaging.ImageFormat]::Png)
$graphics.Dispose()
$bitmap.Dispose()
$font.Dispose()
$brush.Dispose()
$promptBrush.Dispose()
$mutedBrush.Dispose()
