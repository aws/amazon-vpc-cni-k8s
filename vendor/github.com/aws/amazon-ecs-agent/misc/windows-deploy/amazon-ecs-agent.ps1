# Copyright 2014-2016 Amazon.com, Inc. or its affiliates. All Rights Reserved
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


$ecsRootDir = "C:\ProgramData\Amazon\ECS\"

# LogMsg [-message] <String> [-logLevel <String>]
## This function should be used to write a message to the log file so that we have consistent
## formatting and log levels.
## TODO: This is a very basic logging function. We should make it better, or replace it with
## something that knows about about actual log level.
function LogMsg([string]$message = "Logging no message", $logLevel = "INFO") {
     $logdir = $ecsRootDir + "log\win-agent-init.log"
     Add-Content $logdir "$(Get-Date)    [$logLevel] $message"
}

$env:ECS_DATADIR = $ecsRootDir + "data"
$env:ECS_LOGFILE = $ecsRootDir + "log\ecs-agent.log"
$env:ECS_AUDIT_LOGFILE = $ecsRootDir + "log\audit.log"
$env:ECS_LOGLEVEL = "debug"
$env:ECS_AVAILABLE_LOGGING_DRIVERS = "[`"json-file`",`"awslogs`"]"


try {
    $datadir = $ecsRootDir+"data"
    if(!$(Test-Path -Path  $datadir)) {
        mkdir $datadir
    }
    $logdir = $ecsRootDir+"log"
    if(!$(Test-Path -Path $logdir)) {
        mkdir $logdir
    }
} catch  {
    ## Just echo here because log isn't setup/
    echo $_.Exception.Message
    exit 1
}

LogMsg "Checking if docker is running."

try {
    $dockerSrv = Get-Service -Name 'docker'
    $stat = $dockerSrv.WaitForStatus('Running', '00:15:00')

    docker ps
    if (${LastExitCode} -ne 0) {
        LogMsg -message "Docker ps didn't go well." -logLevel "ERROR"
        LogMsg -message $_.Exception.Message -logLevel "ERROR"
        exit 1;
    }

    LogMsg "Docker is running and ready."

    if([System.Environment]::GetEnvironmentVariable("ECS_ENABLE_TASK_IAM_ROLE", "Machine") -eq "true") {
        LogMsg "IAM roles environment variable is set."
        .\hostsetup.ps1
    }

    LogMsg "Starting agent... "
    try {
        .\amazon-ecs-agent.exe
    } catch {
        LogMsg -message "Could not start agent.exe." -logLevel "ERROR"
        LogMsg -message $_.Exception.Message -logLevel "ERROR"
        exit 2
    }
} catch [System.ServiceProcess.TimeoutException] {
    LogMsg -message "Docker not started before timeout." -logLevel "ERROR"
    exit 3
}
