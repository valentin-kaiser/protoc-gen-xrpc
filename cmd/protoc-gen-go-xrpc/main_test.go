package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

func TestParseOptions(t *testing.T) {
	tests := []struct {
		name               string
		input              string
		wantRelative       bool
		wantOpenAPIDir     string
		wantOpenAPIVersion string
		wantModule         string
		wantErr            string
	}{
		{name: "empty", input: ""},
		{name: "source relative", input: "paths=source_relative", wantRelative: true},
		{name: "module only", input: "module=github.com/example", wantModule: "github.com/example"},
		{name: "combined with spaces", input: " paths=source_relative , module=github.com/example ", wantRelative: true, wantModule: "github.com/example"},
		{name: "OpenAPI directory", input: "openapi_dir=api/specs", wantOpenAPIDir: "api/specs"},
		{name: "OpenAPI version", input: "openapi_version=1.2.3", wantOpenAPIVersion: "1.2.3"},
		{name: "unknown option", input: "paths=source_relative,foo=bar", wantErr: "unknown parameter: foo=bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts, err := parseOptions(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if opts.relative != tt.wantRelative {
				t.Fatalf("expected relative=%v, got %v", tt.wantRelative, opts.relative)
			}

			if opts.module != tt.wantModule {
				t.Fatalf("expected module=%q, got %q", tt.wantModule, opts.module)
			}
			if opts.openAPIDir != tt.wantOpenAPIDir {
				t.Fatalf("expected openAPIDir=%q, got %q", tt.wantOpenAPIDir, opts.openAPIDir)
			}
			if opts.openAPIVersion != tt.wantOpenAPIVersion {
				t.Fatalf("expected openAPIVersion=%q, got %q", tt.wantOpenAPIVersion, opts.openAPIVersion)
			}
		})
	}
}

func TestGenerate_SourceRelativePathOption(t *testing.T) {
	req := buildRequestForPortal([]*descriptorpb.FileDescriptorProto{buildMerpTypesProto()})
	req.Parameter = proto.String("paths=source_relative")

	resp := generate(req)
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}

	if len(resp.File) != 2 {
		t.Fatalf("expected exactly two generated files, got %d", len(resp.File))
	}

	if got, want := normalizePath(findGeneratedFile(t, resp, ".pb.go").GetName()), "portal/v1/portal_xrpc.pb.go"; got != want {
		t.Fatalf("expected generated filename %q, got %q", want, got)
	}
}

func TestGenerate_ModulePathOption(t *testing.T) {
	req := buildRequestForPortal([]*descriptorpb.FileDescriptorProto{buildMerpTypesProto()})
	req.Parameter = proto.String("module=github.com/example")

	resp := generate(req)
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}

	if len(resp.File) != 2 {
		t.Fatalf("expected exactly two generated files, got %d", len(resp.File))
	}

	if got, want := normalizePath(findGeneratedFile(t, resp, ".pb.go").GetName()), "portal/v1/portal_xrpc.pb.go"; got != want {
		t.Fatalf("expected generated filename %q, got %q", want, got)
	}
}

func TestGenerate_UnknownOptionReturnsError(t *testing.T) {
	req := buildRequestForPortal([]*descriptorpb.FileDescriptorProto{buildMerpTypesProto()})
	req.Parameter = proto.String("bad=true")

	resp := generate(req)
	if resp.GetError() == "" {
		t.Fatal("expected generate to return an error")
	}

	if !strings.Contains(resp.GetError(), "Failed to parse options") {
		t.Fatalf("expected parse-options error, got: %s", resp.GetError())
	}
}

func TestGenerate_NoServicesProducesNoFile(t *testing.T) {
	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"empty/v1/empty.proto"},
		ProtoFile: []*descriptorpb.FileDescriptorProto{
			{
				Name:    proto.String("empty/v1/empty.proto"),
				Package: proto.String("empty.v1"),
				Options: &descriptorpb.FileOptions{GoPackage: proto.String("github.com/example/empty/v1;v1")},
			},
		},
	}

	resp := generate(req)
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}

	if len(resp.File) != 0 {
		t.Fatalf("expected no generated files, got %d", len(resp.File))
	}
}

func TestGenerate_StreamingMethodShapes(t *testing.T) {
	req := buildStreamingRequest()
	content := mustGenerateSingleContent(t, req)

	checks := []string{
		"Unary(ctx context.Context, in *Req) (*Res, error)",
		"ClientStream(ctx context.Context, in <-chan *Req) (*Res, error)",
		"ServerStream(ctx context.Context, in *Req, out chan<- *Res) error",
		"Bidi(ctx context.Context, in <-chan *Req, out chan<- *Res) error",
		"func (c *StreamServiceClient) ClientStream(ctx context.Context, in <-chan *Req) (*Res, error)",
		"func (c *StreamServiceClient) ServerStream(ctx context.Context, in *Req, out chan<- *Res) error",
		"func (c *StreamServiceClient) Bidi(ctx context.Context, in <-chan *Req, out chan<- *Res) error",
	}

	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Fatalf("expected generated code to contain %q, got:\n%s", check, content)
		}
	}
}

func TestGenerate_OpenAPI(t *testing.T) {
	resp := generate(buildStreamingRequest())
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}

	var document map[string]any
	if err := json.Unmarshal([]byte(findGeneratedFile(t, resp, ".openapi.json").GetContent()), &document); err != nil {
		t.Fatalf("generated OpenAPI is not valid JSON: %v", err)
	}
	if got, want := document["openapi"], "3.1.0"; got != want {
		t.Fatalf("expected OpenAPI version %q, got %q", want, got)
	}
	if got, want := document["info"].(map[string]any)["version"], "0.0.0"; got != want {
		t.Fatalf("expected default document version %q, got %q", want, got)
	}

	paths := document["paths"].(map[string]any)
	if _, ok := paths["/StreamService/Unary"].(map[string]any)["post"]; !ok {
		t.Fatalf("expected unary POST operation, got: %#v", paths["/StreamService/Unary"])
	}
	for _, path := range []string{
		"/StreamService/ClientStream",
		"/StreamService/ServerStream",
		"/StreamService/Bidi",
	} {
		op, ok := paths[path].(map[string]any)["get"].(map[string]any)
		if !ok {
			t.Fatalf("expected streaming GET operation for %q, got: %#v", path, paths[path])
		}
		if op["x-stream"] == nil || op["responses"].(map[string]any)["101"] == nil {
			t.Fatalf("streaming operation %q must describe the stream kind and 101 response", path)
		}
		if op["requestBody"] != nil {
			t.Fatalf("streaming operation %q must not declare a requestBody for the handshake", path)
		}
		if _, ok := op["responses"].(map[string]any)["101"].(map[string]any)["content"]; ok {
			t.Fatalf("streaming operation %q must not put frame schemas in the 101 content", path)
		}
		messages, ok := op["x-stream-messages"].(map[string]any)
		if !ok || messages["client"] == nil || messages["server"] == nil {
			t.Fatalf("streaming operation %q must describe frames in x-stream-messages, got: %#v", path, op)
		}
	}

	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas["stream.v1.Req"]; !ok {
		t.Fatalf("expected request schema, got: %#v", schemas)
	}
}

func TestGenerate_OpenAPIVersion(t *testing.T) {
	req := buildStreamingRequest()
	req.Parameter = proto.String("openapi_version=1.2.3")
	resp := generate(req)
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}

	var document map[string]any
	if err := json.Unmarshal([]byte(findGeneratedFile(t, resp, ".openapi.json").GetContent()), &document); err != nil {
		t.Fatalf("generated OpenAPI is not valid JSON: %v", err)
	}
	if got, want := document["info"].(map[string]any)["version"], "1.2.3"; got != want {
		t.Fatalf("expected document version %q, got %q", want, got)
	}
}

func TestGenerate_OpenAPIOutputDirectory(t *testing.T) {
	req := buildRequestForPortal([]*descriptorpb.FileDescriptorProto{buildMerpTypesProto()})
	req.Parameter = proto.String("openapi_dir=api/specs")

	resp := generate(req)
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}

	if got, want := normalizePath(findGeneratedFile(t, resp, ".openapi.json").GetName()), "api/specs/portal/v1/portal.openapi.json"; got != want {
		t.Fatalf("expected generated OpenAPI filename %q, got %q", want, got)
	}
}

func TestGenerate_OpenAPISchemas(t *testing.T) {
	resp := generate(buildSchemaRequest())
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}

	var document map[string]any
	if err := json.Unmarshal([]byte(findGeneratedFile(t, resp, ".openapi.json").GetContent()), &document); err != nil {
		t.Fatalf("generated OpenAPI is not valid JSON: %v", err)
	}
	schemas := document["components"].(map[string]any)["schemas"].(map[string]any)
	request := schemas["schema.v1.Request"].(map[string]any)
	properties := request["properties"].(map[string]any)

	if got := properties["displayName"].(map[string]any)["type"]; got != "string" {
		t.Fatalf("expected JSON field name and string schema, got %#v", properties["displayName"])
	}
	if got := properties["numbers"].(map[string]any)["type"]; got != "array" {
		t.Fatalf("expected repeated field array schema, got %#v", properties["numbers"])
	}
	if _, ok := properties["labels"].(map[string]any)["additionalProperties"]; !ok {
		t.Fatalf("expected map field schema, got %#v", properties["labels"])
	}
	if got := properties["status"].(map[string]any)["enum"]; got == nil {
		t.Fatalf("expected enum schema, got %#v", properties["status"])
	}
	statusDescription, _ := properties["status"].(map[string]any)["description"].(string)
	if !strings.Contains(statusDescription, "Lifecycle status.") || !strings.Contains(statusDescription, "Ready for use.") {
		t.Fatalf("expected field comment merged with enum legend, got %q", statusDescription)
	}
	if _, ok := request["allOf"]; !ok {
		t.Fatalf("expected oneof constraint, got %#v", request)
	}
	if _, ok := schemas["schema.v1.Request.Child"]; !ok {
		t.Fatalf("expected nested message schema, got %#v", schemas)
	}
}

func TestGenerate_DescriptorUsesGoDescriptorIdent(t *testing.T) {
	req := buildRequestForPortal([]*descriptorpb.FileDescriptorProto{buildMerpTypesProto()})
	content := mustGenerateSingleContent(t, req)

	if !strings.Contains(content, "return File_portal_v1_portal_proto") {
		t.Fatalf("expected descriptor method to return nested descriptor ident, got:\n%s", content)
	}
}

func TestGenerate_CrossPackageReturnTypeUsesQualifiedAlias(t *testing.T) {
	req := buildRequestForPortal([]*descriptorpb.FileDescriptorProto{buildMerpTypesProto()})
	content := mustGenerateSingleContent(t, req)

	if !strings.Contains(content, `v1 "github.com/example/merp/v1"`) {
		t.Fatalf("expected generated import alias v1 for merp package, got:\n%s", content)
	}

	if !strings.Contains(content, "CreateTenant(ctx context.Context, in *CreateTenantRequest) (*v1.AcceptedRevision, error)") {
		t.Fatalf("expected generated signature to use *v1.AcceptedRevision, got:\n%s", content)
	}
}

func TestGenerate_AliasCollisionUsesDistinctAliases(t *testing.T) {
	req := buildRequestForPortal([]*descriptorpb.FileDescriptorProto{
		buildMerpTypesProto(),
		buildAcmeTypesProto(),
	})
	content := mustGenerateSingleContent(t, req)

	aliases := extractImportAliases(content)
	merpAlias, ok := aliases["github.com/example/merp/v1"]
	if !ok {
		t.Fatalf("expected import for github.com/example/merp/v1, got:\n%s", content)
	}

	acmeAlias, ok := aliases["github.com/example/acme/v1"]
	if !ok {
		t.Fatalf("expected import for github.com/example/acme/v1, got:\n%s", content)
	}

	if merpAlias == acmeAlias {
		t.Fatalf("expected distinct aliases for colliding package names, got %q", merpAlias)
	}

	if !strings.Contains(content, "*"+merpAlias+".AcceptedRevision") {
		t.Fatalf("expected merp type reference to use alias %q, got:\n%s", merpAlias, content)
	}

	if !strings.Contains(content, "*"+acmeAlias+".AcmeRevision") {
		t.Fatalf("expected acme type reference to use alias %q, got:\n%s", acmeAlias, content)
	}
}

func TestGenerate_Streaming(t *testing.T) {
	req := buildStreamingRequest()

	resp := generate(req)
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}
	if len(resp.File) != 2 {
		t.Fatalf("expected the Go file and the OpenAPI document, got %d files", len(resp.File))
	}
	file := findGeneratedFile(t, resp, ".pb.go")
	if !strings.HasSuffix(file.GetName(), "_xrpc.pb.go") {
		t.Fatalf("unexpected filename %q", file.GetName())
	}

	spec := findGeneratedFile(t, resp, ".openapi.json").GetContent()
	for _, check := range []string{`"jsonrpc"`, `"xrpc.Error"`, `"const": "StreamService.Unary"`, "xrpc.message"} {
		if !strings.Contains(spec, check) {
			t.Fatalf("expected OpenAPI document to contain %q, got:\n%s", check, spec)
		}
	}

	content := file.GetContent()
	checks := []string{
		"go-core/web/xrpc\"",
		"func RegisterStreamServiceServer(server StreamServiceServer) *xrpc.Service {",
		"return xrpc.Register(server)",
		"func NewStreamServiceClient(endpoint string, opts ...xrpc.ClientOption) (*StreamServiceClient, error) {",
		"c.client.Call(ctx, \"",
		"xrpc.ClientStream(ctx, c.client, ",
		"xrpc.ServerStream(ctx, c.client, ",
		"xrpc.BidiStream(ctx, c.client, ",
	}
	for _, check := range checks {
		if !strings.Contains(content, check) {
			t.Fatalf("expected generated code to contain %q, got:\n%s", check, content)
		}
	}
	if strings.Contains(content, "web/jrpc") {
		t.Fatalf("xrpc output must not import jrpc, got:\n%s", content)
	}
}

func mustGenerateSingleContent(t *testing.T, req *pluginpb.CodeGeneratorRequest) string {
	t.Helper()

	resp := generate(req)
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}

	return findGeneratedFile(t, resp, ".pb.go").GetContent()
}

func findGeneratedFile(t *testing.T, resp *pluginpb.CodeGeneratorResponse, suffix string) *pluginpb.CodeGeneratorResponse_File {
	t.Helper()

	for _, file := range resp.File {
		if strings.HasSuffix(file.GetName(), suffix) {
			return file
		}
	}

	t.Fatalf("generated response does not contain a file ending in %q", suffix)
	return nil
}

func buildRequestForPortal(externals []*descriptorpb.FileDescriptorProto) *pluginpb.CodeGeneratorRequest {
	portal := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("portal/v1/portal.proto"),
		Package: proto.String("portal.v1"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("github.com/example/portal/v1;v1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("CreateTenantRequest")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("PortalService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("CreateTenant"),
						InputType:  proto.String(".portal.v1.CreateTenantRequest"),
						OutputType: proto.String(".merp.v1.AcceptedRevision"),
					},
				},
			},
		},
		Dependency: []string{"merp/v1/types.proto"},
	}

	for _, ext := range externals {
		if ext.GetName() == "acme/v1/types.proto" {
			portal.Dependency = append(portal.Dependency, ext.GetName())
			portal.Service[0].Method = append(portal.Service[0].Method, &descriptorpb.MethodDescriptorProto{
				Name:       proto.String("CreateAcme"),
				InputType:  proto.String(".portal.v1.CreateTenantRequest"),
				OutputType: proto.String(".acme.v1.AcmeRevision"),
			})
		}
	}

	protoFiles := append([]*descriptorpb.FileDescriptorProto{}, externals...)
	protoFiles = append(protoFiles, portal)

	return &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"portal/v1/portal.proto"},
		ProtoFile:      protoFiles,
	}
}

func buildMerpTypesProto() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("merp/v1/types.proto"),
		Package: proto.String("merp.v1"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("github.com/example/merp/v1;v1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("AcceptedRevision")},
		},
	}
}

func buildAcmeTypesProto() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("acme/v1/types.proto"),
		Package: proto.String("acme.v1"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("github.com/example/acme/v1;v1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("AcmeRevision")},
		},
	}
}

func TestGenerate_SchemaTS(t *testing.T) {
	opt := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	rep := descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	msg := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	field := func(name, json string, n int32, label *descriptorpb.FieldDescriptorProto_Label, typ *descriptorpb.FieldDescriptorProto_Type, typeName string) *descriptorpb.FieldDescriptorProto {
		f := &descriptorpb.FieldDescriptorProto{Name: proto.String(name), JsonName: proto.String(json), Number: proto.Int32(n), Label: label, Type: typ}
		if typeName != "" {
			f.TypeName = proto.String(typeName)
		}
		return f
	}

	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("schema/v1/schema.proto"),
		Package: proto.String("schema.v1"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("github.com/example/schema/v1;v1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Child"), Field: []*descriptorpb.FieldDescriptorProto{field("display_name", "displayName", 1, opt, str, "")}},
			{
				Name: proto.String("Parent"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("child", "child", 2, opt, msg, ".schema.v1.Child"),
					field("items", "items", 3, rep, msg, ".schema.v1.Child"),
					field("by_id", "byId", 4, rep, msg, ".schema.v1.Parent.ByIdEntry"),
					field("tags", "tags", 5, rep, str, ""),
				},
				NestedType: []*descriptorpb.DescriptorProto{{
					Name:    proto.String("ByIdEntry"),
					Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
					Field: []*descriptorpb.FieldDescriptorProto{
						field("key", "key", 1, opt, str, ""),
						field("value", "value", 2, opt, msg, ".schema.v1.Child"),
					},
				}},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("Things"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       proto.String("Get"),
				InputType:  proto.String(".schema.v1.Child"),
				OutputType: proto.String(".schema.v1.Parent"),
			}},
		}},
	}

	req := &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"schema/v1/schema.proto"},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{file},
		Parameter:      proto.String("schema_ts=../ts/xrpc_schema.ts"),
	}
	resp := generate(req)
	if resp.GetError() != "" {
		t.Fatalf("generate returned error: %s", resp.GetError())
	}

	content := findGeneratedFile(t, resp, "xrpc_schema.ts").GetContent()
	for _, want := range []string{
		`"schema.v1.Child": {1: ["displayName"]},`,
		`"schema.v1.Parent": {2: ["child", "schema.v1.Child"], 3: ["items", "schema.v1.Child", 1], 4: ["byId", "schema.v1.Child", 2], 5: ["tags"]},`,
		`"Things.Get": ["schema.v1.Child", "schema.v1.Parent"],`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("schema is missing %s, got:\n%s", want, content)
		}
	}
	if strings.Contains(content, "ByIdEntry") {
		t.Errorf("map entries must not appear as messages, got:\n%s", content)
	}
}

func buildStreamingRequest() *pluginpb.CodeGeneratorRequest {
	stream := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("stream/v1/stream.proto"),
		Package: proto.String("stream.v1"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("github.com/example/stream/v1;v1")},
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: proto.String("Req")},
			{Name: proto.String("Res")},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("StreamService"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("Unary"),
						InputType:  proto.String(".stream.v1.Req"),
						OutputType: proto.String(".stream.v1.Res"),
					},
					{
						Name:            proto.String("ClientStream"),
						InputType:       proto.String(".stream.v1.Req"),
						OutputType:      proto.String(".stream.v1.Res"),
						ClientStreaming: proto.Bool(true),
					},
					{
						Name:            proto.String("ServerStream"),
						InputType:       proto.String(".stream.v1.Req"),
						OutputType:      proto.String(".stream.v1.Res"),
						ServerStreaming: proto.Bool(true),
					},
					{
						Name:            proto.String("Bidi"),
						InputType:       proto.String(".stream.v1.Req"),
						OutputType:      proto.String(".stream.v1.Res"),
						ClientStreaming: proto.Bool(true),
						ServerStreaming: proto.Bool(true),
					},
				},
			},
		},
	}

	return &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"stream/v1/stream.proto"},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{stream},
	}
}

func buildSchemaRequest() *pluginpb.CodeGeneratorRequest {
	request := &descriptorpb.DescriptorProto{
		Name:      proto.String("Request"),
		OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("selector")}},
		NestedType: []*descriptorpb.DescriptorProto{
			{
				Name:    proto.String("LabelsEntry"),
				Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("key"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
					{Name: proto.String("value"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			},
			{
				Name: proto.String("Child"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("id"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				},
			},
		},
		Field: []*descriptorpb.FieldDescriptorProto{
			{Name: proto.String("display_name"), JsonName: proto.String("displayName"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
			{Name: proto.String("numbers"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_INT32.Enum()},
			{Name: proto.String("labels"), Number: proto.Int32(3), Label: descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(), TypeName: proto.String(".schema.v1.Request.LabelsEntry")},
			{Name: proto.String("status"), Number: proto.Int32(4), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_ENUM.Enum(), TypeName: proto.String(".schema.v1.Status")},
			{Name: proto.String("child"), Number: proto.Int32(5), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(), TypeName: proto.String(".schema.v1.Request.Child")},
			{Name: proto.String("by_id"), Number: proto.Int32(6), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), OneofIndex: proto.Int32(0)},
			{Name: proto.String("by_name"), Number: proto.Int32(7), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), OneofIndex: proto.Int32(0)},
		},
	}
	file := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("schema/v1/schema.proto"),
		Package: proto.String("schema.v1"),
		Options: &descriptorpb.FileOptions{GoPackage: proto.String("github.com/example/schema/v1;v1")},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name:  proto.String("Status"),
			Value: []*descriptorpb.EnumValueDescriptorProto{{Name: proto.String("STATUS_UNSPECIFIED"), Number: proto.Int32(0)}, {Name: proto.String("STATUS_READY"), Number: proto.Int32(1)}},
		}},
		SourceCodeInfo: &descriptorpb.SourceCodeInfo{Location: []*descriptorpb.SourceCodeInfo_Location{
			{Path: []int32{4, 0, 2, 3}, Span: []int32{1, 0, 1}, LeadingComments: proto.String(" Lifecycle status.\n")},
			{Path: []int32{5, 0, 2, 1}, Span: []int32{2, 0, 1}, LeadingComments: proto.String(" Ready for use.\n")},
		}},
		MessageType: []*descriptorpb.DescriptorProto{request, {Name: proto.String("Response")}},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: proto.String("SchemaService"),
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name: proto.String("Inspect"), InputType: proto.String(".schema.v1.Request"), OutputType: proto.String(".schema.v1.Response"),
			}},
		}},
	}

	return &pluginpb.CodeGeneratorRequest{
		FileToGenerate: []string{"schema/v1/schema.proto"},
		ProtoFile:      []*descriptorpb.FileDescriptorProto{file},
	}
}

func extractImportAliases(content string) map[string]string {
	aliases := map[string]string{}
	re := regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\s+"([^"]+)"\s*$`)
	matches := re.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		aliases[m[2]] = m[1]
	}
	return aliases
}

func normalizePath(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}
