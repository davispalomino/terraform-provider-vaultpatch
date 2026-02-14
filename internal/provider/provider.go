package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ provider.Provider = &VaultPatchProvider{}

type VaultPatchProvider struct {
	version string
}

type VaultPatchProviderModel struct {
	Address  types.String `tfsdk:"address"`
	RoleID   types.String `tfsdk:"role_id"`
	SecretID types.String `tfsdk:"secret_id"`
}

type VaultClient struct {
	Address    string
	Token      string
	HTTPClient *http.Client
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &VaultPatchProvider{
			version: version,
		}
	}
}

func (p *VaultPatchProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "vaultpatch"
	resp.Version = p.version
}

func (p *VaultPatchProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Provider for partial key management in HashiCorp Vault KV v2 secrets. " +
			"Supports create, update, and delete of individual keys without affecting other keys in the same secret path.",
		Attributes: map[string]schema.Attribute{
			"address": schema.StringAttribute{
				Description: "The URL of the Vault server (e.g., https://vault.example.com).",
				Required:    true,
				Sensitive:   false,
			},
			"role_id": schema.StringAttribute{
				Description: "The AppRole Role ID for authenticating with Vault.",
				Required:    true,
				Sensitive:   true,
			},
			"secret_id": schema.StringAttribute{
				Description: "The AppRole Secret ID for authenticating with Vault.",
				Required:    true,
				Sensitive:   true,
			},
		},
	}
}

func (p *VaultPatchProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config VaultPatchProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Address.IsUnknown() || config.Address.IsNull() {
		resp.Diagnostics.AddError("Missing Vault Address", "The 'address' attribute must be set.")
		return
	}
	if config.RoleID.IsUnknown() || config.RoleID.IsNull() {
		resp.Diagnostics.AddError("Missing Role ID", "The 'role_id' attribute must be set.")
		return
	}
	if config.SecretID.IsUnknown() || config.SecretID.IsNull() {
		resp.Diagnostics.AddError("Missing Secret ID", "The 'secret_id' attribute must be set.")
		return
	}

	address := config.Address.ValueString()
	roleID := config.RoleID.ValueString()
	secretID := config.SecretID.ValueString()

	token, err := authenticateAppRole(address, roleID, secretID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Vault Authentication Failed",
			fmt.Sprintf("Could not authenticate with Vault at %s: %s", address, err),
		)
		return
	}

	client := &VaultClient{
		Address: address,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

func (p *VaultPatchProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewKvKeysResource,
	}
}

func (p *VaultPatchProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

func authenticateAppRole(address, roleID, secretID string) (string, error) {
	loginURL := fmt.Sprintf("%s/v1/auth/approle/login", address)

	payload := map[string]string{
		"role_id":   roleID,
		"secret_id": secretID,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal login payload: %w", err)
	}

	resp, err := http.Post(loginURL, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return "", fmt.Errorf("failed to send login request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read login response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse login response: %w", err)
	}

	if result.Auth.ClientToken == "" {
		return "", fmt.Errorf("vault returned empty client token")
	}

	return result.Auth.ClientToken, nil
}
