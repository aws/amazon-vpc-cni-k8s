# Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#	http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.
[cmdletbinding()]
Param (
    [Parameter(Mandatory=$false)]
    [ValidateNotNullOrEmpty()]
    [string]$Path = "$($PSScriptRoot)",

    [Parameter(Mandatory=$false)]
    [ValidateSet("Manual","AutomaticDelayedStart","Disabled")]
    [string]$StartupType = 'Manual'
)
Begin {
    $Script:ServiceName = "AmazonECS"
    if (Get-Service | ?{$_.Name -eq "$($Script:ServiceName)"}) {
        Write-Host "The $($Script:ServiceName) service already exists."
        return
    }
    Function Initialize-Directory {
        Param (
            [string]$Path,
            [string]$Name
        )
        [string]$targetDir = Join-Path "$($Path)" "$($Name)"
        if (-Not (Test-Path $targetDir -ErrorAction:Ignore)) {
            Write-Verbose "creating directory: $($targetDir)"
            New-Item -Path $($targetDir) -ItemType:Directory -Force -ErrorAction:Continue | Out-Null
        } else {
            Write-Verbose "directory found: $($targetDir)"
        }
        return $targetDir
    }

    #
    # Setup Default Directories
    #
    Write-Verbose "Setting up default directories"
    # C:\ProgramData\Amazon
    [string]$Script:AmazonProgramData = Initialize-Directory -Path $ENV:ProgramData -Name "Amazon"
    # C:\ProgramData\Amazon\ECS
    [string]$Script:ECSProgramData = Initialize-Directory -Path $Script:AmazonProgramData -Name  "ECS"
    # C:\ProgramData\Amazon\ECS\data
    [string]$Script:ECSData = Initialize-Directory -Path $Script:ECSProgramData -Name  "data"
    # C:\ProgramData\Amazon\ECS\log
    [string]$Script:ECSLogs = Initialize-Directory -Path $Script:ECSProgramData -Name  "log"

    # C:\Program Files\Amazon
    [string]$Script:AmazonProgramFiles = Initialize-Directory -Path $ENV:ProgramFiles -Name "Amazon"
    # C:\Program Files\Amazon\ECS
    [string]$Script:ECSProgramFiles = Initialize-Directory -Path $Script:AmazonProgramFiles -Name  "ECS"

    if (-not (Test-Path $Path)) {
        Throw "The destination path provided does not exist: $($Path)"
        return
    }
}
Process {
    #
    # Service Config CONSTANTS
    #
    [int]$SERVICE_FAILURE_RESTART_DELAY_RESET_SEC = 300
    [int]$SERVICE_FAILURE_FIRST_DELAY_MS = 5000
    [int]$SERVICE_FAILURE_SECOND_DELAY_MS = 30000
    [int]$SERVICE_FAILURE_DELAY_MS = 60000

    #
    # Check for agent
    #
    $ECS_EXE = Join-Path (Get-Item $Path).FullName 'amazon-ecs-agent.exe'
    if(-not (Test-Path $ECS_EXE)) {
        Throw "Failed to find agent `"$($ECS_EXE)`""
        return
    }

    #
    # Setup Default Agent Settings
    #
    try {
        [Environment]::SetEnvironmentVariable("ECS_LOGFILE", "$($Script:ECSLogs)\ecs-agent.log", "Machine")
        [Environment]::SetEnvironmentVariable("ECS_DATADIR", "$($Script:ECSData)", "Machine")
    } catch {
        Write-Host "Failed to set agent default configuration."
        Throw $_
    }

    #
    # Create Service
    #
    try {
        $ServiceParams = @{
            Name = "$($Script:ServiceName)";
            BinaryPathName = "$($ECS_EXE) -windows-service";
            DisplayName = "Amazon ECS";
            Description = "The $($Script:ServiceName) service runs the Amazon ECS agent";
            DependsOn = 'Docker';
        }
        if ($StartupType -eq 'AutomaticDelayedStart') {
            $ServiceParams += @{
                StartupType = 'Automatic';
            }
        } else {
            $ServiceParams += @{
                StartupType = $StartupType;
            }
        }
        Write-Verbose "Creating Service:"
        Write-Verbose "$($ServiceParams | Out-String)"
        $newService = New-Service @ServiceParams
        if ($StartupType -eq 'AutomaticDelayedStart') {
            Write-Verbose "Setting 'Automatic' StartupType to 'Delayed-Auto'"
            $config = sc.exe config $($Script:ServiceName) Start= Delayed-Auto
            Write-Verbose $config
        }
        Write-Verbose "Setting Service Restart Configuration:"
        Write-Verbose "    reset=300"
        Write-Verbose $("    actions=restart/$($SERVICE_FAILURE_FIRST_DELAY_MS)/" + `
                                    "restart/$($SERVICE_FAILURE_SECOND_DELAY_MS)/" + `
                                    "restart/$($SERVICE_FAILURE_DELAY_MS)")
        $failure = sc.exe failure $($Script:ServiceName) reset=300 actions=restart/$($SERVICE_FAILURE_FIRST_DELAY_MS)/restart/$($SERVICE_FAILURE_SECOND_DELAY_MS)/restart/$($SERVICE_FAILURE_DELAY_MS)
        Write-Verbose $failure
        $failureflag = sc.exe failureflag $($Script:ServiceName) 1
        Write-Verbose $failureflag
    } catch {
        Write-Host "Failed to create windows service for ECS agent"
        Throw $_
        return
    }
    return $newService
} End { }