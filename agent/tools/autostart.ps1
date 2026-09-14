# Starts the agent automatically when you log on.
#
# Usage:
#   .\autostart.ps1 -Install      register the task and start the agent
#   .\autostart.ps1 -Status       show whether it is registered and running
#   .\autostart.ps1 -Uninstall    remove it
#
# This registers a scheduled task rather than a Windows service. A service runs
# before login with no desktop session, which means no tray icon and no way to
# open the UI or reload the config. The agent is operated from the tray, so it
# has to start inside a logged-on session.

[CmdletBinding(DefaultParameterSetName = 'Status')]
param(
    [Parameter(ParameterSetName = 'Install')][switch]$Install,
    [Parameter(ParameterSetName = 'Uninstall')][switch]$Uninstall,
    [Parameter(ParameterSetName = 'Status')][switch]$Status,

    # Where the agent lives. Defaults to the directory above this script, which
    # is where the build puts it.
    [string]$AgentDir
)

$ErrorActionPreference = 'Stop'

$TaskName = 'spotdash agent'

if (-not $AgentDir) { $AgentDir = Split-Path -Parent $PSScriptRoot }
$exe = Join-Path $AgentDir 'spotdash.exe'
$config = Join-Path $AgentDir 'config.json'

function Get-Task {
    Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
}

function Show-Status {
    $task = Get-Task
    if (-not $task) {
        Write-Host "not registered"
    }
    else {
        $info = Get-ScheduledTaskInfo -TaskName $TaskName
        Write-Host "registered, state $($task.State)"
        Write-Host "  last run    : $($info.LastRunTime)"
        Write-Host "  last result : $($info.LastTaskResult)"
        Write-Host "  next run    : $($info.NextRunTime)"
    }

    $running = Get-Process -Name 'spotdash' -ErrorAction SilentlyContinue
    if ($running) {
        Write-Host "agent running, pid $($running.Id -join ', ')"
    }
    else {
        Write-Host "agent not running"
    }
}

function Install-Task {
    if (-not (Test-Path $exe)) {
        Write-Error "spotdash.exe not found at $exe. Build it first with .\run.ps1, or pass -AgentDir."
    }
    # A task that fails at every logon is worse than no task, and the failure
    # would be invisible because there is no console to print to.
    if (-not (Test-Path $config)) {
        Write-Error "config.json not found at $config. Copy config.example.json to config.json and set a token first."
    }

    $action = New-ScheduledTaskAction -Execute $exe -WorkingDirectory $AgentDir
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME

    $settings = New-ScheduledTaskSettingsSet `
        -AllowStartIfOnBatteries `
        -DontStopIfGoingOnBatteries `
        -StartWhenAvailable `
        -RestartCount 3 `
        -RestartInterval (New-TimeSpan -Minutes 1) `
        -MultipleInstances IgnoreNew
    # Tasks are killed after three days by default, which for something meant to
    # run continuously is a restart at an arbitrary hour with no explanation.
    $settings.ExecutionTimeLimit = 'PT0S'
    # The panel is worth having as soon as the desktop is usable, but not at the
    # cost of slowing down logon.
    $trigger.Delay = 'PT15S'

    # Limited rather than Highest: elevation would put the tray icon in a
    # different integrity level from the desktop and it would not appear.
    $principal = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive -RunLevel Limited

    if (Get-Task) {
        Write-Host "Replacing the existing task"
        Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    }

    Register-ScheduledTask -TaskName $TaskName -Action $action -Trigger $trigger `
        -Settings $settings -Principal $principal `
        -Description 'Starts the spotdash agent at logon so the desk panel comes back after a reboot.' | Out-Null

    Write-Host "Registered '$TaskName'"
    Write-Host "  runs   : $exe"
    Write-Host "  in     : $AgentDir"

    if (Get-Process -Name 'spotdash' -ErrorAction SilentlyContinue) {
        Write-Host "The agent is already running, leaving it alone."
    }
    else {
        Start-ScheduledTask -TaskName $TaskName
        Write-Host "Started it now as well."
    }
}

function Uninstall-Task {
    if (-not (Get-Task)) {
        Write-Host "not registered, nothing to remove"
        return
    }
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
    Write-Host "Removed '$TaskName'. A running agent is left alone; quit it from the tray."
}

switch ($PSCmdlet.ParameterSetName) {
    'Install' { Install-Task }
    'Uninstall' { Uninstall-Task }
    default { Show-Status }
}
