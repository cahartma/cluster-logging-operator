package aws

import (
	_ "embed"
	"github.com/openshift/cluster-logging-operator/internal/collector/s3"
	"html/template"
	"strings"

	log "github.com/ViaQ/logerr/v2/log/static"
	obs "github.com/openshift/cluster-logging-operator/api/observability/v1"
	"github.com/openshift/cluster-logging-operator/internal/api/observability"
	"github.com/openshift/cluster-logging-operator/internal/collector/common"
	"github.com/openshift/cluster-logging-operator/internal/constants"
	"github.com/openshift/cluster-logging-operator/internal/reconcile"
	"github.com/openshift/cluster-logging-operator/internal/runtime"
	"github.com/openshift/cluster-logging-operator/internal/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	AWSCredentialsTemplate = template.Must(template.New("aws credentials").Parse(awsCredentialsTemplateStr))
	//go:embed credentials.tmpl
	awsCredentialsTemplateStr string
)

type AWSWebIdentity struct {
	Name                 string
	RoleARN              string
	WebIdentityTokenFile string
	AssumeRoleARN        string
	ExternalID           string
	SessionName          string
}

// ReconcileAWSCredentialsConfigMap reconciles a configmap with credential profile(s) for AWS output(s).
func ReconcileAWSCredentialsConfigMap(k8sClient client.Client, reader client.Reader, namespace, name string, outputs []obs.OutputSpec, secrets observability.Secrets, configMaps map[string]*corev1.ConfigMap, owner metav1.OwnerReference) (*corev1.ConfigMap, error) {
	log.V(3).Info("generating AWS ConfigMap")
	credString, err := GenerateAWSCredentialProfiles(outputs, secrets)

	if err != nil || credString == "" {
		return nil, err
	}

	configMap := runtime.NewConfigMap(
		namespace,
		name,
		map[string]string{
			constants.AWSCredentialsKey: credString,
		})

	utils.AddOwnerRefToObject(configMap, owner)
	return configMap, reconcile.Configmap(k8sClient, k8sClient, configMap)
}

// GenerateAWSCredentialProfiles generates AWS CLI profiles for a credentials file from spec'd AWS output role ARNs and returns the formatted content as a string.
func GenerateAWSCredentialProfiles(outputs []obs.OutputSpec, secrets observability.Secrets) (string, error) {
	// Gather all AWS output's role_arns/tokens
	webIds := GatherAWSWebIdentities(outputs, secrets)

	// No AWS outputs
	if webIds == nil {
		return "", nil
	}

	// Execute Go template to generate credential profile(s)
	w := &strings.Builder{}
	err := AWSCredentialsTemplate.Execute(w, webIds)
	if err != nil {
		return "", err
	}
	return w.String(), nil
}

// GatherAWSWebIdentities takes spec'd role arns and generates AWSWebIdentity objects with a name and token path from secret or projected SA token
func GatherAWSWebIdentities(outputs []obs.OutputSpec, secrets observability.Secrets) (webIds []AWSWebIdentity) {
	for _, o := range outputs {
		// Handle S3 outputs
		// TESTING TODO: only handling s3 here for now
		if isRoleAuth, s3Auth := s3.OutputIsS3RoleAuth(o); isRoleAuth {
			webId := AWSWebIdentity{
				Name:                 o.Name,
				RoleARN:              s3.ParseRoleArn(s3Auth, secrets),
				WebIdentityTokenFile: common.ServiceAccountBasePath(constants.TokenKey),
			}

			if o.S3.Authentication.IAMRole.Token.From == obs.BearerTokenFromSecret {
				secret := o.S3.Authentication.IAMRole.Token.Secret
				webId.WebIdentityTokenFile = common.SecretPath(secret.Name, secret.Key)
			}

			if isAssumeRole, assumeRoleSpec := s3.OutputIsAssumeRole(o); isAssumeRole {
				webId.AssumeRoleARN = secrets.AsString(&assumeRoleSpec.RoleARN)
				if found, extID := s3.AssumeRoleHasExternalId(assumeRoleSpec); found {
					webId.ExternalID = extID
				}
				if found, name := s3.AssumeRoleHasSessionName(assumeRoleSpec); found {
					webId.SessionName = name
				}
			}
			webIds = append(webIds, webId)
		}
	}
	return webIds
}
