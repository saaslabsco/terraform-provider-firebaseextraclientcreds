// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/function"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure FirebaseExtraProvider satisfies various provider interfaces.
var _ provider.Provider = &FirebaseExtraProvider{}
var _ provider.ProviderWithFunctions = &FirebaseExtraProvider{}

// FirebaseExtraProvider defines the provider implementation.
type FirebaseExtraProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// FirebaseExtraProviderModel describes the provider data model.
type FirebaseExtraProviderModel struct {
	AccessToken types.String `tfsdk:"accesstoken"`
	Endpoint    types.String `tfsdk:"endpoint"`
}

func (p *FirebaseExtraProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "firebaseextra"
	resp.Version = p.version
}

func (p *FirebaseExtraProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"accesstoken": schema.StringAttribute{
				MarkdownDescription: "Access Token. Read more on https://firebase.google.com/docs/remote-config/automate-rc#curl. For progrmatically use https://stackoverflow.com/questions/53890526/how-do-i-create-an-access-token-from-service-account-credentials-using-rest-api, or simplest `gcloud auth print-access-token --impersonate-service-account=some-service-account-that-has-firebase-iam-access`",
				Sensitive:           true,
				Required:            true,
			},
			"endpoint": schema.StringAttribute{
				MarkdownDescription: "Firebase Endpoint",
				Optional:            true,
			},
		},
	}
}

func (p *FirebaseExtraProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data FirebaseExtraProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	// Configuration values are now available.
	// if data.Endpoint.IsNull() { /* ... */ }

	// Example client configuration for data sources and resources
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
	fc := &FirebaseClient{
		Client:      client,
		accesstoken: data.AccessToken.ValueString(),
		endpoint:    data.Endpoint.ValueString(),
	}
	resp.DataSourceData = fc
	resp.ResourceData = fc
}

func (p *FirebaseExtraProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewRemoteConfigResource,
	}
}

func (p *FirebaseExtraProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		//NewExampleDataSource,
	}
}

func (p *FirebaseExtraProvider) Functions(ctx context.Context) []func() function.Function {
	return []func() function.Function{
		//NewExampleFunction,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &FirebaseExtraProvider{
			version: version,
		}
	}
}
