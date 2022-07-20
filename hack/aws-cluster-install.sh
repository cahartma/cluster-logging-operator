#!/bin/bash

# Credit to Brett Jones the original author of create-aws-cluster.sh
# most of this is taken directly from that script
# Purpose: Provide a simple/default set of instructions for
# easily creating an OpenShift cluster in sts mode (manual credentials mode)
# Includes flags for creating the clf resources necessary for role based SA tokens

set -ueo pipefail

# colors can be commented out if unwanted
bold=$(tput bold)
red=$(tput setaf 1)
green=$(tput setaf 2)
yellow=$(tput setaf 3)
blue=$(tput setaf 4)
cyan=$(tput setaf 6)
reset=$(tput sgr0)

#Color    Value
#black     0 
#red       1 
#green     2 
#yellow    3 
#blue      4 
#magenta   5 
#cyan      6 
#white     7 

# leave these
read_color() {
  echo
  read -p "${bold}$1${reset}"
}

echo_bold() {
  echo
  echo "${bold}$1${reset}"
}

echo_green() {
  echo "${green}$1${reset}"
}

echo_yellow() {
  echo "${yellow}$1${reset}"
}

echo_cyan() {
  echo "${cyan}$1${reset}"
}

echo_blue() {
  echo "${blue}$1${reset}"
}

#RED='\033[0;31m'
#GREEN='\033[0;32m'
#TAN='\033[0;33m'
#AQUA='\033[0;36m'
#NC='\033[0m'

ME=$(basename "$0")
KERBEROS_USERNAME=${KERBEROS_USERNAME:-$(whoami)}
TMP_DIR=${TMP_DIR:-$HOME/tmp}
CLUSTER_DIR=${CLUSTER_DIR:-${TMP_DIR}/installer}
CCO_UTILITY_DIR=${CCO_UTILITY_DIR:-${TMP_DIR}/cco}
CCO_RESOURCE_NAME=${KERBEROS_USERNAME}-$(date +'%m%d')
CLUSTER_NAME=${KERBEROS_USERNAME}-$(date +'%m%d')cluster$(date +'%H%M')
REGION=${REGION:-"us-east-2"}
SSH_KEY="$(cat $HOME/.ssh/id_ed25519.pub)"
PULL_SECRET="$(tr -d '[:space:]' < $HOME/.docker/config.json)"
SECRET_NAME=${SECRET_NAME:-"vector-cw-secret"}
COLLECTOR=${COLLECTOR:-"vector"}


DEBUG='info'
CONFIG_ONLY=${CONFIG_ONLY:-0}

usage() {
  echo ${blue}
	cat <<-EOF

	Deploy a cluster to AWS with STS enabled via manual mode and the CCO utility

	Usage:
	  ${ME} [flags]

	Flags:
      --cleanup            Destroy existing cluster if necessary and exit
  -d, --dir string         Install Assets directory (default "${CLUSTER_DIR}")
	  -o, --config-only        Create install config only
	  -i, --create-instance    Create a cluster logging instance with vector
	  -f, --logforwarding      Create an instance of logforwarding
	  -c, --collector          Specify collector (default "${COLLECTOR}" or "fluentd")
	  -s, --secret-name        Specify AWS secret name (default "${SECRET_NAME}")
	  -r, --region             Specify AWS region (default "${REGION}")
	  -d, --debug              Use debug log level for install
	  -h, --help               Help for ${ME}
	EOF
  echo ${reset}

#  for (( i = 30; i < 38; i++ )); do
#    echo -e "\033[0;"$i"m Normal: (0;$i); \033[1;"$i"m Light: (1;$i)";
#  done
#  echo -e "$NC"
printf '\e[%smX ' {30..37} 0; echo           ### foreground
printf '\e[%smX ' {40..47} 0; echo           ### background
echo
}

main() {
  CREATE_INSTANCE=0
  CLEANUP_ONLY=0
  while [[ $# -gt 0 ]]; do
    key="$1"
    case $key in
      --cleanup)
        CLEANUP_ONLY=1
        shift # past argument
        ;;
			-d|--dir)
				CLUSTER_DIR="$2"
				shift # past argument
				shift # past value
				;;
      -r|--region)
        REGION="$2"
        shift # past argument
        shift # past value
        ;;
      -c|--collector)
        COLLECTOR="$2"
        shift # past argument
        shift # past value
        ;;
      -s|--secret-name)
        SECRET_NAME="$2"
        shift # past argument
        shift # past value
        ;;
      -o|--config)
        CONFIG_ONLY=1
        shift # past argument
        ;;
      -d|--debug)
        DEBUG='debug'
        shift # past argument
        ;;
      -i|--create-instance)
        CREATE_INSTANCE=1
        shift # past argument
        ;;
      -f|--logforwarding)
        cluster_log_forwarder && exit 0
        ;;
      -h|--help)
        usage && exit 0
        ;;
      *)
        echo -e "${red}Unknown flag $1${reset}" > /dev/stderr
        echo
        usage
        exit 1
        ;;
    esac
  done

  if [ "${CLEANUP_ONLY}" -eq 1 ]; then
      destroy_cluster
      exit 0
  fi

  if [[ "${CREATE_INSTANCE}" -eq 1 ]]; then
      echo -e "creating vector preview of clusterlogging"
      clusterlogging_instance | oc apply -f -
      echo
      exit 0
  fi
  setup

  # exit after setup if config only
  [ $CONFIG_ONLY -eq 1 ] && echo && exit 0

  echo -e "\nSetup is complete and ready to create cluster..."
  create_cluster
}

# Remove existing files and create install config
setup() {
  echo
  echo_cyan "Reminder: VPN must be connected before we start the installer"
  confirm

  if [[ -d ${CLUSTER_DIR} || -d ${CCO_UTILITY_DIR} ]]; then
      echo
      echo_cyan "Existing install or cco utility files need to removed from ${TMP_DIR}"
      confirm
      [ -d ${CLUSTER_DIR} ] && rm -r ${CLUSTER_DIR} && echo "Removing ${CLUSTER_DIR}"
      [ -d ${CCO_UTILITY_DIR} ] && rm -r ${CCO_UTILITY_DIR} && echo "Removing ${CCO_UTILITY_DIR}"
  fi

  make_config
}

make_config() {
  echo -e "\nCreating install config"
  mkdir -p ${CLUSTER_DIR}

	cat <<-EOF > "${CLUSTER_DIR}/install-config.yaml"
	---
	apiVersion: v1
	baseDomain: devcluster.openshift.com
	compute:
	- architecture: amd64
	  hyperthreading: Enabled
	  name: worker
	  platform: {}
	  replicas: 3
	controlPlane:
	  architecture: amd64
	  hyperthreading: Enabled
	  name: master
	  platform: {}
	  replicas: 3
	metadata:
	  creationTimestamp: null
	  name: ${CLUSTER_NAME}
	networking:
	  clusterNetwork:
	  - cidr: 10.128.0.0/14
	    hostPrefix: 23
	  machineNetwork:
	  - cidr: 10.0.0.0/16
	  networkType: OpenShiftSDN
	  serviceNetwork:
	  - 172.30.0.0/16
	platform:
	  aws:
	    region: ${REGION}
	publish: External
	pullSecret: |-
	  ${PULL_SECRET}
	sshKey: |-
	  ${SSH_KEY}
	EOF

  echo "Install config file created at ${CLUSTER_DIR}"
  # create a copy
  cp ${CLUSTER_DIR}/install-config.yaml ${TMP_DIR}/install-config$(date +'%m%d').yaml
  echo "Copy created at ${TMP_DIR}/install-config$(date +'%m%d').yaml"
}

create_cluster() {
  #  just in case
  [ $CONFIG_ONLY -eq 1 ] && exit 0

  confirm

  echo "Creating cluster ${CLUSTER_NAME} at ${CLUSTER_DIR}"
  echo

  if openshift-install create cluster --dir ${CLUSTER_DIR} --log-level=${DEBUG} ; then
    post_install
  else
    _notify_send -t 5000 \
      'FAILED to create cluster' \
      'Errors creating cluster see /.openshift_install.log for details'
    return 1
  fi
}

confirm() {
    echo
    read -p "Do you want to continue (y/N)? " CONT
    if [ "$CONT" != "y" ]; then
      echo "Okay, Exiting."
      exit 0
    fi
    echo

#    while true; do
#      read -r -p "Do you want to continue (y/N)? " answer
#      case $answer in
#          [Yy]* ) break;;
#          [Nn]* ) echo "Okay, Exiting." && exit;;
#          * ) echo "Please answer y or N";;
#      esac
#    done
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
      nodeCount: 1
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
      type: ${COLLECTOR}
      fluentd: {}
EOF
}

cluster_log_forwarder() {
  echo -e "\nReady to install logfowarding resources"
  confirm

  echo -e "\nApplying secret for ${SECRET_NAME}"
  oc apply -f hack/${SECRET_NAME}.yaml

  echo -e "\nCreating logforwarder instance resource file"
  cat <<-EOF > hack/cw-logforwarder.yaml
---
apiVersion: "logging.openshift.io/v1"
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
        groupPrefix: ${CCO_RESOURCE_NAME}
        region: ${REGION}
      secret:
        name: ${SECRET_NAME}
  pipelines:
    - name: all-logs
      inputRefs:
        - infrastructure
        - audit
        - application
      outputRefs:
        - cw
EOF

  echo -e "\nApply logforwarder yaml file"
  oc apply -f hack/cw-logforwarder.yaml

  echo
  exit 0
}

post_install() {
  _notify_send -t 5000 \
    "OCP cluster ${CLUSTER_NAME} " \
    "Created successfully"

  echo_bold "Successfully deployed cluster!"

#  echo_bold "--- Setup ---"
#  echo_cyan "export KUBECONFIG=${CLUSTER_DIR}/auth/kubeconfig"
#  echo_cyan "oc login -u kubeadmin -p \$(cat ${CLUSTER_DIR}/auth/kubeadmin-password)"
#
#  echo_bold "--- Cleanup Commands ---"
#  echo_green "${ME} --cleanup"
#
#  echo_bold "Alternatively, to clean up ccoctl resources after destroying cluster..."
#  echo_green "ccoctl aws delete --name=${CCO_RESOURCE_NAME} --region=${REGION}"
#
#  echo_bold "Cloudwatch log groups..."
##  echo_green "aws logs describe-log-groups --query 'logGroups[?starts_with(logGroupName,\`${CCO_RESOURCE_NAME}\`)].logGroupName' --region ${REGION} --output text"
#  echo_green "aws logs describe-log-groups --log-group-name-prefix '${CCO_RESOURCE_NAME}' --region ${REGION} --output text"
#  echo_green "aws logs delete-log-group --region ${REGION} --log-group-name ${CCO_RESOURCE_NAME}- "

  echo_bold "----"
  echo_blue "export KUBECONFIG=${CLUSTER_DIR}/auth/kubeconfig && "
  echo_blue "oc login -u kubeadmin -p \$(cat ${CLUSTER_DIR}/auth/kubeadmin-password)"
  echo_bold "----"
#  echo_blue "make clean && make deploy-elasticsearch-operator && make deploy-image && make deploy-catalog && make install"
  echo_blue "make clean && make deploy"
  echo_bold "----"
  echo_blue "oc apply -f hack/cr.yaml && echo 'sleeping...' && "
  echo_blue "sleep 90 && oc apply -f hack/cw-secret-test.yaml && oc apply -f hack/cw-logforwarder.yaml"

  echo_bold "----- Done -----"
  exit 0
}

destroy_cluster() {

	echo_bold "Destroying cluster under ${CLUSTER_DIR}..."
  confirm

	if openshift-install --dir "${CLUSTER_DIR}" destroy cluster; then
		_notify_send -t 5000 \
			'OCP cluster deleted' \
			'Successfully deleted OCP cluster' || :
	else
		_notify_send -t 5000 \
			'FAILED to delete OCP cluster' \
			'FAILURE trying to delete OCP cluster. See log for details'
		return 1
	fi

	cleanup_cco_utility_resources

  echo_bold "--- Done ---"
  exit 0
}

cleanup_cco_utility_resources() {
  echo_bold "Cleaning up cco resources at AWS"

  if ccoctl aws delete --name=${CCO_RESOURCE_NAME} --region=${REGION}; then
    _notify_send -t 5000 \
      'AWS resources deleted' \
      'Successfully deleted OCP AWS resources' || :
  else
    _notify_send -t 5000 \
      'FAILED to delete AWS resources' \
      'FAILURE trying to delete OCP cluster resources'
    return 1
  fi
}

_notify_send() {
	notify-send "$@"
}

# abort with an error message (not currently used)
abort() {
	read -r line func file <<< "$(caller 0)"
	echo -e "${red}ERROR in $file:$func:$line: $1${reset}" > /dev/stderr
	echo "Bye"
	exit 1
}

# ---
# Never put anything below this line. This is to prevent any partial execution
# if curl ever interrupts the download prematurely. In that case, this script
# will not execute since this is the last line in the script.
err_report() { echo "Error on line $1"; }
trap 'err_report $LINENO' ERR

main "$@"
