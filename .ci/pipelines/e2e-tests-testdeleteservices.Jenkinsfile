// This library overrides the default checkout behavior to enable sleep+retries if there are errors
// Added to help overcome some recurring github connection issues
// @Library('apm@current') _

def failedTests = []
def lib

pipeline {

    agent {
        label 'linux'
    }

    environment {
        VAULT_ADDR = credentials('vault-addr')
        VAULT_ROLE_ID = credentials('vault-role-id')
        VAULT_SECRET_ID = credentials('vault-secret-id')
        GCLOUD_PROJECT = credentials('k8s-operators-gcloud-project')
    }

    stages {
        stage('Checkout from GitHub') {
            steps {
                checkout scm
            }
        }
        stage('Load common scripts') {
            steps {
                script {
                    lib = load ".ci/common/tests.groovy"
                }
            }
        }
       /* stage('Run Checks') {
            when {
                expression {
                    notOnlyDocs()
                }
            }
            steps {
                sh 'make -C .ci TARGET=ci-check ci'
            }
        }*/
        stage("E2E tests") {
            steps {
                sh '.ci/setenvconfig e2e/master'
                
                sh 'echo TESTS_MATCH = TestDeleteServices >> .env'
                sh 'echo E2E_SKIP_CLEANUP = true >> .env'
                sh 'sed "s/CLUSTER_NAME=eck-e2e/CLUSTER_NAME=eck-debug-endpoints-e2e/" -i .env'
                sh 'sed "s/clusterName: eck-e2e/clusterName: eck-debug-endpoints-e2e/" -i deployer-config.yml'
                
                script {
                    // setup the test environment and run the tests once
                    env.SHELL_EXIT_CODE = sh(returnStatus: true, script: 'make -C .ci get-test-artifacts TARGET=ci-build-operator-e2e-run ci')
                    if (env.SHELL_EXIT_CODE != 0) {
                        sh 'exit $SHELL_EXIT_CODE'
                    }
                    // run the tests again, 20 times
                    for (i = 0; i <20; i++) {
                        env.SHELL_EXIT_CODE = sh(returnStatus: true, script: 'make -C .ci TARGET=e2e-run ci')
                        if (env.SHELL_EXIT_CODE != 0) {
                          sh 'exit $SHELL_EXIT_CODE'
                        }
                    }
                }
            }
        }
    }

    post {
        success {
            build job: 'cloud-on-k8s-e2e-cleanup',
                parameters: [string(name: 'JKS_PARAM_GKE_CLUSTER', value: "eck-debug-endpoints-e2e-${BUILD_NUMBER}")],
                wait: false
             // schedule again
            build job: 'cloud-on-k8s-e2e-tests-testdeleteservices', wait: false
        }
        unsuccessful {
            script {
                def msg = lib.generateSlackMessage("E2E tests failed!", env.BUILD_URL, failedTests)

                slackSend(
                      channel: '#cloud-k8s',
                      color: 'danger',
                      message: msg,
                    tokenCredentialId: 'cloud-ci-slack-integration-token',
                    botUser: true,
                    failOnError: true
                )
            }
        }
        cleanup {
            cleanWs()
        }
    }
}
