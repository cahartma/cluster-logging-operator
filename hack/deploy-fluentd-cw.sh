#!/bin/bash
set -e

# colors
BLACK='\033[0;30m'
GRAY='\033[1;30m'
RED='\033[0;31m'
GREEN='\033[0;32m'
LGREEN='\033[1;32m'
ORANGE='\033[0;33m'
LORANGE='\033[1;33m'
BLUE='\033[0;34m'
LBLUE='\033[1;34m'
VIOLET='\033[0;35m'
AQUA='\033[0;36m'
WHITE='\033[0;37m'
BOLDWHITE='\033[1;37m'
NC='\033[0m'

TEST_SECRET_NAME="cw-secret-test"
ME=$(basename "$0")
KERBEROS_USERNAME=${KERBEROS_USERNAME:-$(whoami)}
HACK_DIR=${HACK_DIR:-"$(pwd)/hack"}
LOCAL_CONFIG=${LOCAL_CONFIG:-${HACK_DIR}/fluent.config}
CLUSTER_NAME_DATE=${KERBEROS_USERNAME}-$(date +'%m%d')
HOUR_MIN=${HOUR_MIN}:-$(date +'%H%M')
REGION=${REGION:-"us-east-2"}

usage() {
  echo -e "${LBLUE}"
	cat <<-EOF

	----------------------------------------------------------------------------------------------
	Fluentd Tooling for a logging and cloudwatch forwarding
	  - creates and applies resources to facilitate a simple test of cloudwatch forwarding

	Usage:
	  ${ME} [flags]

	Flags:
	  -i, --create-instance       Create a clusterlogging instance with fluentd
	  -f, --create-forwarder      Create a clusterlogforwarder instance to cloudwatch
	  -d, --describe-config       Describe the configmap
	  -c, --grep-config           Grep the configmap for cloudwatch pipeline
	  -e, --extract-config        Extract the configmap from the collector (fluent.conf)
	  -g, --describe-groups       Describe aws log groups
	  -s, --describe-streams      Describe aws log stream by type. for example: "${ME} -s audit"
	  -p, --patch-instance        Edit clusterlogging instance and change to Unmanaged
	  -x, --delete-logging        Delete clusterlogging and clf instance
	  -h, --help                  Help for ${ME}
	EOF
	echo -e "${NC}"
}

main() {
  CREATE_INSTANCE=0
  CREATE_FORWARDER=0
  PATCH_CL_ONLY=0
  while [[ $# -gt 0 ]]; do
    key="$1"
    logtype="$2"
    case $key in
      -e|--extract-config)
        extract_collector_config && exit 0
        ;;
      -c|--grep-config)
        grep_collector_config && exit 0
        ;;
      -d|--describe-config)
        describe_collector_config && exit 0
        ;;
      -g|--describe-groups)
        describe_log_groups && exit 0
        ;;
      -s|--describe-streams)
        [[ -v logtype ]] && describe_log_streams $logtype && exit 0
        ;;
      -p|--patch-only)
        PATCH_CL_ONLY=1
        shift # past argument
        ;;
      -i|--create-instance)
        CREATE_INSTANCE=1
        shift # past argument
        ;;
      -f|--create-forwarder)
        CREATE_FORWARDER=1
        shift # past argument
        ;;
      -h|--help)
         usage && exit 0
         ;;
      -x|--delete-logging)
         oc delete clusterlogging instance && oc delete clf instance
         echo
         exit 0
         ;;
      *)
        echo -e "${RED}Unknown flag $1${NC}" > /dev/stderr
        echo
        usage
        exit 1
        ;;
    esac
  done

  if [[ "${CREATE_INSTANCE}" -eq 1 ]]; then
      echo -e "creating fluentd instance of clusterlogging"
      clusterlogging_instance | oc apply -f -
      echo
      exit 0
  fi

  if [[ "${CREATE_FORWARDER}" -eq 1 ]]; then
      echo -e "creating aws credentials secret"
      oc apply -f ${HACK_DIR}/${TEST_SECRET_NAME}.yaml

      echo -e "creating instance of fluentd cloudwatch forwarder"
      clusterlogforwarder_instance | oc apply -f -
      echo
      exit 0
  fi

  if [[ "${PATCH_CL_ONLY}" -eq 1 ]]; then
      patch_cl_instance
      oc delete pods -lcomponent=collector
      echo
      exit 0
  fi

  usage
  echo
  exit 0
}

describe_log_groups() {
  aws logs describe-log-groups --log-group-name-prefix ${CLUSTER_NAME_DATE} --query logGroups[].logGroupName --output json
}

describe_log_streams() {
  logtype=$1
  echo "aws logs describe-log-streams --log-group-name ${CLUSTER_NAME_DATE}.$logtype--query logStreams[].logStreamName --output json"
  aws logs describe-log-streams --log-group-name ${CLUSTER_NAME_DATE}.$logtype --query logStreams[].logStreamName --output json

}

patch_cl_instance() {
  echo -e "editing clusterlogging instance to: \"Unmanaged\""
  oc patch clusterlogging instance -p '{"spec":{"managementState":"Unmanaged"}}' --type merge
  echo
}

extract_collector_config() {
  echo -e "extracting cluster configmap/collector to local hack dir"
  oc extract configmap/collector --keys=fluent.conf --to=${HACK_DIR} --confirm
}

grep_collector_config() {
  echo -e "grep the collector configmap for cloudwatch pipeline"
  oc describe configmap collector | grep -A14 cloudwatch
  echo
}

describe_collector_config() {
  echo -e "describe the collector configmap"
  oc describe configmap collector
  echo
}

clusterlogging_instance() {
  cat <<-EOF
apiVersion: "logging.openshift.io/v1"
kind: "ClusterLogging"
metadata:
  name: "instance"
  namespace: "openshift-logging"
spec:
  managementState: Managed
  logStore:
    type: "elasticsearch"
    elasticsearch:
      nodeCount: 3
      resources:
        requests:
          limits: 2Gi
      redundancyPolicy: "ZeroRedundancy"
  visualization:
    type: "kibana"
    kibana:
      replicas: 1
  collection:
    logs:
      type: "fluentd"
      fluentd: {}
EOF
}

clusterlogforwarder_instance() {
  cat <<-EOF
apiVersion: logging.openshift.io/v1
kind: ClusterLogForwarder
metadata:
  name: instance
  namespace: openshift-logging
spec:
  outputs:
    - name: cw
      type: cloudwatch
      cloudwatch:
        groupBy: logType
        groupPrefix: ${CLUSTER_NAME_DATE}-fluent${HOUR_MIN}
        region: ${REGION}
      secret:
        name: ${TEST_SECRET_NAME}
  pipelines:
    - detectMultilineErrors: true
      name: forward-to-cw
      inputRefs:
        - infrastructure
        - application
        - audit
      outputRefs:
        - cw
EOF
}

# ---
# Never put anything below this line

main "$@"