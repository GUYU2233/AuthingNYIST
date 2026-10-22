# Native UI smoke test. Uses an isolated config, never clicks Connect or Disconnect.
param([string]$Exe = "$PSScriptRoot\..\dist\nyist-connect.exe")
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class NativeSmoke {
 [DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr h,uint m,IntPtr w,IntPtr l);
 [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
 [DllImport("user32.dll", EntryPoint="SendMessageW", CharSet=CharSet.Unicode)]
 public static extern IntPtr ComboText(IntPtr h,uint m,IntPtr w,System.Text.StringBuilder text);
}
'@
$config = Join-Path $env:TEMP ('nyist-ui-smoke-' + [guid]::NewGuid().ToString('N') + '.json')
$p = $null
try {
    $cfg = @{network='nyist-tec';username='';password='';autoStart=$false;startMinimized=$false;background=$true;autoConnect=$false;host='https://auth.nyist.edu.cn';acId='1'}
    [IO.File]::WriteAllText($config,($cfg | ConvertTo-Json))
    $p = Start-Process -FilePath (Resolve-Path $Exe).Path -ArgumentList @('--config', ('"' + $config + '"')) -PassThru
    if (-not $p.WaitForInputIdle(10000)) { throw 'Application did not become idle' }
    $root = [System.Windows.Automation.AutomationElement]::RootElement
    $condition = New-Object System.Windows.Automation.AndCondition(
        (New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::ProcessIdProperty,$p.Id)),
        (New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::NameProperty,'NYIST Connect · 校园网助手')))
    $deadline = [DateTime]::UtcNow.AddSeconds(15)
    do {
        $window = $root.FindFirst([System.Windows.Automation.TreeScope]::Children,$condition)
        if ($window) { break }
        $p.Refresh()
        if ($p.HasExited) { throw "Application exited before main window (code $($p.ExitCode))" }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $deadline)
    if (-not $window) { throw 'Native main window missing' }
    $handle = [IntPtr]$window.Current.NativeWindowHandle
    $names = $window.FindAll([System.Windows.Automation.TreeScope]::Descendants,[System.Windows.Automation.Condition]::TrueCondition) | ForEach-Object {$_.Current.Name}
    # A collapsed ComboBox does not expose every item through UIA on Server.
    # Read the native model without opening a popup or changing the network.
    $comboCond = New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::ControlTypeProperty,[System.Windows.Automation.ControlType]::ComboBox)
    $combo = $window.FindFirst([System.Windows.Automation.TreeScope]::Descendants,$comboCond)
    if (-not $combo) { throw 'Network ComboBox missing' }
    $comboHandle = [IntPtr]$combo.Current.NativeWindowHandle
    $count = [NativeSmoke]::SendMessage($comboHandle,0x146,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32()
    $networks = @()
    for ($i=0; $i -lt $count; $i++) {
        $text = New-Object System.Text.StringBuilder 256
        $null = [NativeSmoke]::ComboText($comboHandle,0x148,[IntPtr]$i,$text)
        $networks += $text.ToString()
    }
    foreach ($network in @('nyist-tec','nyist-stu')) {
        if ($networks -notcontains $network) { throw "Missing network option: $network" }
    }
    foreach ($label in @('登录 Windows 时自动启动','后台运行（关闭窗口时保留托盘）','保存设置','连接','断开认证')) {
        if ($names -notcontains $label) { throw "Missing control: $label" }
    }
    'PASS native controls'
    $null = [NativeSmoke]::SendMessage($handle,0x10,[IntPtr]::Zero,[IntPtr]::Zero)
    if ([NativeSmoke]::IsWindowVisible($handle)) { throw 'Close did not hide window' }
    $p.Refresh(); if ($p.HasExited) { throw 'Background close terminated process' }
    'PASS close hides window and retains process'
    # Simulate the same callback delivered by the notification-area left click.
    $null = [NativeSmoke]::SendMessage($handle,0x8000,[IntPtr]::Zero,[IntPtr]0x201)
    if (-not [NativeSmoke]::IsWindowVisible($handle)) { throw 'Tray callback did not open window' }
    'PASS tray left-click opens window'
    $window = $root.FindFirst([System.Windows.Automation.TreeScope]::Children,$condition)
    $cbCond=New-Object System.Windows.Automation.PropertyCondition([System.Windows.Automation.AutomationElement]::NameProperty,'后台运行（关闭窗口时保留托盘）')
    $checkbox=$window.FindFirst([System.Windows.Automation.TreeScope]::Descendants,$cbCond)
    $toggle=$checkbox.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern)
    $toggle.Toggle()
    # UI Automation providers may queue Toggle asynchronously. Wait for the
    # observed state instead of racing WM_CLOSE against the checkbox event.
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while ($toggle.Current.ToggleState -ne [System.Windows.Automation.ToggleState]::Off) {
        if ([DateTime]::UtcNow -ge $deadline) { throw 'Background checkbox did not turn off' }
        Start-Sleep -Milliseconds 50
    }
    $null = [NativeSmoke]::SendMessage($handle,0x10,[IntPtr]::Zero,[IntPtr]::Zero)
    if (-not $p.WaitForExit(5000)) { throw 'Close with background disabled did not exit' }
    'PASS close exits when background is disabled'
} finally {
    if ($p -and -not $p.HasExited) { Stop-Process -Id $p.Id }
    # Only remove the exact temporary config created above.
    if (Test-Path -LiteralPath $config) { Remove-Item -LiteralPath $config }
}
