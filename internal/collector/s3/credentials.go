package s3

import (
	_ "embed"
	obs "github.com/openshift/cluster-logging-operator/api/observability/v1"
	"github.com/openshift/cluster-logging-operator/internal/api/observability"
	"regexp"
)

// RequiresProfilesConfigMap determine if a credentials configMap should be created for AWS
func RequiresProfilesConfigMap(outputs []obs.OutputSpec) bool {
	for _, o := range outputs {
		if found, _ := OutputIsS3RoleAuth(o); found {
			return true
		}
	}
	return false
}

// OutputIsS3RoleAuth identifies if `output.s3.Authentication.IAMRole` exists and returns ref if so
func OutputIsS3RoleAuth(o obs.OutputSpec) (bool, *obs.S3Authentication) {
	if o.S3 != nil && o.S3.Authentication != nil && o.S3.Authentication.IAMRole != nil {
		return true, o.S3.Authentication
	}
	return false, nil
}

// OutputIsAssumeRole identifies if 'output.s3.Authentication.AssumeRole` exists and returns ref if so
func OutputIsAssumeRole(o obs.OutputSpec) (bool, *obs.AwsAssumeRole) {
	if o.S3 != nil && o.S3.Authentication != nil && o.S3.Authentication.AssumeRole != nil {
		return true, o.S3.Authentication.AssumeRole
	}
	return false, nil
}

// AssumeRoleHasExternalId identifies if externalID exists and returns the string
func AssumeRoleHasExternalId(assumeRole *obs.AwsAssumeRole) (bool, string) {
	if assumeRole.ExternalID != "" {
		return true, assumeRole.ExternalID
	}
	return false, ""
}

// AssumeRoleHasSessionName identifies if session name exists and returns the string
func AssumeRoleHasSessionName(assumeRole *obs.AwsAssumeRole) (bool, string) {
	if assumeRole.SessionName != "" {
		return true, assumeRole.SessionName
	}
	return false, ""
}

// ParseRoleArn search for valid AWS arn, return emtpy for no match
func ParseRoleArn(authSpec *obs.S3Authentication, secrets observability.Secrets) string {
	var roleString string
	if authSpec.IAMRole != nil {
		roleString = secrets.AsString(&authSpec.IAMRole.RoleARN)
	}
	return findSubstring(roleString)
}

// ParseAssumeRoleArn search for valid AWS assumeRole arn, return empty for no match
func ParseAssumeRoleArn(assumeRoleSpec *obs.AwsAssumeRole, secrets observability.Secrets) string {
	var roleString string
	if assumeRoleSpec != nil {
		roleString = secrets.AsString(&assumeRoleSpec.RoleARN)
	}
	return findSubstring(roleString)
}

// findSubstring matches regex on a valid AWS role arn and returns empty for no match
func findSubstring(roleString string) string {
	if roleString != "" {
		reg := regexp.MustCompile(`(arn:aws(.*)?:(iam|sts)::\d{12}:role\/\S+)\s?`)
		roleArn := reg.FindStringSubmatch(roleString)
		if roleArn != nil {
			return roleArn[1] // the capturing group is index 1
		}
	}
	return ""
}
