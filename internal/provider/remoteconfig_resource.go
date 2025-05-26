// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"golang.org/x/oauth2/google"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &RemoteConfigResource{}
var _ resource.ResourceWithImportState = &RemoteConfigResource{}

func NewRemoteConfigResource() resource.Resource {
	return &RemoteConfigResource{}
}

type FirebaseClient struct {
	*http.Client
	accesstoken string
	endpoint    string
}

// RemoteConfigResource defines the resource implementation.
type RemoteConfigResource struct {
	client *FirebaseClient
}

// RemoteConfigResourceModel describes the resource data model.
type RemoteConfigResourceModel struct {
	ID              types.String                               `tfsdk:"id"`
	Project         types.String                               `tfsdk:"project"`
	Version         types.String                               `tfsdk:"version"`
	Etag            types.String                               `tfsdk:"etag"`
	Parameters      []RemoteConfigParameterModel               `tfsdk:"parameters"`
	ParameterGroups map[string]RemoteConfigParameterGroupModel `tfsdk:"parameter_groups"`
}

type RemoteConfigParameterGroupModel struct {
	Description types.String                          `tfsdk:"description",json:"description"`
	Parameters  map[string]RemoteConfigParameterModel `tfsdk:"parameters",json:"parameters"`
}

type RemoteConfigParameterModel struct {
	Name         types.String `tfsdk:"name"`
	Description  types.String `tfsdk:"description"`
	ValueType    types.String `tfsdk:"value_type"`
	DefaultValue types.String `tfsdk:"default_value"`
}

func (r *RemoteConfigResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_remoteconfig"
}

func (r *RemoteConfigResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "Remote Config represents a remoteconfig item in FireBase",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "An internal id to keep track with firebase",
			},
			"version": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Published remoteconfig version",
			},
			"etag": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Published etag version",
			},

			"project": schema.StringAttribute{
				MarkdownDescription: "Firebase Project ID",
				Required:            true,
			},
			"parameters": schema.ListNestedAttribute{
				Required: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "name",
						},
						"default_value": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "default_value",
						},
						"description": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "description",
						},
						"value_type": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "value type",
						},
					},
				},
			},

			"parameter_groups": schema.MapNestedAttribute{
				Optional: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"description": schema.StringAttribute{
							Required:            true,
							MarkdownDescription: "description",
						},
						"parameters": schema.MapNestedAttribute{
							Required: true,
							NestedObject: schema.NestedAttributeObject{
								Attributes: map[string]schema.Attribute{
									"name": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "name",
									},
									"default_value": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "default_value",
									},
									"description": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "description",
									},
									"value_type": schema.StringAttribute{
										Required:            true,
										MarkdownDescription: "value type",
									},
								},
							},
						},
					},
				},
			},
		},
		// end parameter group

	}
}

func (r *RemoteConfigResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*FirebaseClient)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *FirebaseClient, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.client = client
}

func (r *RemoteConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	data := &RemoteConfigResourceModel{}

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	payload := RemoteConfigUpdate{
		Parameters:      make(map[string]RemoteConfigParameter),
		ParameterGroups: make(map[string]RemoteConfigParameterGroup),
	}
	for _, item := range data.Parameters {
		payload.Parameters[item.Name.ValueString()] = RemoteConfigParameter{
			DefaultValue: ConfigValue{
				Value: item.DefaultValue.ValueString(),
			},
			Description: item.Description.ValueString(),
			ValueType:   item.ValueType.ValueString(),
		}
	}

	for name, item := range data.ParameterGroups {
		group := RemoteConfigParameterGroup{
			Description: item.Description.ValueString(),
			Parameters:  make(map[string]RemoteConfigParameter),
		}

		for pname, param := range item.Parameters {
			group.Parameters[pname] = RemoteConfigParameter{
				DefaultValue: ConfigValue{
					Value: param.DefaultValue.ValueString(),
				},
				Description: param.Description.ValueString(),
				ValueType:   param.ValueType.ValueString(),
			}
		}
		payload.ParameterGroups[name] = group
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Error encoding JSON: %v\n", err))
		return
	}

	//httpReq, err := http.NewRequest("POST", fmt.Sprintf("https://firebaseremoteconfig.googleapis.com/v1/projects/%s/remoteConfig", data.project))
	url := fmt.Sprintf("%s/v1/projects/%s/remoteConfig", r.client.endpoint, data.Project.ValueString())

	tflog.Trace(ctx, fmt.Sprintf("submit %s %s", url, string(jsonData)))

	// When creating, we force etag to always match
	// Read more here: https://firebase.google.com/docs/reference/remote-config/rest/v1/projects/updateRemoteConfig
	// This mean that when creating all data is lost and an operator should import existing state instead
	data.Etag = types.StringValue("*")

	if err = r.writeToFireBase(ctx, url, payload, data); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Failed to write data to firebase: %w", err))
		return
	}

	slices.SortFunc(data.Parameters, func(a, b RemoteConfigParameterModel) int {
		return strings.Compare(strings.ToLower(a.Name.ValueString()), strings.ToLower(b.Name.ValueString()))
	})

	// By this time etag and version should be filled
	//data.Version = types.StringValue(target.Version.VersionNumber)
	//tflog.Trace(ctx, fmt.Sprintf("dumpo header %w", httpResp.Header))
	//data.Etag = types.StringValue(httpResp.Header.Get("ETag"))
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RemoteConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data RemoteConfigResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	projectID := data.Project.ValueString()
	if projectID == "" {
		// This is when we import the state
		projectID = data.ID.ValueString()
		data.Project = types.StringValue(projectID)
	}

	url := fmt.Sprintf("%s/v1/projects/%s/remoteConfig", r.client.endpoint, projectID)

	tflog.Trace(ctx, fmt.Sprintf("refresh resource data from %s", url))
	tflog.Trace(ctx, fmt.Sprintf("dump data %v", data))
	httpReq, err := http.NewRequest("GET", url, nil)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+getAccessToken(r.client.accesstoken))
	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		resp.Diagnostics.AddError("refresh error", fmt.Sprintf("unable to make http request to update config to firebase: %w", err))
		return
	}

	defer httpResp.Body.Close()
	bodyBytes, err := io.ReadAll(httpResp.Body)

	tflog.Trace(ctx, fmt.Sprintf("firebase api response %s %s", url, string(bodyBytes)))
	tflog.Trace(ctx, fmt.Sprintf("firebase api header %w", httpResp.Header))

	var target RemoteConfigRead
	//err = json.NewDecoder(httpResp.Body).Decode(&target)
	err = json.Unmarshal(bodyBytes, &target)

	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create remote config on url: %s access token: %s \n%s, resp: %s", url, r.client.accesstoken, err, string(bodyBytes)))
		return
	}

	data.Version = types.StringValue(target.Version.VersionNumber)
	if httpResp.Header.Get("Etag") == "" {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("etag header is  missing in the response: %s %s", string(bodyBytes), httpResp.Header))
		return
	}

	data.Parameters = []RemoteConfigParameterModel{}
	for k, v := range target.Parameters {
		data.Parameters = append(data.Parameters, RemoteConfigParameterModel{
			Name:         types.StringValue(k),
			Description:  types.StringValue(v.Description),
			ValueType:    types.StringValue(v.ValueType),
			DefaultValue: types.StringValue(v.DefaultValue.Value),
		})
	}
	slices.SortFunc(data.Parameters, func(a, b RemoteConfigParameterModel) int {
		return strings.Compare(strings.ToLower(a.Name.ValueString()), strings.ToLower(b.Name.ValueString()))
	})

	data.ParameterGroups = make(map[string]RemoteConfigParameterGroupModel)
	for k, v := range target.ParameterGroups {
		data.ParameterGroups[k] = RemoteConfigParameterGroupModel{
			Description: types.StringValue(v.Description),
			Parameters:  make(map[string]RemoteConfigParameterModel),
		}

		for paramName, paramValue := range v.Parameters {
			data.ParameterGroups[k].Parameters[paramName] = RemoteConfigParameterModel{
				Name:         types.StringValue(paramName),
				Description:  types.StringValue(paramValue.Description),
				ValueType:    types.StringValue(paramValue.ValueType),
				DefaultValue: types.StringValue(paramValue.DefaultValue.Value),
			}
		}
	}

	slices.SortFunc(data.Parameters, func(a, b RemoteConfigParameterModel) int {
		return strings.Compare(strings.ToLower(a.Name.ValueString()), strings.ToLower(b.Name.ValueString()))
	})

	data.ID = types.StringValue(data.Project.ValueString())
	data.Version = types.StringValue(target.Version.VersionNumber)
	data.Etag = types.StringValue(httpResp.Header.Get("ETag"))
	tflog.Trace(ctx, fmt.Sprintf("refresh remote config for version %s", data.Version.ValueString(), data.Etag.ValueString()))

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RemoteConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data RemoteConfigResourceModel

	// Read Terraform plan data into the model
	diags := req.Plan.Get(ctx, &data)
	resp.Diagnostics.Append(diags...)

	tflog.Trace(ctx, fmt.Sprintf("DUMP AFTER UPDATE: %v", data))

	if resp.Diagnostics.HasError() {
		return
	}

	payload := RemoteConfigUpdate{
		Parameters:      make(map[string]RemoteConfigParameter),
		ParameterGroups: make(map[string]RemoteConfigParameterGroup),
	}
	for _, item := range data.Parameters {
		payload.Parameters[item.Name.ValueString()] = RemoteConfigParameter{
			DefaultValue: ConfigValue{
				Value: item.DefaultValue.ValueString(),
			},
			Description: item.Description.ValueString(),
			ValueType:   item.ValueType.ValueString(),
		}
	}
	slices.SortFunc(data.Parameters, func(a, b RemoteConfigParameterModel) int {
		return strings.Compare(strings.ToLower(a.Name.ValueString()), strings.ToLower(b.Name.ValueString()))
	})

	for name, item := range data.ParameterGroups {
		group := RemoteConfigParameterGroup{
			Description: item.Description.ValueString(),
			Parameters:  make(map[string]RemoteConfigParameter),
		}

		for pname, param := range item.Parameters {
			group.Parameters[pname] = RemoteConfigParameter{
				DefaultValue: ConfigValue{
					Value: param.DefaultValue.ValueString(),
				},
				Description: param.Description.ValueString(),
				ValueType:   param.ValueType.ValueString(),
			}
		}
		payload.ParameterGroups[name] = group
	}

	var state RemoteConfigResourceModel
	diags2 := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags2...)
	if resp.Diagnostics.HasError() {
		return
	}

	data.Etag = types.StringValue(state.Etag.ValueString())

	//httpReq, err := http.NewRequest("POST", fmt.Sprintf("https://firebaseremoteconfig.googleapis.com/v1/projects/%s/remoteConfig", data.project))
	url := fmt.Sprintf("%s/v1/projects/%s/remoteConfig", r.client.endpoint, data.Project.ValueString())

	if err := r.writeToFireBase(ctx, url, payload, &data); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Failed to write data to firebase: %w", err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *RemoteConfigResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data RemoteConfigResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// If applicable, this is a great opportunity to initialize any necessary
	// provider client data and make a call using it.
	// httpResp, err := r.client.Do(httpReq)
	// if err != nil {
	//     resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete example, got error: %s", err))
	//     return
	// }
}

func (r *RemoteConfigResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func (r *RemoteConfigResource) writeToFireBase(ctx context.Context, url string, payload RemoteConfigUpdate, data *RemoteConfigResourceModel) error {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		tflog.Warn(ctx, fmt.Sprintf("Error encoding JSON: %v\n", err))
		return err
	}

	httpReq, err := http.NewRequest("PUT", url, bytes.NewReader(jsonData))
	httpReq.Header.Set("Content-Type", "application/json")
	tflog.Trace(ctx, fmt.Sprintf("dump data %v", data))
	tflog.Trace(ctx, fmt.Sprintf("prepare to update remote config url: %s etag: %s version %s payload: %s", url, data.Etag.ValueString(), data.Version.ValueString(), string(jsonData)))
	httpReq.Header.Set("If-Match", data.Etag.ValueString())

	httpReq.Header.Set("Authorization", "Bearer "+getAccessToken(r.client.accesstoken))
	httpResp, err := r.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("unable to make http request to update config to firebase: %w", err)
	}

	defer httpResp.Body.Close()
	bodyBytes, err := io.ReadAll(httpResp.Body)

	tflog.Trace(ctx, fmt.Sprintf("firebase api response %s %s", url, string(bodyBytes)))
	tflog.Trace(ctx, fmt.Sprintf("firebase api header %w", httpResp.Header))

	var target RemoteConfigRead
	//err = json.NewDecoder(httpResp.Body).Decode(&target)
	err = json.Unmarshal(bodyBytes, &target)

	if err != nil {
		return fmt.Errorf("Unable to create remote config on url: %s access token: %s \n%s, resp: %s", url, r.client.accesstoken, err, string(bodyBytes))
	}

	if httpResp.Header.Get("Etag") == "" {
		return fmt.Errorf("cannot write to firebase:\n%s", string(bodyBytes))
	}

	data.Version = types.StringValue(target.Version.VersionNumber)
	data.Etag = types.StringValue(httpResp.Header.Get("ETag"))
	data.ID = types.StringValue(data.Project.ValueString())

	tflog.Trace(ctx, fmt.Sprintf("publish remote config with version %s and etag %s", data.Version, data.Etag))

	return nil
}

type ConfigValue struct {
	Value string `json:"value"`
}
type RemoteConfigParameter struct {
	DefaultValue ConfigValue `json:"defaultValue"`
	Description  string      `json:"description"`
	ValueType    string      `json:"valueType"`
}

type RemoteConfigParameterGroup struct {
	Description string                           `json:"description"`
	Parameters  map[string]RemoteConfigParameter `json:"parameters"`
}
type RemoteConfigVersion struct {
	VersionNumber string    `json:"versionNumber"`
	UpdateTime    time.Time `json:"updateTime"`
	UpdateUser    struct {
		Email string `json:"email"`
	} `json:"updateUser"`
	UpdateOrigin string `json:"updateOrigin"`
	UpdateType   string `json:"updateType"`
}

type RemoteConfigRead struct {
	Parameters      map[string]RemoteConfigParameter      `json:"parameters"`
	ParameterGroups map[string]RemoteConfigParameterGroup `json:"parameterGroups"`
	Version         RemoteConfigVersion                   `json:"version"`
}

type RemoteConfigUpdate struct {
	Parameters      map[string]RemoteConfigParameter      `json:"parameters"`
	ParameterGroups map[string]RemoteConfigParameterGroup `json:"parameter_groups"`
}

func getAccessToken(clientCreds string) string {
	scopes := []string{"https://www.googleapis.com/auth/cloud-platform"} // Specify required scopes

	// Find default credentials using the environment variable or ADC
	credentials, err := google.JWTConfigFromJSON([]byte(clientCreds), scopes...)
	if err != nil {
		panic(err)
	}

	// Get the access token
	token, err := credentials.TokenSource(context.Background()).Token()
	if err != nil {
		panic(err)
	}
	return token.AccessToken
}
