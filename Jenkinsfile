pipeline {
    agent any

    tools {
        go 'latest'
    }
    environment {
        GO14MODULE = 'on'
        CGO_ENABLED = 0
        GOPATH = "${JENKINS_HOME}/jobs/${JOB_NAME}/builds/${BUILD_ID}"
        BUILD_DIR = "build"
    }
    options {
        timestamps()
        timeout(time: 15, unit: 'MINUTES')
        buildDiscarder(logRotator(artifactDaysToKeepStr: '30', artifactNumToKeepStr: '10', daysToKeepStr: '30', numToKeepStr: '10'))
    }
    stages {
        stage('Build') {
            steps {
                echo "Compile and build"
                sh(script: "go build", returnStdout: true)
            }
        }

        stage('Test') {
            steps {
                withEnv(["PATH+GO=${GOPATH}/bin"]) {
                    echo "Running vetting"
                    sh(script:"go vet .", returnStdout: true)
                }
            }
        }

        stage('Create RPM') {
            steps {
                sh("mkdir -p ${BUILD_DIR}")

                script {
                    timeout(5) {
                        status = sh(script: "tito build --rpm -o ${BUILD_DIR}", returnStatus: true)
                    }

                    if (status != 0) {
                        error "Can't build RPM artifact"
                    }
                }
            }
        }

        stage('Upload RPM to Repository') {
            steps {
                script {
                    withCredentials([usernamePassword(credentialsId: 'stingrNETrepo', usernameVariable: 'repo_user', passwordVariable: 'repo_pass')]) {
                        response = sh(script: "curl -o /dev/null -w '%{http_code}' http://repo.stingr.net:8284/upload/ --user ${repo_user}:${repo_pass} --upload-file ${BUILD_DIR}/x86_64/contester-advfiler-0.*.x86_64.rpm", returnStdout: true)
                    }

                    if (response != "200") {
                        error "can't upload artifact to repo. Response code is ${response}"
                    }
                }
            }
        }
    }
    post {
        always {
            cleanWs(disableDeferredWipeout: true, deleteDirs: true)
        }
    }
}
