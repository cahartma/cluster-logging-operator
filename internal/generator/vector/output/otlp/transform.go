package otlp

import (
	. "github.com/openshift/cluster-logging-operator/internal/generator/framework"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/elements"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/helpers"
	"strings"
)

type Route struct {
	ComponentID string
	Desc        string
	Inputs      string
}

func (r Route) Name() string {
	return "routeTemplate"
}

func (r Route) Template() string {
	return `{{define "routeTemplate" -}}
{{if .Desc -}}
# {{.Desc}}
{{end -}}
[transforms.{{.ComponentID}}]
type = "route"
inputs = {{.Inputs}}
route.container = '.log_source == "container"'
route.journal = '.log_source == "node"'
# route.audit = '.log_type == "audit"'
route.linux = '.log_source == "auditd"'
route.kube = '.log_source == "kubeAPI"'
route.openshift = '.log_source == "openshiftAPI"'
route.ovn = '.log_source == "ovn"'
{{end}}
`
}

func RouteBySource(id string, inputs []string) Element {
	return Route{
		Desc:        "Route container, journal, and audit logs separately",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
	}
}

type Reduce struct {
	ComponentID string
	Desc        string
	Inputs      string
}

func (r Reduce) Name() string {
	return "reduceTemplate"
}

func (r Reduce) Template() string {
	return `{{define "reduceTemplate" -}}
{{if .Desc -}}
# {{.Desc}}
{{end -}}
[transforms.{{.ComponentID}}]
type = "reduce"
inputs = {{.Inputs}}
# The maximum period of time to wait after the last event is received, 
# before a combined event should be considered complete. 
expire_after_ms = 10000
# maximum number of events to group together, this seems to work best at lower value for app logs??
max_events = 50
group_by = [".kubernetes.namespace_name",".kubernetes.pod_name",".kubernetes.container_name"]
# When grouping, discard all but the last value found for each resource attribute.
merge_strategies.resource = "retain"
# When grouping, discard all but the first value found for each resource attribute.
# merge_strategies.resource.attributes = "discard"
merge_strategies.logRecords = "array"
{{end}}
`
}

type Merge struct {
	ComponentID string
	Desc        string
	Inputs      string
}

func (m Merge) Name() string {
	return "mergeTemplate"
}

func (m Merge) Template() string {
	return `{{define "mergeTemplate" -}}
{{if .Desc -}}
# {{.Desc}}
{{end -}}
[transforms.{{.ComponentID}}]
type = "reduce"
inputs = {{.Inputs}}
expire_after_ms = 10000
max_events = 50
group_by = [".k8s.node.name"]
merge_strategies.resource = "retain"
merge_strategies.logRecords = "array"
{{end}}
`
}

type Retain struct {
	ComponentID string
	Desc        string
	Inputs      string
}

func (t Retain) Name() string {
	return "retainTemplate"
}

func (t Retain) Template() string {
	return `{{define "retainTemplate" -}}
{{if .Desc -}}
# {{.Desc}}
{{end -}}
[transforms.{{.ComponentID}}]
type = "reduce"
inputs = {{.Inputs}}
expire_after_ms = 10000
max_events = 50
# node.name is derived from .hostname
group_by = [".k8s.node.name", "openshift.log.source"]
merge_strategies.resource = "retain"
merge_strategies.logRecords = "array"
{{end}}
`
}

func GroupByContainer(id string, inputs []string) Element {
	return Reduce{
		Desc:        "Merge container logs and group by namespace, pod, and container",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
	}
}

func GroupByNode(id string, inputs []string) Element {
	return Merge{
		Desc:        "Merge node logs and group by resource",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
	}
}

func GroupByAudit(id string, inputs []string) Element {
	return Retain{
		Desc:        "Merge audit logs and group by resource",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
	}
}

func FormatBatch(id string, inputs []string) Element {
	return elements.Remap{
		Desc:        "Remap to match OTLP/HTTP request payload",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
		VRL: strings.TrimSpace(`
. = {
      "resource": {
         "attributes": .resource.attributes,
      },
      "scopeLogs": [
        {"logRecords": .logRecords}
      ]
    }
`),
	}
}

func TransformContainer(id string, inputs []string) Element {
	return elements.Remap{
		Desc:        "Normalize container log records to OTLP semantic conventions",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
		VRL: strings.TrimSpace(`
# OTLP semantic conventions for application and infrastructure containers 
if .log_source == "container" {
	# Included attribute fields
	meta = [
	  "kubernetes.pod_name", 
	  "kubernetes.pod_id",
	  "kubernetes.namespace_name",
	  "kubernetes.container_name",
	  "openshift.cluster_id",
	  "hostname",
	  "file"
	]
	replace = {
	  "pod.id": "pod.uid",
	  "cluster.id": "cluster.uid",
	  "hostname": "node.name",
	  "file": "logs.file.path"
	}
	# Create resource attributes based on meta and replaces list
	resource.attributes = []
	for_each(meta) -> |_,value| {
	  sub_key = value
	  path = split(value,".")
	  # if one or more dots (levels), replace the final underscores with dots
	  if length(path) > 1 {
		sub_key = replace!(path[-1],"_",".")
	  }
      # check for matches in replace
	  if get!(replace, [sub_key]) != null {
		sub_key = string!(get!(replace, [sub_key]))
	  } 
	  # Add all fields to "resource.attributes.k8s"
      resource.attributes = append(
		resource.attributes,
		[{"key": "k8s." + sub_key, "value": {"stringValue": get!(.,path)}}]
	  )
	}
	# Append kube pod labels
	if exists(.kubernetes.labels) {
		for_each(object!(.kubernetes.labels)) -> |key,value| {  
			resource.attributes = append(
				resource.attributes,
				[{"key": "k8s.pod.labels." + key, "value": {"stringValue": value}}]
			)
		}
    }
	# Appending "openshift" attributes
	resource.attributes = append(
		resource.attributes, 
        [{"key": "openshift.log.type", "value": {"stringValue": .log_type}},
        {"key": "openshift.log.source", "value": {"stringValue": .log_source}}]
	)
	# transform the record
	r = {}
	r.timeUnixNano = to_string(to_unix_timestamp(parse_timestamp!(.@timestamp, format:"%+"), unit:"nanoseconds"))
	r.observedTimeUnixNano = to_string(to_unix_timestamp(now(), unit:"nanoseconds"))
	# Convert syslog severity keyword to number, default to 9 (unknown)
	r.severityNumber = to_syslog_severity(.level) ?? 9
	r.body = {"stringValue": string!(.message)}
	. = {
	  "kubernetes": .kubernetes,
	  "resource": resource,
	  "logRecords": r
	 }
}
`),
	}
}

func TransformJournal(id string, inputs []string) Element {
	return elements.Remap{
		Desc:        "Normalize node log events to OTLP semantic conventions",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
		VRL: strings.TrimSpace(`
# OTLP semantic conventions for infrastructure journal logs 
if .log_source == "node" {
    meta = [
	  "systemd.t.BOOT_ID",
      "systemd.t.COMM",
      "systemd.t.CAP_EFFECTIVE",
      "systemd.t.CMDLINE",
      "systemd.t.COMM",
      "systemd.t.EXE",
      "systemd.t.GID",
      "systemd.t.MACHINE_ID",
      "systemd.t.PID",
      "systemd.t.SELINUX_CONTEXT",
      "systemd.t.SYSTEMD_CGROUP",
      "systemd.t.SYSTEMD_INVOCATION_ID",
      "systemd.t.SYSTEMD_SLICE",
      "systemd.t.SYSTEMD_UNIT",
      "systemd.t.TRANSPORT",
      "systemd.t.UID",
      "systemd.u.SYSLOG_FACILITY",
	  "systemd.u.SYSLOG_IDENTIFIER",
	  "hostname",
	  "openshift.cluster_id"
	]
	replace = {
	  "hostname": "node.name",
	  "cluster.id": "cluster.uid",
      "SYSTEMD.CGROUP": "system.cgroup",
      "SYSTEMD.INVOCATION.ID": "system.invocation.id",
      "SYSTEMD.SLICE": "system.slice",
      "SYSTEMD.UNIT": "system.unit",
      "SYSLOG.FACILITY": "syslog.facility",
      "SYSLOG.IDENTIFIER": "syslog.identifier"
	}
	
	resource.attributes = []
	for_each(meta) -> |_,value| {
	  # single key with no dots, sub_key is the value
      sub_key = value
	  path = split(value,".")
	  # if one or more dots (levels), replace the last part's underscores with dots	
	  if length(path) > 1 {
		sub_key = replace!(path[-1],"_",".")
	  }
      # check for matches in replace
      if get!(replace, [sub_key]) != null {
		# replace if found
		sub_key = string!(get!(replace, [sub_key]))
	  } else {
		# if not found in replace, then downcase any remaining in the list
        sub_key = downcase(sub_key)
      }
	  # Add them all to "resource.attributes"
      resource.attributes = append(
		resource.attributes,
		[{"key": sub_key, "value": {"stringValue": get!(.,path)}}]
	  )
	}
    # Appending "openshift" attributes
	resource.attributes = append(
        resource.attributes, 
        [{"key": "openshift.log.type", "value": {"stringValue": .log_type}},
        {"key": "openshift.log.source", "value": {"stringValue": .log_source}}]
	)
	# Transform into resource log records 
	r = {}
	r.timeUnixNano = to_string(to_unix_timestamp(parse_timestamp!(.@timestamp, format:"%+"), unit:"nanoseconds"))
	r.observedTimeUnixNano = to_string(to_unix_timestamp(now(), unit:"nanoseconds"))
	# Convert syslog severity keyword to number, default to 9 (unknown)
	r.severityNumber = to_syslog_severity(.level) ?? 9
	r.body = {"stringValue": string!(.message)}
	. = {
	  "resource": resource,
	  "logRecords": r
	 }
}
`),
	}
}

//func TransformAudit(id string, inputs []string) Element {
//	return elements.Remap{
//		Desc:        "Normalize audit log event to OTLP semantic conventions",
//		ComponentID: id,
//		Inputs:      helpers.MakeInputs(inputs...),
//		VRL: strings.TrimSpace(`
//# OTLP semantic conventions for audit log events
//if .log_type == "audit" {
//	# Included attribute fields
//	meta = [
//	  "openshift.cluster_id",
//	  "hostname",
//	  "file"
//	]
//	replace = {
//	  "cluster.id": "cluster.uid",
//	  "hostname": "node.name",
//	  "file": "logs.file.path"
//	}
//	# Create resource attributes based on meta and replaces list
//	resource.attributes = []
//	for_each(meta) -> |_,value| {
//	  sub_key = value
//	  path = split(value,".")
//	  # if one or more dots (levels), replace the final underscores with dots
//	  if length(path) > 1 {
//		sub_key = replace!(path[-1],"_",".")
//	  }
//      # check for matches in replace
//	  if get!(replace, [sub_key]) != null {
//		sub_key = string!(get!(replace, [sub_key]))
//	  }
//	  # Add all fields to "resource.attributes.k8s"
//      resource.attributes = append(
//		resource.attributes,
//		[{"key": sub_key, "value": {"stringValue": get!(.,path)}}]
//	  )
//	}
//	# Appending custom "openshift" attributes
//	resource.attributes = append(
//        resource.attributes,
//        [{"key": "openshift.log.type", "value": {"stringValue": .log_type}},
//        {"key": "openshift.log.source", "value": {"stringValue": .log_source}}]
//	)
//	# transform the record
//	r = {}
//	r.timeUnixNano = to_string(to_unix_timestamp(parse_timestamp!(.@timestamp, format:"%+"), unit:"nanoseconds"))
//	r.observedTimeUnixNano = to_string(to_unix_timestamp(now(), unit:"nanoseconds"))
//	# Convert syslog severity keyword to number, default to 9 (unknown)
//	r.severityNumber = to_syslog_severity(.level) ?? 9
//	r.body = {"stringValue": string!(.message)}
//	. = {
//	  "resource": resource,
//	  "logRecords": r
//	 }
//}
//`),
//	}
//}

func TransformAuditHost(id string, inputs []string) Element {
	return elements.Remap{
		Desc:        "Normalize audit log record to OTLP semantic conventions",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
		VRL: strings.TrimSpace(`
# OTLP semantic conventions for auditd linux log records 
# Included attribute fields
meta = [
  "openshift.cluster_id",
  "hostname",
  "file"
]
replace = {
  "cluster.id": "cluster.uid",
  "hostname": "node.name",
  "file": "logs.file.path"
}
# Create resource attributes based on meta and replaces list
resource.attributes = []
for_each(meta) -> |_,value| {
  sub_key = value
  path = split(value,".")
  # if one or more dots (levels), replace the final underscores with dots
  if length(path) > 1 {
	sub_key = replace!(path[-1],"_",".")
  }
  # check for matches in replace
  if get!(replace, [sub_key]) != null {
	sub_key = string!(get!(replace, [sub_key]))
  } 
  # Add all fields to "resource.attributes.k8s"
  resource.attributes = append(
	resource.attributes,
	[{"key": "k8s." + sub_key, "value": {"stringValue": get!(.,path)}}]
  )
}
# Appending custom "openshift" attributes
resource.attributes = append(
	resource.attributes, 
	[{"key": "openshift.log.type", "value": {"stringValue": .log_type}},
	{"key": "openshift.log.source", "value": {"stringValue": .log_source}}]
)
# transform the record
r = {}
r.timeUnixNano = to_string(to_unix_timestamp(parse_timestamp!(.@timestamp, format:"%+"), unit:"nanoseconds"))
r.observedTimeUnixNano = to_string(to_unix_timestamp(now(), unit:"nanoseconds"))
# Convert syslog severity keyword to number, default to 9 (unknown)
r.severityNumber = to_syslog_severity(.level) ?? 9
r.body = {"stringValue": string!(.message)}
. = {
  "resource": resource,
  "logRecords": r
 }

`),
	}
}
func TransformAuditKube(id string, inputs []string) Element {
	return elements.Remap{
		Desc:        "Normalize audit log kube record to OTLP semantic conventions",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
		VRL: strings.TrimSpace(`
# OTLP semantic conventions for audit log kubeAPI records 
# Included attribute fields
meta = [
  "openshift.cluster_id",
  "hostname",
  "file"
]
replace = {
  "cluster.id": "cluster.uid",
  "hostname": "node.name",
  "file": "logs.file.path"
}
# Create resource attributes based on meta and replaces list
resource.attributes = []
for_each(meta) -> |_,value| {
  sub_key = value
  path = split(value,".")
  # if one or more dots (levels), replace the final underscores with dots
  if length(path) > 1 {
	sub_key = replace!(path[-1],"_",".")
  }
  # check for matches in replace
  if get!(replace, [sub_key]) != null {
	sub_key = string!(get!(replace, [sub_key]))
  } 
  # Add all fields to "resource.attributes.k8s"
  resource.attributes = append(
	resource.attributes,
	[{"key": "k8s." + sub_key, "value": {"stringValue": get!(.,path)}}]
  )
}
# Appending custom "openshift" attributes
resource.attributes = append(
	resource.attributes, 
	[{"key": "openshift.log.type", "value": {"stringValue": .log_type}},
	{"key": "openshift.log.source", "value": {"stringValue": .log_source}}]
)
# transform the record
r = {}
r.timeUnixNano = to_string(to_unix_timestamp(parse_timestamp!(.@timestamp, format:"%+"), unit:"nanoseconds"))
r.observedTimeUnixNano = to_string(to_unix_timestamp(now(), unit:"nanoseconds"))
# Convert syslog severity keyword to number, default to 9 (unknown)
r.severityNumber = to_syslog_severity(.level) ?? 9
r.body = {"stringValue": string!(.message)}
. = {
  "resource": resource,
  "logRecords": r
}

`),
	}
}
func TransformAuditOpenshift(id string, inputs []string) Element {
	return elements.Remap{
		Desc:        "Normalize audit log openshiftAPI record to OTLP semantic conventions",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
		VRL: strings.TrimSpace(`
# OTLP semantic conventions for audit log openshiftAPI records 
# Included attribute fields
meta = [
  "openshift.cluster_id",
  "hostname",
  "file"
]
replace = {
  "cluster.id": "cluster.uid",
  "hostname": "node.name",
  "file": "logs.file.path"
}
# Create resource attributes based on meta and replaces list
resource.attributes = []
for_each(meta) -> |_,value| {
  sub_key = value
  path = split(value,".")
  # if one or more dots (levels), replace the final underscores with dots
  if length(path) > 1 {
	sub_key = replace!(path[-1],"_",".")
  }
  # check for matches in replace
  if get!(replace, [sub_key]) != null {
	sub_key = string!(get!(replace, [sub_key]))
  } 
  # Add all fields to "resource.attributes.k8s"
  resource.attributes = append(
	resource.attributes,
	[{"key": "k8s." + sub_key, "value": {"stringValue": get!(.,path)}}]
  )
}
# Appending custom "openshift" attributes
resource.attributes = append(
	resource.attributes, 
	[{"key": "openshift.log.type", "value": {"stringValue": .log_type}},
	{"key": "openshift.log.source", "value": {"stringValue": .log_source}}]
)
# transform the record
r = {}
r.timeUnixNano = to_string(to_unix_timestamp(parse_timestamp!(.@timestamp, format:"%+"), unit:"nanoseconds"))
r.observedTimeUnixNano = to_string(to_unix_timestamp(now(), unit:"nanoseconds"))
# Convert syslog severity keyword to number, default to 9 (unknown)
r.severityNumber = to_syslog_severity(.level) ?? 9
r.body = {"stringValue": string!(.message)}
. = {
  "resource": resource,
  "logRecords": r
 }

`),
	}
}
func TransformAuditOvn(id string, inputs []string) Element {
	return elements.Remap{
		Desc:        "Normalize audit log ovn records to OTLP semantic conventions",
		ComponentID: id,
		Inputs:      helpers.MakeInputs(inputs...),
		VRL: strings.TrimSpace(`
# OTLP semantic conventions for audit log ovn records 
# Included attribute fields
meta = [
  "openshift.cluster_id",
  "hostname",
  "file"
]
replace = {
  "cluster.id": "cluster.uid",
  "hostname": "node.name",
  "file": "logs.file.path"
}
# Create resource attributes based on meta and replaces list
resource.attributes = []
for_each(meta) -> |_,value| {
  sub_key = value
  path = split(value,".")
  # if one or more dots (levels), replace the final underscores with dots
  if length(path) > 1 {
	sub_key = replace!(path[-1],"_",".")
  }
  # check for matches in replace
  if get!(replace, [sub_key]) != null {
	sub_key = string!(get!(replace, [sub_key]))
  } 
  # Add all fields to "resource.attributes.k8s"
  resource.attributes = append(
	resource.attributes,
	[{"key": "k8s." + sub_key, "value": {"stringValue": get!(.,path)}}]
  )
}
# Appending custom "openshift" attributes
resource.attributes = append(
	resource.attributes, 
	[{"key": "openshift.log.type", "value": {"stringValue": .log_type}},
	{"key": "openshift.log.source", "value": {"stringValue": .log_source}}]
)
# transform the record
r = {}
r.timeUnixNano = to_string(to_unix_timestamp(parse_timestamp!(.@timestamp, format:"%+"), unit:"nanoseconds"))
r.observedTimeUnixNano = to_string(to_unix_timestamp(now(), unit:"nanoseconds"))
# Convert syslog severity keyword to number, default to 9 (unknown)
r.severityNumber = to_syslog_severity(.level) ?? 9
r.body = {"stringValue": string!(.message)}
. = {
  "resource": resource,
  "logRecords": r
 }
`),
	}
}
