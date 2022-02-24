package cloudwatch

type AWSKey struct {
	KeyIDPath           string
	KeyPath             string
	KeyRoleArn          string
	KeyWebIdentityToken string
}

func (a AWSKey) Name() string {
	return "awsKeyTemplate"
}

func (a AWSKey) Template() string {
	// Is the role populated in the secret
	if len(a.KeyRoleArn) > 0 {
		return `{{define "` + a.Name() + `" -}}
<web_identity_credentials>
  role_session_name fluentd-log-forwarding
  role_arn "#{open({{ .KeyRoleArn }},'r') do |f|f.read.strip end}"
  web_identity_token_file "#{open({{ .KeyWebIdentityToken }},'r') do |f|f.read.strip end}"
</web_identity_credentials>
{{end}}`
	}
	// Use id and key
	return `{{define "` + a.Name() + `" -}}
aws_key_id "#{open({{ .KeyIDPath }},'r') do |f|f.read.strip end}"
aws_sec_key "#{open({{ .KeyPath }},'r') do |f|f.read.strip end}"
{{end}}`
}
