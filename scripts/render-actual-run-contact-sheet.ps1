param(
  [Parameter(Mandatory = $true)]
  [string]$BuildProofPath,

  [Parameter(Mandatory = $true)]
  [string]$KeyCompletionPath,

  [Parameter(Mandatory = $true)]
  [string]$OpCompletionPath,

  [Parameter(Mandatory = $true)]
  [string]$DraftPath,

  [Parameter(Mandatory = $true)]
  [string]$OutputPath
)

$ErrorActionPreference = "Stop"

Add-Type -AssemblyName System.Drawing

function Wrap-Lines {
  param(
    [string[]]$Lines,
    [int]$MaxChars
  )
  $out = New-Object System.Collections.Generic.List[string]
  foreach ($line in $Lines) {
    if ($line.Length -eq 0) {
      $out.Add("")
      continue
    }
    for ($i = 0; $i -lt $line.Length; $i += $MaxChars) {
      $len = [Math]::Min($MaxChars, $line.Length - $i)
      $out.Add($line.Substring($i, $len))
    }
  }
  return $out
}

function Format-Completion {
  param(
    [string]$Command,
    [string]$Path
  )
  $items = Get-Content -LiteralPath $Path -Raw | ConvertFrom-Json
  $lines = New-Object System.Collections.Generic.List[string]
  $lines.Add("PS> $Command")
  foreach ($item in $items) {
    $lines.Add(("{0,-16} -> {1,-18} {2}" -f $item.label, $item.insertText, $item.detail))
    $lines.Add(("  compileDraft: kind={0}; queue={1}; key={2}; reason={3}" -f $item.compileDraft.kind, $item.compileDraft.queue, $item.compileDraft.key, $item.compileDraft.reason))
  }
  return $lines
}

function Format-Draft {
  param(
    [string]$Command,
    [string]$Path
  )
  $lines = New-Object System.Collections.Generic.List[string]
  $lines.Add("PS> $Command")
  foreach ($line in (Get-Content -LiteralPath $Path)) {
    $lines.Add($line)
  }
  return $lines
}

$panels = @(
  @{
    Title = "00 actual Windows CI build/test proof"
    Lines = @("PS> go version / go test ./... / go build / Get-FileHash") + (Get-Content -LiteralPath $BuildProofPath)
  },
  @{
    Title = "01 actual key candidates from --complete '{`"`"'"
    Lines = Format-Completion -Command ".\artifacts\hq-reflective-windows-amd64.exe --complete '{`"`"'" -Path $KeyCompletionPath
  },
  @{
    Title = "02 actual op value candidates from --complete '{`"op`":q'"
    Lines = Format-Completion -Command ".\artifacts\hq-reflective-windows-amd64.exe --complete '{`"op`":q'" -Path $OpCompletionPath
  },
  @{
    Title = "03 actual accepted instruction from --draft"
    Lines = Format-Draft -Command ".\artifacts\hq-reflective-windows-amd64.exe --draft '{`"op`":`"queue.create`",...}'" -Path $DraftPath
  }
)

$font = New-Object System.Drawing.Font("Consolas", 14, [System.Drawing.FontStyle]::Regular, [System.Drawing.GraphicsUnit]::Pixel)
$titleFont = New-Object System.Drawing.Font("Consolas", 15, [System.Drawing.FontStyle]::Bold, [System.Drawing.GraphicsUnit]::Pixel)
$imageWidth = 1280
$outerPad = 22
$panelPad = 14
$lineHeight = [int][Math]::Ceiling($font.GetHeight()) + 4
$titleHeight = [int][Math]::Ceiling($titleFont.GetHeight()) + 10
$maxChars = 132

$renderPanels = New-Object System.Collections.ArrayList
$height = $outerPad
foreach ($panel in $panels) {
  $wrapped = Wrap-Lines -Lines $panel.Lines -MaxChars $maxChars
  $panelHeight = $titleHeight + ($wrapped.Count * $lineHeight) + ($panelPad * 2)
  $entry = @{
    Title = $panel.Title
    Lines = $wrapped
    Height = [Math]::Max(210, $panelHeight)
  }
  [void]$renderPanels.Add($entry)
  $height += $entry.Height + $outerPad
}

$bitmap = New-Object System.Drawing.Bitmap($imageWidth, $height)
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
$graphics.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit

$pageBg = [System.Drawing.Color]::FromArgb(255, 36, 38, 42)
$panelBg = [System.Drawing.Color]::FromArgb(255, 10, 12, 16)
$panelBorder = [System.Drawing.Color]::FromArgb(255, 62, 68, 76)
$titleFg = [System.Drawing.Color]::FromArgb(255, 233, 238, 245)
$textFg = [System.Drawing.Color]::FromArgb(255, 223, 231, 241)
$promptFg = [System.Drawing.Color]::FromArgb(255, 112, 215, 168)

$graphics.Clear($pageBg)
$panelBrush = New-Object System.Drawing.SolidBrush($panelBg)
$borderPen = New-Object System.Drawing.Pen($panelBorder, 1)
$titleBrush = New-Object System.Drawing.SolidBrush($titleFg)
$textBrush = New-Object System.Drawing.SolidBrush($textFg)
$promptBrush = New-Object System.Drawing.SolidBrush($promptFg)

$y = $outerPad
foreach ($panel in $renderPanels) {
  $panelHeight = [int]$panel["Height"]
  $rectWidth = [int]($imageWidth - ($outerPad * 2))
  $rect = New-Object System.Drawing.Rectangle($outerPad, $y, $rectWidth, $panelHeight)
  $graphics.FillRectangle($panelBrush, $rect)
  $graphics.DrawRectangle($borderPen, $rect)
  $graphics.DrawString($panel["Title"], $titleFont, $titleBrush, $outerPad + $panelPad, $y + $panelPad)
  $textY = $y + $panelPad + $titleHeight
  foreach ($line in $panel["Lines"]) {
    $brush = if ($line.StartsWith("PS>")) { $promptBrush } else { $textBrush }
    $graphics.DrawString($line, $font, $brush, $outerPad + $panelPad, $textY)
    $textY += $lineHeight
  }
  $y += $panelHeight + $outerPad
}

$outDir = Split-Path -Parent $OutputPath
if ($outDir) {
  New-Item -ItemType Directory -Force $outDir | Out-Null
}

$bitmap.Save($OutputPath, [System.Drawing.Imaging.ImageFormat]::Png)
$graphics.Dispose()
$bitmap.Dispose()
$font.Dispose()
$titleFont.Dispose()
$panelBrush.Dispose()
$borderPen.Dispose()
$titleBrush.Dispose()
$textBrush.Dispose()
$promptBrush.Dispose()
