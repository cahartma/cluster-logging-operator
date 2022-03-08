package cloudwatch

import (
	"fmt"
	"path/filepath"
	"strings"

	logging "github.com/openshift/cluster-logging-operator/apis/logging/v1"
	"github.com/openshift/cluster-logging-operator/internal/constants"
	. "github.com/openshift/cluster-logging-operator/internal/generator"
	. "github.com/openshift/cluster-logging-operator/internal/generator/fluentd/elements"
	"github.com/openshift/cluster-logging-operator/internal/generator/fluentd/helpers"
	"github.com/openshift/cluster-logging-operator/internal/generator/fluentd/output/security"
	"github.com/openshift/cluster-logging-operator/internal/generator/fluentd/source"
	genhelper "github.com/openshift/cluster-logging-operator/internal/generator/helpers"
	corev1 "k8s.io/api/core/v1"
)

type Endpoint struct {
	URL string
}

func (e Endpoint) Name() string {
	return "awsEndpointTemplate"
}

func (e Endpoint) Template() (ret string) {
	ret = `{{define "` + e.Name() + `" -}}`
	if e.URL != "" {
		ret += `endpoint {{ .URL }}
ssl_verify_peer false`
	}
	ret += `{{end}}`
	return
}

type CloudWatch struct {
	Region         string
	SecurityConfig Element
	EndpointConfig Element
}

func (cw CloudWatch) Name() string {
	return "cloudwatchTemplate"
}

func (cw CloudWatch) Template() string {
	return `{{define "` + cw.Name() + `" -}}
@type cloudwatch_logs
auto_create_stream true
region {{.Region }}
log_group_name_key cw_group_name
log_stream_name_key cw_stream_name
remove_log_stream_name_key true
remove_log_group_name_key true
concurrency 2
{{compose_one .SecurityConfig}}
include_time_key true
log_rejected_request true
{{compose_one .EndpointConfig}}
{{end}}`
}

func Conf(bufspec *logging.FluentdBufferSpec, secret *corev1.Secret, o logging.OutputSpec, op Options) []Element {
	logGroupPrefix := LogGroupPrefix(o)
	logGroupName := LogGroupName(o)
	return []Element{
		FromLabel{
			InLabel: helpers.LabelName(o.Name),
			SubElements: []Element{
				GroupNameStreamName(fmt.Sprintf("%s%s", logGroupPrefix, logGroupName),
					"${tag}",
					source.ApplicationTags),
				GroupNameStreamName(fmt.Sprintf("%sinfrastructure", logGroupPrefix),
					"${record['hostname']}.${tag}",
					source.InfraTags),
				GroupNameStreamName(fmt.Sprintf("%saudit", logGroupPrefix),
					"${record['hostname']}.${tag}",
					source.AuditTags),
				OutputConf(bufspec, secret, o, op),
			},
		},
	}
}

func OutputConf(bufspec *logging.FluentdBufferSpec, secret *corev1.Secret, o logging.OutputSpec, op Options) Element {
	if genhelper.IsDebugOutput(op) {
		return genhelper.DebugOutput
	}
	return Match{
		MatchTags: "**",
		MatchElement: CloudWatch{
			Region:         o.Cloudwatch.Region,
			SecurityConfig: SecurityConfig(o, secret),
			EndpointConfig: EndpointConfig(o),
		},
	}
}

func SecurityConfig(o logging.OutputSpec, secret *corev1.Secret) Element {
	// First check for credentials key, indicating a sts-enabled cluster
	if HasAwsCredentialsKey(secret) {
		// Parse values from the credentials string
		mountPath, filePath := ParseIdentityToken(secret)
		return AWSKey{
			KeyRoleArn:          ParseRoleArn(secret),
			KeyRoleSessionName:  constants.AWSRoleSessionName,
			KeyWebIdentityToken: mountPath + "/" + filePath,
		}
	}
	// Use ID and Secret
	return AWSKey{
		KeyID:     security.SecretPath(o.Secret.Name, constants.AWSAccessKeyID),
		KeySecret: security.SecretPath(o.Secret.Name, constants.AWSSecretAccessKey),
	}
}

func EndpointConfig(o logging.OutputSpec) Element {
	return Endpoint{
		URL: o.URL,
	}
}

func GroupNameStreamName(groupName, streamName, tag string) Element {
	return Filter{
		MatchTags: tag,
		Element: RecordModifier{
			Records: []Record{
				{
					Key:        "cw_group_name",
					Expression: groupName,
				},
				{
					Key:        "cw_stream_name",
					Expression: streamName,
				},
			},
		},
	}
}

func LogGroupPrefix(o logging.OutputSpec) string {
	if o.Cloudwatch != nil {
		prefix := o.Cloudwatch.GroupPrefix
		if prefix != nil && strings.TrimSpace(*prefix) != "" {
			return fmt.Sprintf("%s.", *prefix)
		}
	}
	return ""
}

func LogGroupName(o logging.OutputSpec) string {
	if o.Cloudwatch != nil {
		switch o.Cloudwatch.GroupBy {
		case logging.LogGroupByNamespaceName:
			return "${record['kubernetes']['namespace_name']}"
		case logging.LogGroupByNamespaceUUID:
			return "${record['kubernetes']['namespace_id']}"
		default:
			return logging.InputNameApplication
		}
	}
	return ""
}

func HasAwsCredentialsKey(secret *corev1.Secret) bool {
	return security.HasKeys(secret, constants.AWSCredentialsKey)
}

// ParseRoleArn search for 'arn:aws:iam:: in the credentials string and return string up to end of line
func ParseRoleArn(secret *corev1.Secret) string {
	credentials := security.GetFromSecret(secret, constants.AWSCredentialsKey)
	if credentials != "" {
		roleIndex := strings.Index(credentials, "arn:aws:iam::")
		if roleIndex != -1 { // found the role arn string
			roleArn := strings.Split(credentials[roleIndex:], "\n")
			return roleArn[0]
		}
	}
	return ""
}

// ParseIdentityToken split credentials string at 'web_identity_token_file = ' and return everything after
// Return volume mount path and token file path, or default values if exact separator key is not found
func ParseIdentityToken(secret *corev1.Secret) (mountPath, filePath string) {
	credentials := security.GetFromSecret(secret, constants.AWSCredentialsKey)
	if credentials != "" {
		split := strings.Split(credentials, "web_identity_token_file = ")
		if split[0] != credentials { // found the separator
			tokenMountPathWithFile := split[1]
			return filepath.Dir(tokenMountPathWithFile), filepath.Base(tokenMountPathWithFile)
		}
	}
	// Use default
	return constants.AWSWebIdentityTokenMount, constants.AWSWebIdentityTokenFilePath
}

func AppendVolumeActions(secret *corev1.Secret) (mount corev1.VolumeMount, volume corev1.Volume) {
	if HasAwsCredentialsKey(secret) {
		mountPath, filePath := ParseIdentityToken(secret)
		mount = corev1.VolumeMount{
			Name:      constants.AWSWebIdentityTokenName,
			ReadOnly:  true,
			MountPath: mountPath,
		}
		volume = corev1.Volume{
			Name: constants.AWSWebIdentityTokenName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Audience: "openshift",
								Path:     filePath,
							},
						},
					},
				},
			},
		}
	}
	return
}
