package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

var _ resource.Resource = &KvKeysResource{}
var _ resource.ResourceWithImportState = &KvKeysResource{}

type KvKeysResource struct {
	client *VaultClient
}

type KvKeysResourceModel struct {
	ID    types.String `tfsdk:"id"`
	Mount types.String `tfsdk:"mount"`
	Path  types.String `tfsdk:"path"`
	Keys  types.Map    `tfsdk:"keys"`
}

func NewKvKeysResource() resource.Resource {
	return &KvKeysResource{}
}

func (r *KvKeysResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kv_keys"
}

func (r *KvKeysResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages individual keys within a Vault KV v2 secret path. " +
			"Only the specified keys are created, updated, or deleted — other keys in the same path are never touched.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier for this resource (mount/path).",
				Computed:    true,
			},
			"mount": schema.StringAttribute{
				Description: "The mount path of the KV v2 secrets engine (e.g., 'app_demo').",
				Required:    true,
			},
			"path": schema.StringAttribute{
				Description: "The path within the mount where the secret lives (e.g., 'my-service/test').",
				Required:    true,
			},
			"keys": schema.MapAttribute{
				Description: "A map of key-value pairs to manage within the secret. " +
					"Only these keys will be affected; existing keys not listed here are preserved.",
				Required:    true,
				Sensitive:   true,
				ElementType: types.StringType,
			},
		},
	}
}

func (r *KvKeysResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*VaultClient)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			"Expected *VaultClient, got something else.",
		)
		return
	}

	r.client = client
}

func (r *KvKeysResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan KvKeysResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	mount := plan.Mount.ValueString()
	path := plan.Path.ValueString()

	planKeys := make(map[string]string)
	resp.Diagnostics.Append(plan.Keys.ElementsAs(ctx, &planKeys, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Creating keys in Vault", map[string]interface{}{
		"mount": mount,
		"path":  path,
		"keys":  keysOnly(planKeys),
	})

	existingData, err := r.readSecret(mount, path)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to Read Existing Secret",
			fmt.Sprintf("Could not read %s/%s: %s", mount, path, err),
		)
		return
	}

	if !keysMatch(existingData, planKeys) {
		merged := mergeKeys(existingData, planKeys)

		if err := r.writeSecret(mount, path, merged); err != nil {
			resp.Diagnostics.AddError(
				"Failed to Write Secret",
				fmt.Sprintf("Could not write to %s/%s: %s", mount, path, err),
			)
			return
		}
	} else {
		tflog.Info(ctx, "All keys already exist with the same values, skipping write", map[string]interface{}{
			"mount": mount,
			"path":  path,
		})
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s/%s", mount, path))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *KvKeysResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state KvKeysResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	mount := state.Mount.ValueString()
	path := state.Path.ValueString()

	stateKeys := make(map[string]string)
	resp.Diagnostics.Append(state.Keys.ElementsAs(ctx, &stateKeys, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Reading keys from Vault", map[string]interface{}{
		"mount": mount,
		"path":  path,
	})

	existingData, err := r.readSecret(mount, path)
	if err != nil {
		tflog.Warn(ctx, "Could not read secret from Vault, removing from state", map[string]interface{}{
			"error": err.Error(),
		})
		resp.State.RemoveResource(ctx)
		return
	}

	currentKeys := make(map[string]string)
	for key := range stateKeys {
		if val, exists := existingData[key]; exists {
			currentKeys[key] = val
		}
	}

	if len(currentKeys) == 0 {
		tflog.Warn(ctx, "None of the managed keys exist in Vault, removing from state")
		resp.State.RemoveResource(ctx)
		return
	}

	keysMapValue, diags := types.MapValueFrom(ctx, types.StringType, currentKeys)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state.Keys = keysMapValue
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *KvKeysResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan KvKeysResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	mount := plan.Mount.ValueString()
	path := plan.Path.ValueString()

	planKeys := make(map[string]string)
	resp.Diagnostics.Append(plan.Keys.ElementsAs(ctx, &planKeys, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state KvKeysResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stateKeys := make(map[string]string)
	resp.Diagnostics.Append(state.Keys.ElementsAs(ctx, &stateKeys, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Updating keys in Vault", map[string]interface{}{
		"mount": mount,
		"path":  path,
		"keys":  keysOnly(planKeys),
	})

	existingData, err := r.readSecret(mount, path)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to Read Existing Secret",
			fmt.Sprintf("Could not read %s/%s: %s", mount, path, err),
		)
		return
	}

	for key := range stateKeys {
		if _, existsInPlan := planKeys[key]; !existsInPlan {
			delete(existingData, key)
		}
	}

	merged := mergeKeys(existingData, planKeys)

	if err := r.writeSecret(mount, path, merged); err != nil {
		resp.Diagnostics.AddError(
			"Failed to Write Secret",
			fmt.Sprintf("Could not write to %s/%s: %s", mount, path, err),
		)
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s/%s", mount, path))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *KvKeysResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state KvKeysResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	mount := state.Mount.ValueString()
	path := state.Path.ValueString()

	stateKeys := make(map[string]string)
	resp.Diagnostics.Append(state.Keys.ElementsAs(ctx, &stateKeys, false)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "Deleting keys from Vault", map[string]interface{}{
		"mount": mount,
		"path":  path,
		"keys":  keysOnly(stateKeys),
	})

	existingData, err := r.readSecret(mount, path)
	if err != nil {
		tflog.Warn(ctx, "Could not read secret during delete, assuming already cleaned up", map[string]interface{}{
			"error": err.Error(),
		})
		return
	}

	for key := range stateKeys {
		delete(existingData, key)
	}

	if err := r.writeSecret(mount, path, existingData); err != nil {
		resp.Diagnostics.AddError(
			"Failed to Write Secret After Delete",
			fmt.Sprintf("Could not update %s/%s after removing keys: %s", mount, path, err),
		)
		return
	}
}

func (r *KvKeysResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := req.ID

	idx := strings.Index(id, "/")
	if idx < 0 {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"Import ID must be in the format 'mount/path' (e.g., 'app_envs/my-service/test').",
		)
		return
	}

	mount := id[:idx]
	path := id[idx+1:]

	if mount == "" || path == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"Both mount and path must be non-empty. Format: 'mount/path'.",
		)
		return
	}

	existingData, err := r.readSecret(mount, path)
	if err != nil {
		resp.Diagnostics.AddError(
			"Failed to Read Secret During Import",
			fmt.Sprintf("Could not read %s/%s: %s", mount, path, err),
		)
		return
	}

	keysMapValue, diags := types.MapValueFrom(ctx, types.StringType, existingData)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := KvKeysResourceModel{
		ID:    types.StringValue(id),
		Mount: types.StringValue(mount),
		Path:  types.StringValue(path),
		Keys:  keysMapValue,
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *KvKeysResource) readSecret(mount, path string) (map[string]string, error) {
	url := fmt.Sprintf("%s/v1/%s/data/%s", r.client.Address, mount, path)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-Vault-Token", r.client.Token)
	req.Header.Set("X-Vault-Request", "true")

	resp, err := r.client.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return make(map[string]string), nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vault returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data struct {
			Data map[string]interface{} `json:"data"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Data.Data == nil {
		return make(map[string]string), nil
	}

	data := make(map[string]string)
	for k, v := range result.Data.Data {
		data[k] = fmt.Sprintf("%v", v)
	}

	return data, nil
}

func (r *KvKeysResource) writeSecret(mount, path string, data map[string]string) error {
	url := fmt.Sprintf("%s/v1/%s/data/%s", r.client.Address, mount, path)

	payload := map[string]interface{}{
		"data": data,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("X-Vault-Token", r.client.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("vault returned status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func mergeKeys(existingData, newKeys map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range existingData {
		merged[k] = v
	}
	for k, v := range newKeys {
		merged[k] = v
	}
	return merged
}

func keysMatch(existing, planned map[string]string) bool {
	for k, v := range planned {
		if ev, ok := existing[k]; !ok || ev != v {
			return false
		}
	}
	return true
}

func keysOnly(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}
