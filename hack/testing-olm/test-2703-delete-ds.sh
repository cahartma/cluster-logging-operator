#!/bin/bash
# Jira LOG-2703 - Collector DaemonSet is not removed when CLF is deleted for fluentd/vector only CL instance

set -euo pipefail

source "$(dirname $0)/../common"

mkdir -p /tmp/artifacts/junit
os::test::junit::declare_suite_start "[ClusterLogging] Remove Logging Daemonset"

start_seconds=$(date +%s)

GINKGO_OPTS=${GINKGO_OPTS:-""}
CLF_INCLUDES=${CLF_INCLUDES:-}
export CLUSTER_LOGGING_OPERATOR_NAMESPACE="openshift-logging"
NAMESPACE="${CLUSTER_LOGGING_OPERATOR_NAMESPACE}"
TIMEOUT=$minute
INTERVAL=3
SECRET_NAME=${SECRET_NAME:-"cw-test-secret"}
COLLECTOR=${COLLECTOR:-"vector"}

cleanup(){
  local return_code="$?"

  os::test::junit::declare_suite_end

  set +e
  os::log::info "Running cleanup"
  end_seconds=$(date +%s)
  runtime="$(($end_seconds - $start_seconds))s"

  if [ "${DO_CLEANUP:-false}" == "true" ] ; then
    make undeploy-all
  fi

  set -e
  exit ${return_code}
}
trap cleanup exit

if [ "${DO_SETUP:-false}" == "true" ] ; then
  make deploy
fi

reset_logging(){
  oc delete --ignore-not-found --force --grace-period=5 -n ${NAMESPACE} "clusterlogging/instance" "clf/instance"||:
}

clusterlogging_instance_no_store() {
  cat <<-EOF
---
apiVersion: "logging.openshift.io/v1"
kind: ClusterLogging
metadata:
  name: instance
  namespace: ${NAMESPACE}
spec:
  collection:
    type: ${COLLECTOR}
EOF
}

vector_cw_secret() {
  # fake credentials
  cat <<-EOF
---
apiVersion: v1
kind: Secret
metadata:
  name: ${SECRET_NAME}
  namespace: ${NAMESPACE}
data:
  aws_access_key_id: dGVzdAo=
  aws_secret_access_key: dGVzdAo=
EOF
}

default_clf() {
  cat <<-EOF
---
apiVersion: "logging.openshift.io/v1"
kind: ClusterLogForwarder
metadata:
  name: instance
  namespace: ${NAMESPACE}
spec:
  outputs:
    - name: cw
      type: cloudwatch
      cloudwatch:
        region: us-east-1
      secret:
        name: ${SECRET_NAME}
  pipelines:
    - name: test-logs
      inputRefs:
        - infrastructure
        - audit
      outputRefs:
        - cw
EOF
}

test_log_2703() {
  echo
  os::log::info "Removing any existing logging instances..."
  reset_logging

  os::log::info "=========================================================="
  os::log::info "============      TESTING LOG-2703      =================="
  os::log::info "=========================================================="

  os::log::info "Creating a CloudWatch secret for the forwarder..."
  vector_cw_secret | oc apply -f -

  os::log::info "Creating ClusterLogForwarder instance..."
  default_clf | oc apply -f -

  os::log::info "Creating the ClusterLogging instance with no default logStore..."
  clusterlogging_instance_no_store | oc apply -f -

  os::log::info "Waiting for collector DaemonSet to be ready..."
  sleep 5
  os::cmd::try_until_text "oc -n $NAMESPACE get ds/collector -o jsonpath={.status.numberReady} --ignore-not-found" "6" ${TIMEOUT} ${INTERVAL}

  os::log::info "Verifying that CollectorDeadEnd is currently false..."
  os::cmd::try_until_text "oc -n $NAMESPACE get clusterlogging/instance -o 'jsonpath={.status.conditions[?(@.type=="CollectorDeadEnd")].status}' --ignore-not-found" "False" ${TIMEOUT} ${INTERVAL}

  os::log::info "=========================================================="
  os::log::info "Log forwarding is configured, ready to proceed with test..."
  os::log::info "=========================================================="

  os::log::info "Deleting the ClusterLogForwarder instance..."
  os::cmd::try_until_text "oc -n $NAMESPACE delete clf/instance --ignore-not-found" 'clusterlogforwarder.logging.openshift.io "instance" deleted' ${TIMEOUT} ${INTERVAL}

  os::log::info "Verifying that collector DaemonSet is removed..."
  os::cmd::try_until_failure "oc -n $NAMESPACE get ds/collector"

  os::log::info "Verifying status that CollectorDeadEnd is now true..."
  os::cmd::try_until_text "oc -n $NAMESPACE get clusterlogging/instance -o 'jsonpath={.status.conditions[?(@.type=="CollectorDeadEnd")].status}' --ignore-not-found" "True" ${TIMEOUT} ${INTERVAL}

  os::log::info "================================================================"
  os::log::info "*** TEST PASSED *** LOG-2703 Verified"
  os::log::info "================================================================"

  exit 0
}

# Run the test
failed=0
test_log_2703
failed=$?
reset_logging
exit $failed
