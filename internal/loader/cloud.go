package loader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	parametermanager "cloud.google.com/go/parametermanager/apiv1"
	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/rakunlabs/chu/loader/external/loaderawssecrets"
	"github.com/rakunlabs/chu/loader/external/loaderawsssm"
	"github.com/rakunlabs/chu/loader/external/loaderazurekeyvault"
	"github.com/rakunlabs/chu/loader/external/loadergcpparameter"
	"github.com/rakunlabs/chu/loader/external/loadergcpsecret"
	"google.golang.org/api/option"
)

// cloudClients caches the chu external loaders, keyed by region, vault URL or
// location, so sources sharing the same target reuse one SDK client.
type cloudClients struct {
	awsSecrets    map[string]*loaderawssecrets.Loader
	awsSSM        map[string]*loaderawsssm.Loader
	azureKeyVault map[string]*loaderazurekeyvault.Loader
	gcpSecret     map[string]*loadergcpsecret.Loader
	gcpParameter  map[string]*loadergcpparameter.Loader
}

// envOr returns v when set, otherwise the first non-empty env value.
func envOr(v string, envs ...string) string {
	if v != "" {
		return v
	}

	for _, e := range envs {
		if ev := os.Getenv(e); ev != "" {
			return ev
		}
	}

	return ""
}

func awsRegion(region string) (string, error) {
	region = envOr(region, "AWS_REGION", "AWS_DEFAULT_REGION")
	if region == "" {
		return "", errors.New("aws region is required, set region or AWS_REGION env")
	}

	return region, nil
}

func (c *cloudClients) awsSecretsLoader(ctx context.Context, region string) (*loaderawssecrets.Loader, error) {
	if l, ok := c.awsSecrets[region]; ok {
		return l, nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load aws config: %w", err)
	}

	l := &loaderawssecrets.Loader{}
	l.SetClient(secretsmanager.NewFromConfig(cfg))

	if c.awsSecrets == nil {
		c.awsSecrets = map[string]*loaderawssecrets.Loader{}
	}

	c.awsSecrets[region] = l

	return l, nil
}

func (c *cloudClients) awsSSMLoader(ctx context.Context, region string) (*loaderawsssm.Loader, error) {
	if l, ok := c.awsSSM[region]; ok {
		return l, nil
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("failed to load aws config: %w", err)
	}

	l := &loaderawsssm.Loader{}
	l.SetClient(ssm.NewFromConfig(cfg))

	if c.awsSSM == nil {
		c.awsSSM = map[string]*loaderawsssm.Loader{}
	}

	c.awsSSM[region] = l

	return l, nil
}

func (c *cloudClients) azureKeyVaultLoader(vaultURL string) (*loaderazurekeyvault.Loader, error) {
	if l, ok := c.azureKeyVault[vaultURL]; ok {
		return l, nil
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create azure credential: %w", err)
	}

	client, err := azsecrets.NewClient(vaultURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create azure keyvault client: %w", err)
	}

	l := &loaderazurekeyvault.Loader{}
	l.SetClient(client)

	if c.azureKeyVault == nil {
		c.azureKeyVault = map[string]*loaderazurekeyvault.Loader{}
	}

	c.azureKeyVault[vaultURL] = l

	return l, nil
}

// gcpEndpoint returns the client options for a regional endpoint, nothing for global.
func gcpEndpoint(service, location string) []option.ClientOption {
	if location == "global" {
		return nil
	}

	return []option.ClientOption{
		option.WithEndpoint(fmt.Sprintf("%s.%s.rep.googleapis.com:443", service, location)),
	}
}

func (c *cloudClients) gcpSecretLoader(ctx context.Context, location string) (*loadergcpsecret.Loader, error) {
	if l, ok := c.gcpSecret[location]; ok {
		return l, nil
	}

	client, err := secretmanager.NewClient(ctx, gcpEndpoint("secretmanager", location)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gcp secret manager client: %w", err)
	}

	l := &loadergcpsecret.Loader{}
	l.SetClient(client)

	if c.gcpSecret == nil {
		c.gcpSecret = map[string]*loadergcpsecret.Loader{}
	}

	c.gcpSecret[location] = l

	return l, nil
}

func (c *cloudClients) gcpParameterLoader(ctx context.Context, location string) (*loadergcpparameter.Loader, error) {
	if l, ok := c.gcpParameter[location]; ok {
		return l, nil
	}

	client, err := parametermanager.NewClient(ctx, gcpEndpoint("parametermanager", location)...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gcp parameter manager client: %w", err)
	}

	l := &loadergcpparameter.Loader{}
	l.SetClient(client)

	if c.gcpParameter == nil {
		c.gcpParameter = map[string]*loadergcpparameter.Loader{}
	}

	c.gcpParameter[location] = l

	return l, nil
}

// close releases the gRPC connections held by the GCP clients.
func (c *cloudClients) close() {
	for _, l := range c.gcpSecret {
		if client := l.Client(); client != nil {
			_ = client.Close()
		}
	}

	for _, l := range c.gcpParameter {
		if client := l.Client(); client != nil {
			_ = client.Close()
		}
	}
}

// ////////////////////////////////////////////////////////////////////////////

func (c *ConfigAWSSecrets) fetch(ctx context.Context, cl *clients) ([]byte, string, error) {
	region, err := awsRegion(c.Region)
	if err != nil {
		return nil, "", fmt.Errorf("aws_secrets: %w", err)
	}

	secretID := strings.Trim(c.SecretID, "/")
	if secretID == "" {
		return nil, "", errors.New("aws_secrets: secret_id is required")
	}

	if prefix := strings.Trim(envOr(c.Prefix, "AWS_SECRETS_PREFIX"), "/"); prefix != "" && !strings.HasPrefix(secretID, "arn:") {
		secretID = prefix + "/" + secretID
	}

	l, err := cl.cloud.awsSecretsLoader(ctx, region)
	if err != nil {
		return nil, "", err
	}

	data, err := l.LoadKey(ctx, secretID)
	if err != nil {
		return nil, "", fmt.Errorf("aws_secrets %s: %w", secretID, err)
	}

	return data, "", nil
}

func (c *ConfigAWSSSM) fetch(ctx context.Context, cl *clients) ([]byte, string, error) {
	region, err := awsRegion(c.Region)
	if err != nil {
		return nil, "", fmt.Errorf("aws_ssm: %w", err)
	}

	name := strings.Trim(c.Parameter, "/")
	if name == "" {
		return nil, "", errors.New("aws_ssm: parameter is required")
	}

	if prefix := strings.Trim(envOr(c.Prefix, "AWS_SSM_PREFIX"), "/"); prefix != "" {
		name = prefix + "/" + name
	}

	if !strings.HasPrefix(name, "arn:") {
		name = "/" + name
	}

	l, err := cl.cloud.awsSSMLoader(ctx, region)
	if err != nil {
		return nil, "", err
	}

	data, err := l.LoadKey(ctx, name)
	if err != nil {
		return nil, "", fmt.Errorf("aws_ssm %s: %w", name, err)
	}

	return data, "", nil
}

func (c *ConfigAzureKeyVault) fetch(ctx context.Context, cl *clients) ([]byte, string, error) {
	vaultURL := envOr(c.VaultURL, "AZURE_KEYVAULT_URL")
	if vaultURL == "" {
		if name := os.Getenv("AZURE_KEYVAULT_NAME"); name != "" {
			vaultURL = fmt.Sprintf("https://%s.vault.azure.net/", name)
		}
	}

	if vaultURL == "" {
		return nil, "", errors.New("azure_key_vault: vault_url is required, set vault_url or AZURE_KEYVAULT_URL/AZURE_KEYVAULT_NAME env")
	}

	secret := strings.ReplaceAll(strings.Trim(c.Secret, "/"), "/", "-")
	if secret == "" {
		return nil, "", errors.New("azure_key_vault: secret is required")
	}

	l, err := cl.cloud.azureKeyVaultLoader(vaultURL)
	if err != nil {
		return nil, "", err
	}

	data, err := l.LoadKey(ctx, secret)
	if err != nil {
		return nil, "", fmt.Errorf("azure_key_vault %s: %w", secret, err)
	}

	return data, "", nil
}

// gcpTarget resolves project, location and version with env fallbacks.
func gcpTarget(projectID, location, version, envLocation, envVersion string) (string, string, string, error) {
	projectID = envOr(projectID, "GCP_PROJECT_ID")
	if projectID == "" {
		return "", "", "", errors.New("project_id is required, set project_id or GCP_PROJECT_ID env")
	}

	location = envOr(location, envLocation)
	if location == "" {
		location = "global"
	}

	version = envOr(version, envVersion)
	if version == "" {
		version = "latest"
	}

	return projectID, location, version, nil
}

func (c *ConfigGCPSecret) fetch(ctx context.Context, cl *clients) ([]byte, string, error) {
	projectID, location, version, err := gcpTarget(c.ProjectID, c.Location, c.Version, "GCP_SECRET_LOCATION", "GCP_SECRET_VERSION")
	if err != nil {
		return nil, "", fmt.Errorf("gcp_secret: %w", err)
	}

	secret := strings.ReplaceAll(strings.Trim(c.Secret, "/"), "/", "-")
	if secret == "" {
		return nil, "", errors.New("gcp_secret: secret is required")
	}

	var resourceName string
	if location == "global" {
		resourceName = fmt.Sprintf("projects/%s/secrets/%s/versions/%s", projectID, secret, version)
	} else {
		resourceName = fmt.Sprintf("projects/%s/locations/%s/secrets/%s/versions/%s", projectID, location, secret, version)
	}

	l, err := cl.cloud.gcpSecretLoader(ctx, location)
	if err != nil {
		return nil, "", err
	}

	data, err := l.LoadKey(ctx, resourceName)
	if err != nil {
		return nil, "", fmt.Errorf("gcp_secret %s: %w", resourceName, err)
	}

	return data, "", nil
}

func (c *ConfigGCPParameter) fetch(ctx context.Context, cl *clients) ([]byte, string, error) {
	projectID, location, version, err := gcpTarget(c.ProjectID, c.Location, c.Version, "GCP_PARAMETER_LOCATION", "GCP_PARAMETER_VERSION")
	if err != nil {
		return nil, "", fmt.Errorf("gcp_parameter: %w", err)
	}

	parameter := strings.ReplaceAll(strings.Trim(c.Parameter, "/"), "/", "-")
	if parameter == "" {
		return nil, "", errors.New("gcp_parameter: parameter is required")
	}

	resourceName := fmt.Sprintf("projects/%s/locations/%s/parameters/%s/versions/%s", projectID, location, parameter, version)

	l, err := cl.cloud.gcpParameterLoader(ctx, location)
	if err != nil {
		return nil, "", err
	}

	data, err := l.LoadKey(ctx, resourceName)
	if err != nil {
		return nil, "", fmt.Errorf("gcp_parameter %s: %w", resourceName, err)
	}

	return data, "", nil
}
