package loki

import (
	"fmt"
	"strings"

	"github.com/openshift/cluster-logging-operator/internal/generator/vector/output"

	"github.com/openshift/cluster-logging-operator/internal/generator/vector/normalize"

	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/internal/constants"
	. "github.com/openshift/cluster-logging-operator/internal/generator"
	genhelper "github.com/openshift/cluster-logging-operator/internal/generator/helpers"
	. "github.com/openshift/cluster-logging-operator/internal/generator/vector/elements"
	vectorhelpers "github.com/openshift/cluster-logging-operator/internal/generator/vector/helpers"
	"github.com/openshift/cluster-logging-operator/internal/generator/vector/output/security"
	"github.com/openshift/cluster-logging-operator/internal/utils/sets"
	corev1 "k8s.io/api/core/v1"
)

const (
	logType                          = "log_type"
	lokiLabelKubernetesNamespaceName = "kubernetes.namespace_name"
	lokiLabelKubernetesPodName       = "kubernetes.pod_name"
	lokiLabelKubernetesHost          = "kubernetes.host"
	lokiLabelKubernetesContainerName = "kubernetes.container_name"
	podNamespace                     = "kubernetes.namespace_name"

	// OTel
	otellogType                          = "openshift.log_type"
	otellokiLabelKubernetesNamespaceName = "k8s.namespace_name"
	otellokiLabelKubernetesPodName       = "k8s.pod_name"
	otellokiLabelKubernetesContainerName = "k8s.container_name"
	otellokiLabelKubernetesNodeName      = "k8s.node_name"
)

var (
	defaultLabelKeys = []string{
		logType,

		//container labels
		lokiLabelKubernetesNamespaceName,
		lokiLabelKubernetesPodName,
		lokiLabelKubernetesContainerName,

		// OTel labels
		otellogType,
		otellokiLabelKubernetesNamespaceName,
		otellokiLabelKubernetesPodName,
		otellokiLabelKubernetesContainerName,
	}
	requiredLabelKeys = []string{
		otellokiLabelKubernetesNodeName,
		lokiLabelKubernetesHost,
	}
	viaqOtelLabelMap = map[string]string{
		logType:                          otellogType,
		lokiLabelKubernetesNamespaceName: otellokiLabelKubernetesNamespaceName,
		lokiLabelKubernetesPodName:       otellokiLabelKubernetesPodName,
		lokiLabelKubernetesContainerName: otellokiLabelKubernetesContainerName,
	}
	lokiEncodingJson = fmt.Sprintf("%q", "json")
)

type Loki struct {
	ComponentID string
	Inputs      string
	TenantID    Element
	Endpoint    string
	LokiLabel   []string
}

func (l Loki) Name() string {
	return "lokiVectorTemplate"
}

func (l Loki) Template() string {
	return `{{define "` + l.Name() + `" -}}
[sinks.{{.ComponentID}}]
type = "loki"
inputs = {{.Inputs}}
endpoint = "{{.Endpoint}}"
out_of_order_action = "accept"
healthcheck.enabled = false
{{kv .TenantID -}}
{{end}}`
}

type LokiEncoding struct {
	ComponentID string
	Codec       string
}

func (le LokiEncoding) Name() string {
	return "lokiEncoding"
}

func (le LokiEncoding) Template() string {
	return `{{define "` + le.Name() + `" -}}
[sinks.{{.ComponentID}}.encoding]
codec = {{.Codec}}
{{end}}`
}

type Label struct {
	Name  string
	Value string
}

type LokiLabels struct {
	ComponentID string
	Labels      []Label
}

func (l LokiLabels) Name() string {
	return "lokiLabels"
}

func (l LokiLabels) Template() string {
	return `{{define "` + l.Name() + `" -}}
[sinks.{{.ComponentID}}.labels]
{{range $i, $label := .Labels -}}
{{$label.Name}} = "{{$label.Value}}"
{{end -}}
{{end}}
`
}

func Conf(o logging.OutputSpec, inputs []string, secret *corev1.Secret, op Options) []Element {
	id := vectorhelpers.FormatComponentID(o.Name)
	if genhelper.IsDebugOutput(op) {
		return []Element{
			Debug(id, vectorhelpers.MakeInputs(inputs...)),
		}
	}
	componentID := fmt.Sprintf("%s_%s", id, "remap")
	dedottedID := normalize.ID(id, "dedot")
	return MergeElements(
		[]Element{
			CleanupFields(componentID, inputs),
			normalize.DedotLabels(dedottedID, []string{componentID}),
			Output(o, []string{dedottedID}),
			Encoding(o),
			output.NewBuffer(id),
			output.NewRequest(id),
			Labels(o),
		},
		TLSConf(o, secret, op),
		BasicAuth(o, secret),
		BearerTokenAuth(o, secret),
	)
}

func Output(o logging.OutputSpec, inputs []string) Element {
	return Loki{
		ComponentID: strings.ToLower(vectorhelpers.Replacer.Replace(o.Name)),
		Inputs:      vectorhelpers.MakeInputs(inputs...),
		Endpoint:    o.URL,
		TenantID:    Tenant(o.Loki),
	}
}

func Encoding(o logging.OutputSpec) Element {
	return LokiEncoding{
		ComponentID: strings.ToLower(vectorhelpers.Replacer.Replace(o.Name)),
		Codec:       lokiEncodingJson,
	}
}

func lokiLabelKeys(l *logging.Loki) []string {
	var keys sets.String
	if l != nil && len(l.LabelKeys) != 0 {
		keys = *sets.NewString(l.LabelKeys...)
		// Determine which of the OTel labels need to also be added based on spec'd custom labels
		keys.Insert(addOtelEquivalentLabels(l.LabelKeys)...)
	} else {
		keys = *sets.NewString(defaultLabelKeys...)
	}
	// Ensure required tags for serialization
	keys.Insert(requiredLabelKeys...)
	return keys.List()
}

func lokiLabels(lo *logging.Loki) []Label {
	ls := []Label{}
	for _, k := range lokiLabelKeys(lo) {
		r := strings.NewReplacer(".", "_", "/", "_", "\\", "_", "-", "_")
		name := r.Replace(k)
		l := Label{
			Name:  name,
			Value: formatLokiLabelValue(k),
		}
		if val := generateCustomLabelValues(k); val != "" {
			l.Value = val
		}
		ls = append(ls, l)
	}
	return ls
}

// addOtelEquivalentLabels checks spec'd custom label keys to add matching otel labels
// e.g kubernetes.namespace_name = k8s.namespace_name
func addOtelEquivalentLabels(customLabelKeys []string) []string {
	matchingLabels := []string{}

	for _, label := range customLabelKeys {
		if val, ok := viaqOtelLabelMap[label]; ok {
			matchingLabels = append(matchingLabels, val)
		}
	}
	return matchingLabels
}

// generateCustomLabelValues generates custom values for specific labels like kubernetes.host, k8s_* labels
func generateCustomLabelValues(value string) string {
	var labelVal string

	switch value {
	case otellogType:
		labelVal = logType
	case otellokiLabelKubernetesContainerName:
		labelVal = lokiLabelKubernetesContainerName
	case lokiLabelKubernetesNamespaceName, otellokiLabelKubernetesNamespaceName:
		labelVal = podNamespace
	case otellokiLabelKubernetesPodName:
		labelVal = lokiLabelKubernetesPodName
	// Special case for the kubernetes node name (same as kubernetes.host)
	case lokiLabelKubernetesHost, otellokiLabelKubernetesNodeName:
		return "${VECTOR_SELF_NODE_NAME}"
	default:
		return ""
	}
	return fmt.Sprintf("{{%s}}", labelVal)
}

func formatLokiLabelValue(value string) string {
	if strings.HasPrefix(value, "kubernetes.labels.") || strings.HasPrefix(value, "kubernetes.namespace_labels.") {
		parts := strings.SplitAfterN(value, "labels.", 2)
		r := strings.NewReplacer("/", "_", ".", "_")
		key := r.Replace(parts[1])
		key = fmt.Sprintf(`\"%s\"`, key)
		value = fmt.Sprintf("%s%s", parts[0], key)
	}
	return fmt.Sprintf("{{%s}}", value)
}

func Labels(o logging.OutputSpec) Element {
	return LokiLabels{
		ComponentID: strings.ToLower(vectorhelpers.Replacer.Replace(o.Name)),
		Labels:      lokiLabels(o.Loki),
	}
}

func Tenant(l *logging.Loki) Element {
	if l == nil || l.TenantKey == "" {
		return Nil
	}
	return KV("tenant_id", fmt.Sprintf("%q", fmt.Sprintf("{{%s}}", l.TenantKey)))
}

func TLSConf(o logging.OutputSpec, secret *corev1.Secret, op Options) []Element {
	conf := []Element{}
	if isDefaultOutput(o.Name) {
		// Set CA from logcollector ServiceAccount for internal Loki
		tlsConf := security.TLSConf{
			ComponentID: strings.ToLower(vectorhelpers.Replacer.Replace(o.Name)),
			CAFilePath:  `"/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"`,
		}
		tlsConf.SetTLSProfileFromOptions(op)
		return append(conf, tlsConf)
	}

	if o.Secret != nil || (o.TLS != nil && o.TLS.InsecureSkipVerify) {

		if tlsConf := security.GenerateTLSConf(o, secret, op, false); tlsConf != nil {
			tlsConf.NeedsEnabled = false
			conf = append(conf, tlsConf)
		}
	} else if secret != nil {
		// Use secret of logcollector service account as backup
		tlsConf := security.TLSConf{
			ComponentID: strings.ToLower(vectorhelpers.Replacer.Replace(o.Name)),
			CAFilePath:  `"/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"`,
		}
		tlsConf.SetTLSProfileFromOptions(op)
		conf = append(conf, tlsConf)
	}

	return conf
}

func isDefaultOutput(name string) bool {
	return strings.HasPrefix(name, "default-")
}

func BasicAuth(o logging.OutputSpec, secret *corev1.Secret) []Element {
	conf := []Element{}

	if o.Secret != nil {
		hasBasicAuth := false
		conf = append(conf, BasicAuthConf{
			Desc:        "Basic Auth Config",
			ComponentID: strings.ToLower(vectorhelpers.Replacer.Replace(o.Name)),
		})
		if security.HasUsernamePassword(secret) {
			hasBasicAuth = true
			up := UserNamePass{
				Username: security.GetFromSecret(secret, constants.ClientUsername),
				Password: security.GetFromSecret(secret, constants.ClientPassword),
			}
			conf = append(conf, up)
		}
		if !hasBasicAuth {
			return []Element{}
		}
	}

	return conf
}

func BearerTokenAuth(o logging.OutputSpec, secret *corev1.Secret) []Element {
	conf := []Element{}
	if secret != nil {
		// Inject token from secret, either provided by user using a custom secret
		// or from the default logcollector service account.
		if security.HasBearerTokenFileKey(secret) {
			conf = append(conf, BasicAuthConf{
				Desc:        "Bearer Auth Config",
				ComponentID: strings.ToLower(vectorhelpers.Replacer.Replace(o.Name)),
			}, BearerToken{
				Token: security.GetFromSecret(secret, constants.BearerTokenFileKey),
			})
		}
	}
	return conf
}

func CleanupFields(id string, inputs []string) Element {
	return Remap{
		ComponentID: id,
		Inputs:      vectorhelpers.MakeInputs(inputs...),
		VRL:         "del(.tag)",
	}
}
