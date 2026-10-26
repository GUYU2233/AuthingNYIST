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
 public delegate bool EnumProc(IntPtr h, IntPtr p);
 [DllImport("user32.dll")] public static extern bool EnumChildWindows(IntPtr h,EnumProc callback,IntPtr p);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern int GetClassName(IntPtr h,System.Text.StringBuilder text,int max);
 public static IntPtr FindChild(IntPtr parent,string cls,string caption) {
   IntPtr found=IntPtr.Zero;
   EnumChildWindows(parent,(h,p)=> {
     var c=new System.Text.StringBuilder(256); var t=new System.Text.StringBuilder(512);
     GetClassName(h,c,c.Capacity);
     // GetWindowText cannot read another process's child-control text.
     // WM_GETTEXT is marshalled by Windows for these standard controls.
     ComboText(h,0x000D,(IntPtr)t.Capacity,t);
     // PowerShell converts $null to an empty string for string parameters.
     if ((string.IsNullOrEmpty(cls) || string.Equals(c.ToString(),cls,StringComparison.OrdinalIgnoreCase)) && (string.IsNullOrEmpty(caption) || t.ToString()==caption)) { found=h; return false; }
     return true;
   },IntPtr.Zero);
   return found;
 }
}
'@
$config = Join-Path $env:TEMP ('nyist-ui-smoke-' + [guid]::NewGuid().ToString('N') + '.json')
$p = $null
try {
    $running = Get-Process -Name ([IO.Path]::GetFileNameWithoutExtension($Exe)) -ErrorAction SilentlyContinue | Where-Object { $_.SessionId -eq [Diagnostics.Process]::GetCurrentProcess().SessionId }
    if ($running) { throw 'An existing GUI owns the session. Exit it from the tray before running smoke tests; it will not be terminated automatically.' }
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
    # A collapsed ComboBox does not expose every item through UIA on Server.
    # Read the native model without opening a popup or changing the network.
    $comboHandle = [NativeSmoke]::FindChild($handle,'ComboBox',$null)
    if ($comboHandle -eq [IntPtr]::Zero) { throw 'Native network ComboBox missing' }
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
    foreach ($label in @('启动与后台','高优先级自启动（无需登录系统）','登录系统时自动启动','启动时自动连接校园网','启动时最小化到托盘','后台运行（关闭窗口时保留托盘）','保存设置','连接','断开认证')) {
        if ([NativeSmoke]::FindChild($handle,$null,$label) -eq [IntPtr]::Zero) { throw "Missing control: $label" }
    }
    if ([NativeSmoke]::FindChild($handle,$null,'密码由 Windows 加密保存；退出不会注销校园网。') -ne [IntPtr]::Zero) { throw 'Removed password-storage hint is still visible' }
    'PASS grouped native controls; obsolete password hint removed'
    $null = [NativeSmoke]::SendMessage($handle,0x10,[IntPtr]::Zero,[IntPtr]::Zero)
    if ([NativeSmoke]::IsWindowVisible($handle)) { throw 'Close did not hide window' }
    $p.Refresh(); if ($p.HasExited) { throw 'Background close terminated process' }
    'PASS close hides window and retains process'
    # Simulate the same callback delivered by the notification-area left click.
    $null = [NativeSmoke]::SendMessage($handle,0x8000,[IntPtr]::Zero,[IntPtr]0x201)
    if (-not [NativeSmoke]::IsWindowVisible($handle)) { throw 'Tray callback did not open window' }
    'PASS tray left-click opens window'
    $checkbox = [NativeSmoke]::FindChild($handle,'Button','后台运行（关闭窗口时保留托盘）')
    if ($checkbox -eq [IntPtr]::Zero) { throw 'Native background checkbox missing' }
    # BM_CLICK invokes the actual click handler; BM_SETCHECK alone would not.
    $null = [NativeSmoke]::SendMessage($checkbox,0xF5,[IntPtr]::Zero,[IntPtr]::Zero)
    # Verify the resulting state before closing the window.
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while ([NativeSmoke]::SendMessage($checkbox,0xF0,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32() -ne 0) {
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
