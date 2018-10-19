import org.jenkinsci.plugins.pipeline.github.trigger.IssueCommentCause

@Library('pipeline-lib') _

def MAIN_BRANCH                    = 'build'
def DOCKER_REPOSITORY_NAME         = 'amazon-k8s-cni'
def DOCKER_REGISTRY_URL            = 'https://662491802882.dkr.ecr.us-east-1.amazonaws.com'
def DOCKER_REGISTRY_CREDENTIALS_ID = 'ecr:us-east-1:ecr-docker-push'
def PROJECT_PATH                   = 'src/github.com/aws/amazon-vpc-cni-k8s'

properties([
    pipelineTriggers([issueCommentTrigger('!build')])
])
def isForcePublish = !!currentBuild.rawBuild.getCause(IssueCommentCause)

withResultReporting(slackChannel: '#tm-is', mainBranch: MAIN_BRANCH) {
  inDockerAgent(containers: [interactiveContainer(name: 'go', image: 'golang:1.10')]) {
    def version
    def dockerImage

    stage('Build docker image') {
      def gopath = pwd()
      dir(PROJECT_PATH) {
        checkout([
          $class: 'GitSCM',
          branches: scm.branches,
          doGenerateSubmoduleConfigurations: scm.doGenerateSubmoduleConfigurations,
          extensions: scm.extensions + [[$class: 'CloneOption', noTags: false, shallow: false, depth: 0, reference: '']],
          userRemoteConfigs: scm.userRemoteConfigs,
        ])
        version = sh(returnStdout: true, script: 'git describe --tags --always --dirty').trim()
        container('go') {
          withEnv(["GOPATH=${gopath}"]) {
            sh('make build-linux && make download-portmap')
          }
        }
        dockerImage = docker.build(DOCKER_REPOSITORY_NAME, '-f scripts/dockerfiles/Dockerfile.release .')
      }
    }
    if (BRANCH_NAME == MAIN_BRANCH || isForcePublish) {
      stage('Publish docker image') {
        docker.withRegistry(DOCKER_REGISTRY_URL, DOCKER_REGISTRY_CREDENTIALS_ID) {
          echo("Publishing docker image ${dockerImage.imageName()} with tag ${version}")
          dockerImage.push("${version}")
        }
        if (isForcePublish) {
          pullRequest.comment("Built and published ${version}")
        }
      }
    }
  }
}
