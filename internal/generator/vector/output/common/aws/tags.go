package aws

import (
	obs "github.com/openshift/cluster-logging-operator/api/observability/v1"
	"github.com/openshift/cluster-logging-operator/internal/generator/framework"
)

type Tags struct {
	Tags map[string]string
	ID   string
}

func (t Tags) Name() string {
	return "awsTagsTemplate"
}

func (t Tags) Template() (s string) {
	return `{{define "` + t.Name() + `" -}}
[sinks.{{.ID}}.tags]
{{- range $key, $value := .Tags }}
{{ printf "\"%s\" = \"%s\"\n" $key $value }}
{{- end -}}
{{end}}`
}

// NewTags adds the tags config to CloudWatch or S3 sink
func NewTags(id string, cw *obs.Cloudwatch) framework.Element {
	if cw.Tags == nil {
		return Tags{}
	}
	return Tags{
		Tags: cw.Tags,
		ID:   id,
	}
}
