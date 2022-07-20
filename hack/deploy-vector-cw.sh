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

TEST_SECRET_NAME="vector-cw-secret"
ME=$(basename "$0")
KERBEROS_USERNAME=${KERBEROS_USERNAME:-$(whoami)}
HACK_DIR=${HACK_DIR:-"$(pwd)/hack"}
LOCAL_TOML=${LOCAL_TOML:-${HACK_DIR}/vector.toml}
CLUSTER_NAME_DATE=${KERBEROS_USERNAME}-$(date +'%m%d')
HOUR_MIN=${HOUR_MIN:-$(date +'%H%M')}
REGION=${REGION:-"us-east-2"}

usage() {
  echo -e "${AQUA}"
	cat <<-EOF

	--------------------------------------------------------------------------------------------
	Vector Tooling for a logging instance and forwarding
	  - creates and applies resources to facilitate a simple test of cloudwatch forwarding

	Usage:
	  ${ME} [flags]

	Flags:
	  -g, --get-toml              Get the local vector.toml
	  -t, --make-toml             Create a default vector.toml file
	  -e, --extract-toml          Extract the vector.toml from the cluster secret (collector-config)
	  -c, --apply-config          Delete existing and create new collector-config from local encoded toml
	  -d, --restore-default       Delete existing and create new cluster logging instance with vector
	  -i, --create-instance       Create a cluster logging instance with vector
	  -f, --log-forwarding        Create an instance of cluster log-forwarding
	  -p, --patch-instance        Edit clusterlogging instance and change to Unmanaged
	  -h, --help                  Help for ${ME}
	  ---------- AWS Cloudwatch -----------------------------------------------------------------
	  -o, --describe-groups       Describe aws log groups
	  -s, --describe-streams      Describe aws log stream by type --> example: '${ME} -s audit'

	EOF
	echo -e "${NC}"
}

main() {
  CREATE_INSTANCE=0
  PATCH_CL_ONLY=0
#  APPEND_ONLY=0
  while [[ $# -gt 0 ]]; do
    key="$1"
    logtype="$2"
    case $key in
      -t|--make-toml)
        make_default_toml && exit 0
        ;;
      -e|--extract-toml)
        extract_cluster_toml && exit 0
        ;;
#      -a|--append-cw-sink)
#        APPEND_ONLY=1
#        shift # past argument
#        ;;
      -c|--apply-config)
        apply_config && echo && exit 0
        ;;
      -d|--restore-default)
        restore_default && exit 0
        ;;
      -g|--get-toml)
        get_toml_only && exit 0
        ;;
      -p|--patch-only)
        PATCH_CL_ONLY=1
        shift # past argument
        ;;
      -i|--create-instance)
        CREATE_INSTANCE=1
        shift # past argument
        ;;
      -f|--logforwarding)
        cluster_log_forwarder && exit 0
        ;;
      -o|--describe-groups)
        describe_log_groups && exit 0
        ;;
      -s|--describe-streams)
        [[ -v logtype ]] && describe_log_streams $logtype && exit 0
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

  if [[ "${CREATE_INSTANCE}" -eq 1 ]]; then
      echo -e "creating vector instance of clusterlogging"
      clusterlogging_instance | oc apply -f -
      echo
      exit 0
  fi

  if [[ "${PATCH_CL_ONLY}" -eq 1 ]]; then
      patch_cl_instance
      oc delete pods -lcomponent=collector
      echo
      exit 0
  fi

#  if [[ "${APPEND_ONLY}" -eq 1 ]]; then
#      append_cw_sink
#      echo -e "done. use the '-g' flag to view the local file or '-c' to apply the new toml to the cluster\n"
#      exit 0
#  fi

#  make_default_toml
#  append_cw_sink
#  apply_config
#  echo -e "deployed cloudwatch sink to vector\n"
  usage
  exit 0
}
describe_log_groups() {
  aws logs describe-log-groups --log-group-name-prefix ${CLUSTER_NAME_DATE} --query logGroups[].logGroupName --output json
}

describe_log_streams() {
  type=$1
  echo "aws logs describe-log-streams --log-group-name ${CLUSTER_NAME_DATE}.$type --query logStreams[].logStreamName --output json"
  aws logs describe-log-streams --log-group-name ${CLUSTER_NAME_DATE}.$type --query logStreams[].logStreamName --output json

}
restore_default() {
  oc delete clusterlogging instance
  oc delete clf instance
#  oc delete secret/collector-config
#  oc apply -f hack/cr-vector.yaml
  clusterlogging_instance | oc apply -f -
  echo -e "restored default clusterlogging instance\n"
}

get_toml_only() {
  if [ ! -s $LOCAL_TOML ]; then
    echo -e "local vector.toml not found or is empty \nyou can use the '-t' flag to create a local file\n"
  else
    cat ${LOCAL_TOML} && echo
  fi
#  [ -s $LOCAL_TOML ] && cat ${LOCAL_TOML} || echo -e "local vector.toml not found or is empty\n"
}

append_cw_sink() {
  oc apply -f ${HACK_DIR}/${TEST_SECRET_NAME}.yaml
  AWS_KEY_ID=$(oc get secret ${TEST_SECRET_NAME} -o jsonpath='{.data.aws_access_key_id}' | base64 -d)
  AWS_KEY_SECRET=$(oc get secret ${TEST_SECRET_NAME} -o jsonpath='{.data.aws_secret_access_key}' | base64 -d)
  [[ -z ${AWS_KEY_ID} ]] && echo -e "${RED}aws credentials not found${NC}\n" && exit

  echo -e "appending cloudwatch sink"
  cw_sink >> ${LOCAL_TOML}
}

cw_sink() {
  cat <<EOF


# Adding group_name and stream_name field
[transforms.cw_add_grpandstream]
type = "remap"
inputs = ["pipeline_0_"]
source = """
.GroupBy = "logType"
.LogGroupPrefix = "${CLUSTER_NAME_DATE}"
"""

# Cloudwatch
[sinks.cw]
type = "aws_cloudwatch_logs"
inputs = ["cw_add_grpandstream"]
encoding.codec = "json"
region = "${REGION}"
group_name = "{{ LogGroupPrefix }}-{{ log_type }}"
stream_name = "{{ kubernetes.namespace_name }}.{{ kubernetes.pod_uid }}"
auth.access_key_id = "${AWS_KEY_ID}"
auth.secret_access_key = "${AWS_KEY_SECRET}"
healthcheck.enabled = false
EOF
}

patch_cl_instance() {
  echo -e "editing clusterlogging instance to: \"Unmanaged\""
  oc patch clusterlogging instance -p '{"spec":{"managementState":"Unmanaged"}}' --type merge
}

apply_config() {
  if [ ! -s $LOCAL_TOML ]; then
    echo "local vector.toml file not found"
    exit 0
  fi

  echo -e "creating aws credentials secret"
  oc apply -f ${HACK_DIR}/${TEST_SECRET_NAME}.yaml

  echo -e "\ndeleting collector-config secret"
  oc delete secret collector-config

  echo -e "\ncreating new collector-config secret from local toml file"
  oc create secret generic collector-config -n openshift-logging --from-file=${LOCAL_TOML}
#  echo -e "removing local vector.toml"
#  rm ${LOCAL_TOML}

  echo -e "\ndeleting all collector pods"
  oc delete pods -lcomponent=collector
}

extract_cluster_toml() {
  echo -e "extracting from cluster secret 'collector-config'"
  oc extract secret/collector-config --keys=vector.toml --to=${HACK_DIR} --confirm
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
      type: "vector"
      fluentd: {}
EOF
}

cluster_log_forwarder() {
  echo -e "\nReady to install logfowarding resources"
  confirm

  echo -e "\nApplying secret for ${TEST_SECRET_NAME}"
  oc apply -f hack/${TEST_SECRET_NAME}.yaml

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
        groupPrefix: ${CLUSTER_NAME_DATE}
        region: ${REGION}
      secret:
        name: ${TEST_SECRET_NAME}
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

make_default_toml() {
  echo -e "creating local toml file\n"
  cat <<-EOF > ${LOCAL_TOML}
# Logs from containers (including openshift containers)
[sources.raw_container_logs]
type = "kubernetes_logs"
auto_partial_merge = true
exclude_paths_glob_patterns = ["/var/log/pods/openshift-logging_collector-*/*/*.log", "/var/log/pods/openshift-logging_elasticsearch-*/*/*.log", "/var/log/pods/openshift-logging_kibana-*/*/*.log"]

[sources.raw_journal_logs]
type = "journald"

[sources.internal_metrics]
type = "internal_metrics"

[transforms.container_logs]
type = "remap"
inputs = ["raw_container_logs"]
source = """
  level = "unknown"
  if match!(.message,r'(Warning|WARN|W[0-9]+|level=warn|Value:warn|"level":"warn")'){
    level = "warn"
  } else if match!(.message, r'Info|INFO|I[0-9]+|level=info|Value:info|"level":"info"'){
    level = "info"
  } else if match!(.message, r'Error|ERROR|E[0-9]+|level=error|Value:error|"level":"error"'){
    level = "error"
  } else if match!(.message, r'Debug|DEBUG|D[0-9]+|level=debug|Value:debug|"level":"debug"'){
    level = "debug"
  }
  .level = level

  namespace_name = .kubernetes.pod_namespace
  del(.kubernetes.pod_namespace)
  .kubernetes.namespace_name = namespace_name

  del(.file)

  del(.source_type)

  del(.stream)

  del(.kubernetes.pod_ips)
"""

[transforms.journal_logs]
type = "remap"
inputs = ["raw_journal_logs"]
source = """
  .
"""

[transforms.route_container_logs]
type = "route"
inputs = ["container_logs"]
route.app = '!((starts_with!(.kubernetes.namespace_name,"kube")) || (starts_with!(.kubernetes.namespace_name,"openshift")) || (.kubernetes.namespace_name == "default"))'
route.infra = '(starts_with!(.kubernetes.namespace_name,"kube")) || (starts_with!(.kubernetes.namespace_name,"openshift")) || (.kubernetes.namespace_name == "default")'

# Rename log stream to "application"
[transforms.application]
type = "remap"
inputs = ["route_container_logs.app"]
source = """
  .log_type = "application"
"""

# Rename log stream to "infrastructure"
[transforms.infrastructure]
type = "remap"
inputs = ["route_container_logs.infra","journal_logs"]
source = """
  .log_type = "infrastructure"
"""

[transforms.pipeline_0_]
type = "remap"
inputs = ["application","infrastructure"]
source = """
  .
"""

# Adding _id field
[transforms.default_add_es_id]
type = "remap"
inputs = ["pipeline_0_"]
source = """
  index = "default"
  if (.log_type == "application"){
    index = "app"
  }
  if (.log_type == "infrastructure"){
    index = "infra"
  }
  if (.log_type == "audit"){
    index = "audit"
  }
  .write_index = index + "-write"
  ._id = encode_base64(uuid_v4())
"""

[transforms.default_dedot_and_flatten]
type = "lua"
inputs = ["default_add_es_id"]
version = "2"
hooks.process = "process"
source = """
    function process(event, emit)
        if event.log.kubernetes == nil then
            emit(event)
            return
        end
        if event.log.kubernetes.pod_labels == nil then
            emit(event)
            return
        end
        dedot(event.log.kubernetes.pod_labels)
        -- create "flat_labels" key
        event.log.kubernetes.flat_labels = {}
        i = 1
        -- flatten the labels
        for k,v in pairs(event.log.kubernetes.pod_labels) do
          event.log.kubernetes.flat_labels[i] = k.."="..v
          i=i+1
        end
        -- delete the "pod_labels" key
        event.log.kubernetes["pod_labels"] = nil
        emit(event)
    end

    function dedot(map)
        if map == nil then
            return
        end
        local new_map = {}
        local changed_keys = {}
        for k, v in pairs(map) do
            local dedotted = string.gsub(k, "%.", "_")
            if dedotted ~= k then
                new_map[dedotted] = v
                changed_keys[k] = true
            end
        end
        for k in pairs(changed_keys) do
            map[k] = nil
        end
        for k, v in pairs(new_map) do
            map[k] = v
        end
    end
"""

[sinks.default]
type = "elasticsearch"
inputs = ["default_dedot_and_flatten"]
endpoint = "https://elasticsearch:9200"
bulk.index = "{{ write_index }}"
bulk.action = "create"
request.timeout_secs = 2147483648
id_key = "_id"

# TLS Config
[sinks.default.tls]
key_file = "/var/run/ocp-collector/secrets/collector/tls.key"
crt_file = "/var/run/ocp-collector/secrets/collector/tls.crt"

ca_file = "/var/run/ocp-collector/secrets/collector/ca-bundle.crt"

[sinks.prometheus_output]
type = "prometheus_exporter"
inputs = ["internal_metrics"]
address = "0.0.0.0:24231"
default_namespace = "collector"

[sinks.prometheus_output.tls]
enabled = true
key_file = "/etc/collector/metrics/tls.key"
crt_file = "/etc/collector/metrics/tls.crt"
EOF
  echo -e "${LOCAL_TOML} created"
}

# ---
# Never put anything below this line

main "$@"