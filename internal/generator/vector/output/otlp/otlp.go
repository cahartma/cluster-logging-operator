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
	ComponentID string
	Inputs      string
	URI         string
	common.RootMixin
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
	transformContainerID := vectorhelpers.MakeID(id, "pre_otlp_container")
	transformJournalID := vectorhelpers.MakeID(id, "pre_otlp_journal")
	reduceContainerID := vectorhelpers.MakeID(id, "group_by_container")
	reduceJournalID := vectorhelpers.MakeID(id, "group_by_node")
	formatID := vectorhelpers.MakeID(id, "post_otlp")

	els = append(els, RouteJournal(rerouteID, inputs))
	els = append(els, TransformContainer(transformContainerID, []string{rerouteID + ".container"}))
	els = append(els, GroupByContainer(reduceContainerID, []string{transformContainerID}))
	els = append(els, TransformJournal(transformJournalID, []string{rerouteID + ".journal"}))
	els = append(els, GroupByNode(reduceJournalID, []string{transformJournalID}))
	els = append(els, FormatBatch(formatID, []string{reduceContainerID, reduceJournalID}))

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
	req.SetHeaders(headers)
	return req
}
