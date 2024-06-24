package otlp

import (
	log "github.com/ViaQ/logerr/v2/log/static"
	obsv1 "github.com/openshift/cluster-logging-operator/api/observability/v1"
	. "github.com/openshift/cluster-logging-operator/internal/generator/framework"
	genhelper "github.com/openshift/cluster-logging-operator/internal/generator/helpers"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/elements"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/helpers"
	vectorhelpers "github.com/openshift/cluster-logging-operator/internal/generator/vector/helpers"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/output/common"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/output/common/auth"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/output/common/tls"
)

type Otlp struct {
	ComponentID      string
	Inputs           string
	URI              string
	common.RootMixin //TODO: remove??
}

func (p Otlp) Name() string {
	return "vectorOtlpTemplate"
}

func (p Otlp) Template() string {
	return `{{define "` + p.Name() + `" -}}
[sinks.{{.ComponentID}}]
type = "http"
inputs = {{.Inputs}}
uri = "{{.URI}}"
method = "post"
payload_prefix = "{\"resourceLogs\":"
payload_suffix = "}"
encoding.codec = "json"
{{.Compression}}
{{end}}
`
}

// TODO: remove this for otlp ?
func (p *Otlp) SetCompression(algo string) {
	p.Compression.Value = algo
}

func New(id string, o obsv1.OutputSpec, inputs []string, secrets vectorhelpers.Secrets, strategy common.ConfigStrategy, op Options) []Element {

	log.V(0).Info("===== OTLP output --- New() ---", "id", id, "outputSpec", o, "inputs", inputs, "secrets", secrets)

	if genhelper.IsDebugOutput(op) {
		return []Element{
			elements.Debug(helpers.MakeID(id, "debug"), vectorhelpers.MakeInputs(inputs...)),
		}
	}
	var els []Element
	rerouteID := vectorhelpers.MakeID(id, "reroute")
	transformContainerID := vectorhelpers.MakeID(id, "otlp_container")
	reduceContainerID := vectorhelpers.MakeID(id, "group_by_container")
	transformNodeID := vectorhelpers.MakeID(id, "otlp_node")
	reduceNodeID := vectorhelpers.MakeID(id, "group_by_node")
	//transformAuditID := vectorhelpers.MakeID(id, "otlp_audit")
	reduceAuditID := vectorhelpers.MakeID(id, "group_by_audit")
	formatID := vectorhelpers.MakeID(id, "final_otlp")

	transformAuditHostID := vectorhelpers.MakeID(id, "otlp_audit_linux")
	transformAuditKubeID := vectorhelpers.MakeID(id, "otlp_audit_kube")
	transformAuditOpenshiftID := vectorhelpers.MakeID(id, "otlp_audit_openshift")
	transformAuditOvnID := vectorhelpers.MakeID(id, "otlp_audit_ovn")

	els = append(els, RouteBySource(rerouteID, inputs))
	els = append(els, TransformContainer(transformContainerID, []string{rerouteID + ".container"}))
	els = append(els, GroupByContainer(reduceContainerID, []string{transformContainerID}))
	els = append(els, TransformJournal(transformNodeID, []string{rerouteID + ".journal"}))
	els = append(els, GroupByNode(reduceNodeID, []string{transformNodeID}))
	//els = append(els, TransformAudit(transformAuditID, []string{rerouteID + ".audit"}))

	els = append(els, TransformAuditHost(transformAuditHostID, []string{rerouteID + ".linux"}))
	els = append(els, TransformAuditKube(transformAuditKubeID, []string{rerouteID + ".kube"}))
	els = append(els, TransformAuditOpenshift(transformAuditOpenshiftID, []string{rerouteID + ".openshift"}))
	els = append(els, TransformAuditOvn(transformAuditOvnID, []string{rerouteID + ".ovn"}))

	els = append(els, GroupByAudit(reduceAuditID, []string{
		//transformAuditID,
		transformAuditHostID,
		transformAuditKubeID,
		transformAuditOpenshiftID,
		transformAuditOvnID,
	}))

	els = append(els, FormatBatch(formatID, []string{
		reduceContainerID,
		reduceNodeID,
		reduceAuditID,
		rerouteID + "._unmatched",
	}))

	sink := Output(id, o, []string{formatID}, secrets, op)
	if strategy != nil {
		strategy.VisitSink(sink)
	}
	return MergeElements(
		els,
		[]Element{
			sink,
			common.NewAcknowledgments(id, strategy),
			common.NewBatch(id, strategy),
			common.NewBuffer(id, strategy),
			Request(id, o, strategy),
			tls.New(id, o.TLS, secrets, op),
			auth.HTTPAuth(id, o.Otlp.Authentication, secrets),
		},
	)
}

func Output(id string, o obsv1.OutputSpec, inputs []string, secrets vectorhelpers.Secrets, op Options) *Otlp {
	return &Otlp{
		ComponentID: id,
		Inputs:      vectorhelpers.MakeInputs(inputs...),
		URI:         o.Otlp.URL,
		RootMixin:   common.NewRootMixin(nil),
	}
}

func Request(id string, o obsv1.OutputSpec, strategy common.ConfigStrategy) *common.Request {
	req := common.NewRequest(id, strategy)
	if o.Otlp != nil && o.Otlp.Timeout != 0 {
		req.TimeoutSecs.Value = o.Otlp.Timeout
	}
	headers := map[string]string{}
	if o.Otlp != nil && len(o.Otlp.Headers) != 0 {
		headers = o.Otlp.Headers
	}
	// required
	headers["Content-Type"] = "application/json"

	// TODO: does compression need to be set here?  Need to test existing tuning.compression
	// https://opentelemetry.io/docs/specs/otlp/#otlphttp
	// The client MAY gzip the content and in that case MUST include “Content-Encoding: gzip” request header.
	// headers["Content-Encoding"] = "gzip"
	req.SetHeaders(headers)
	return req
}
