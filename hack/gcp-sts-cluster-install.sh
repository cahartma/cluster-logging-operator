#!/bin/bash

set -ueo pipefail

# colors
RED='\033[0;31m'
GREEN='\033[0;32m'
TAN='\033[0;33m'
AQUA='\033[0;36m'
NC='\033[0m'

ME=$(basename "$0")
KERBEROS_USERNAME=${KERBEROS_USERNAME:-$(whoami)}
TMP_DIR=${TMP_DIR:-$HOME/tmp}
CLUSTER_DIR=${CLUSTER_DIR:-${TMP_DIR}/installer}
CCO_UTILITY_DIR=${CCO_UTILITY_DIR:-${TMP_DIR}/cco}
CCO_RESOURCE_NAME=${KERBEROS_USERNAME}-$(date +'%m%d')
CLUSTER_NAME=${KERBEROS_USERNAME}-$(date +'%m%d')cluster$(date +'%H%M')
#RELEASE_IMAGE=${RELEASE_IMAGE:-"quay.io/openshift-release-dev/ocp-release:4.12.0-rc.8-x86_64"}
#RELEASE_IMAGE=${RELEASE_IMAGE:-"quay.io/openshift-release-dev/ocp-release:4.11.26-x86_64"}
RELEASE_IMAGE=${RELEASE_IMAGE:-"quay.io/openshift-release-dev/ocp-release:4.13.9-x86_64"}
REGION=${REGION:-"us-east1"}
SSH_KEY="$(cat $HOME/.ssh/id_ed25519.pub)"
PULL_SECRET="$(tr -d '[:space:]' < $HOME/.docker/config.json)"
SECRET_NAME=${SECRET_NAME:-"gcp-test-secret"}
PROJECT_ID=${PROJECT_ID:-$(gcloud config get-value project)}

DEBUG='info'
CONFIG_ONLY=${CONFIG_ONLY:-0}

usage() {
  echo -e ${AQUA}
	cat <<-EOF
	Deploy sts-enabled cluster to GCP (credentialsMode=Manual) using CCO utility to create SA/IAM resources

	Usage:
	  ${ME} [flags]

	Flags:
	      --cleanup            Destroy existing cluster and exit
	  -d, --dir string         Install directory (default "${CLUSTER_DIR}")
	  -c, --config             Create install config only
	  -l, --logging-role       Create resources at gcloud then create associated cluster secret
	  -i, --logging-instance   Create clusterlogging instance with EO, Kibana, and Vector
	  -f, --logforwarding      Create an instance of forwarder
	  -r, --region             Specify GCP region (default "${REGION}")
	  -g, --debug              Use debug log level for install
	  -h, --help               Help for ${ME}
	EOF
	echo -e ${NC}
#  for (( i = 30; i < 38; i++ )); do
#    echo -e "\033[0;"$i"m Normal: (0;$i); \033[1;"$i"m Light: (1;$i)";
#  done
#  echo -e "$NC"
}

main() {
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
      -c|--config)
        CONFIG_ONLY=1
        shift # past argument
        ;;
      -g|--debug)
        DEBUG='debug'
        shift # past argument
        ;;
      -l|--logging-role)
        logging_role && exit 0
        ;;
      -i|--logging-instance)
        logging_instance && exit 0
        ;;
      -f|--logforwarding)
        forwarder_instance && exit 0
        ;;
      -h|--help)
        usage && exit 0
        ;;
      *)
        echo -e "${RED}Unknown flag $1${NC}" > /dev/stderr
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

  setup

  # exit after setup if config only
  [ $CONFIG_ONLY -eq 1 ] && echo && exit 0

  echo "Extracting credential requests"
  oc adm release extract --credentials-requests ${RELEASE_IMAGE} \
    --cloud=gcp \
    --to=${CCO_UTILITY_DIR}/credrequests
  echo
  echo "Creating IAM resources"
  cd ${CCO_UTILITY_DIR}
  ccoctl gcp create-all --region=${REGION} \
    --name=${CCO_RESOURCE_NAME} \
    --project=${PROJECT_ID} \
    --output-dir=${CCO_UTILITY_DIR}/ \
    --credentials-requests-dir=${CCO_UTILITY_DIR}/credrequests

  echo -e "Creating installer manifests"
  openshift-install create manifests --dir ${CLUSTER_DIR}
  echo "Copying manifest files to install directory"
  cp ${CCO_UTILITY_DIR}/manifests/* ${CLUSTER_DIR}/manifests
  echo "Copying the private key"
  cp -a ${CCO_UTILITY_DIR}/tls ${CLUSTER_DIR}

  echo -e "\nSetup is complete and ready to create cluster..."

  create_cluster
}

# Remove existing files and create install config
setup() {
  echo -e "\n${GREEN}Reminder: VPN must be connected before we start the installer${NC}"
  confirm

  if [[ -d ${CLUSTER_DIR} || -d ${CCO_UTILITY_DIR} ]]; then
      echo -e "\n${AQUA}Existing install or cco utility files need to removed from ${TMP_DIR}${NC}"
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
	baseDomain: observability.gcp.devcluster.openshift.com
	credentialsMode: Manual
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
	  gcp:
	    projectID: openshift-observability
	    region: ${REGION}
	publish: External
	pullSecret: |-
	  ${PULL_SECRET}
	sshKey: |-
	  ${SSH_KEY}
	EOF

  echo "Install config file created at ${CLUSTER_DIR}"
  # create a copy
  cp ${CLUSTER_DIR}/install-config.yaml ${TMP_DIR}/install-config$(date +'%m%d')-gcp.yaml
  echo "Copy created at ${TMP_DIR}/install-config$(date +'%m%d')-gcp.yaml"
}

create_cluster() {
  #  just in case
  [ $CONFIG_ONLY -eq 1 ] && exit 0

#  confirm
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
}

logging_role() {
  echo "Creating logging credrequest at [$PWD/credrequests/${SECRET_NAME}-credrequest.yaml]"
  mkdir -p "credrequests"

	cat <<-EOF > credrequests/${SECRET_NAME}-credrequest.yaml
---
apiVersion: cloudcredential.openshift.io/v1
kind: CredentialsRequest
metadata:
  name: ${SECRET_NAME}-credrequest
  namespace: openshift-logging
spec:
  providerSpec:
    apiVersion: cloudcredential.openshift.io/v1
    kind: GCPProviderSpec
    predefinedRoles:
      - roles/logging.admin
      - roles/iam.workloadIdentityUser
      - roles/iam.serviceAccountUser
    skipServiceCheck: true
  secretRef:
    name: ${SECRET_NAME}
    namespace: openshift-logging
  serviceAccountNames:
    - logcollector
EOF

  echo -e "\nCreating cloud resources and output file for applying our secret"
  ccoctl gcp create-all --name=${CCO_RESOURCE_NAME} --region=${REGION} \
    --credentials-requests-dir=credrequests \
    --output-dir=output \
    --project=${PROJECT_ID}

  echo -e "\nfile [output/manifests/openshift-logging-${SECRET_NAME}-credentials.yaml] created"

  export KUBECONFIG=${CLUSTER_DIR}/auth/kubeconfig
  oc login -u kubeadmin -p $(cat ${CLUSTER_DIR}/auth/kubeadmin-password)

  echo -e "\nCreating secret based on new role and OIDC bucket"
  oc project openshift-logging
  oc apply -f output/manifests/openshift-logging-${SECRET_NAME}-credentials.yaml

  echo
  exit 0
}

logging_instance() {
  instance="hack/cr-vector-gcp.yaml"
  echo -e "\nCreating clusterlogging instance: ${instance}"
  cat <<-EOF > ${instance}
---
apiVersion: logging.openshift.io/v1
kind: ClusterLogging
metadata:
  name: instance
  namespace: openshift-logging
spec:
  logStore:
    type: elasticsearch
    elasticsearch:
      nodeCount: 1
      resources:
        requests:
          memory: 2Gi
      redundancyPolicy: ZeroRedundancy
#  visualization:
#    type: kibana
#    kibana:
#      replicas: 1
  collection:
    type: vector
EOF

  echo -e "\nApplying yaml file"
  oc apply -f ${instance}

  echo
  exit 0
}

forwarder_instance() {
  echo -e "\nReady to install logfowarding resources"
  confirm

  echo -e "\nApplying secret for ${SECRET_NAME}"
  oc apply -f output/manifests/openshift-logging-${SECRET_NAME}-credentials.yaml

  forwarder="hack/gcp-logforwarder.yaml"

  echo -e "\nCreating logforwarder instance:  ${forwarder}"
  cat <<-EOF > ${forwarder}
---
apiVersion: "logging.openshift.io/v1"
kind: ClusterLogForwarder
metadata:
  name: instance
  namespace: openshift-logging
spec:
  outputs:
    - name: gcl
      type: googleCloudLogging
      googleCloudLogging:
        projectId: openshift-observability
        logId: vector-1
      secret:
        name: ${SECRET_NAME}
  pipelines:
    - name: gcl-forward
      inputRefs:
        - application
        - infrastructure
      outputRefs:
        - gcl
EOF

  echo -e "\nApply logforwarder yaml file"
  oc apply -f ${forwarder}

  echo
  exit 0
}

post_install() {
  _notify_send -t 5000 \
    "OCP cluster ${CLUSTER_NAME} " \
    "Created successfully"

  echo -e "\nSuccessfully deployed cluster!"

  echo -e "\n--- Login ---"
  echo -e "${AQUA}  export KUBECONFIG=${CLUSTER_DIR}/auth/kubeconfig${NC}"
  echo -e "${AQUA}  oc login -u kubeadmin -p \$(cat ${CLUSTER_DIR}/auth/kubeadmin-password)${NC}"

  echo -e "\n--- Install Logging Components ---"
  echo -e "${GREEN}  make deploy${NC}"

  echo -e "\n--- Create logging resources for GCP ---"
  echo -e "${TAN}  ./hack/${ME} -l (--logging-role)${NC} to create GCP resources and logging secret"
  echo -e "${TAN}  ./hack/${ME} -i (--logging-instance)${NC} to create vector logging instance"
  echo -e "${TAN}  ./hack/${ME} -f (--logforwarding)${NC} to create logforwarder instance"

  echo -e "\n--- Cleanup Commands ---"
  echo -e "${GREEN}  ./hack/${ME} --cleanup${NC}"
  echo -e "  or"
  echo -e "${GREEN}  ccoctl gcp delete --name=${CCO_RESOURCE_NAME} --project=${PROJECT_ID}${NC} --credentials-requests-dir=${CCO_UTILITY_DIR}/credrequests"

  echo -e "\n-------------- Done --------------\n"
  exit 0
}

destroy_cluster() {

	echo -e "\nDestroying cluster under ${CLUSTER_DIR}..."
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

  echo -e "\n--------- Done ----------\n"
  exit 0
}

cleanup_cco_utility_resources() {
  echo -e "\nCleaning up cco resources at GCP"

  if ccoctl gcp delete --name=${CCO_RESOURCE_NAME} --project=${PROJECT_ID} --credentials-requests-dir=${CCO_UTILITY_DIR}/credrequests; then
    _notify_send -t 5000 \
      'GCP resources deleted' \
      'Successfully deleted OCP GCP resources' || :
  else
    _notify_send -t 5000 \
      'FAILED to delete GCP resources' \
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
	echo -e "${RED}ERROR in $file:$func:$line: $1${NC}" > /dev/stderr
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
