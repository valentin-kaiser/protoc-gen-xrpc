package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/valentin-kaiser/go-core/flag"
	"github.com/valentin-kaiser/go-core/version"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/pluginpb"
)

type generator struct {
	file        *protogen.File
	relative    bool
	genFile     *protogen.GeneratedFile
	opts        *options
	packageName string
	importPath  string
}

var (
	contextContextIdent = protogen.GoIdent{GoName: "Context", GoImportPath: "context"}
	errorsNewIdent      = protogen.GoIdent{GoName: "New", GoImportPath: "errors"}

	protoreflectFileDescriptorIdent = protogen.GoIdent{GoName: "FileDescriptor", GoImportPath: "google.golang.org/protobuf/reflect/protoreflect"}
)

type options struct {
	relative       bool
	module         string
	openAPIDir     string
	openAPIVersion string
	schemaTS       string
}

func main() {
	flag.Unregister("debug")
	flag.Unregister("path")
	flag.Init()

	if flag.Help {
		flag.PrintHelp()
		return
	}

	if flag.Version {
		fmt.Println(version.String())
		return
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Failed to read input: %v", err)
	}

	var req pluginpb.CodeGeneratorRequest
	err = proto.Unmarshal(input, &req)
	if err != nil {
		log.Fatalf("Failed to unmarshal request: %v", err)
	}

	resp := generate(&req)
	output, err := proto.Marshal(resp)
	if err != nil {
		log.Fatalf("Failed to marshal response: %v", err)
	}

	_, err = os.Stdout.Write(output)
	if err != nil {
		log.Fatalf("Failed to write output: %v", err)
	}
}

func generate(req *pluginpb.CodeGeneratorRequest) *pluginpb.CodeGeneratorResponse {
	opts, err := parseOptions(req.GetParameter())
	if err != nil {
		return &pluginpb.CodeGeneratorResponse{
			Error: proto.String(fmt.Sprintf("Failed to parse options: %v", err)),
		}
	}

	gen, err := protogen.Options{}.New(req)
	if err != nil {
		return &pluginpb.CodeGeneratorResponse{
			Error: proto.String(fmt.Sprintf("Failed to create protogen: %v", err)),
		}
	}

	var files []*pluginpb.CodeGeneratorResponse_File
	for _, file := range gen.Files {
		if !file.Generate {
			continue
		}

		generated, err := generateFile(gen, file, opts)
		if err != nil {
			return &pluginpb.CodeGeneratorResponse{
				Error: proto.String(fmt.Sprintf("Failed to generate file %s: %v", file.Desc.Path(), err)),
			}
		}

		files = append(files, generated...)
	}

	if opts.schemaTS != "" {
		files = append(files, &pluginpb.CodeGeneratorResponse_File{
			Name:    proto.String(opts.schemaTS),
			Content: proto.String(generateSchemaTS(gen.Files)),
		})
	}

	return &pluginpb.CodeGeneratorResponse{
		File:              files,
		SupportedFeatures: proto.Uint64(uint64(pluginpb.CodeGeneratorResponse_FEATURE_PROTO3_OPTIONAL)),
	}
}

func parseOptions(parameter string) (*options, error) {
	opts := &options{}

	if parameter == "" {
		return opts, nil
	}

	for _, param := range strings.Split(parameter, ",") {
		param = strings.TrimSpace(param)
		if param == "" {
			continue
		}

		switch {
		case param == "paths=source_relative":
			opts.relative = true
		case strings.HasPrefix(param, "module="):
			opts.module = strings.TrimPrefix(param, "module=")
		case strings.HasPrefix(param, "openapi_dir="):
			opts.openAPIDir = strings.TrimPrefix(param, "openapi_dir=")
		case strings.HasPrefix(param, "openapi_version="):
			opts.openAPIVersion = strings.TrimPrefix(param, "openapi_version=")
		case strings.HasPrefix(param, "schema_ts="):
			opts.schemaTS = strings.TrimPrefix(param, "schema_ts=")
		default:
			return nil, fmt.Errorf("unknown parameter: %s", param)
		}
	}

	return opts, nil
}

func generateFile(plugin *protogen.Plugin, file *protogen.File, opts *options) ([]*pluginpb.CodeGeneratorResponse_File, error) {
	if len(file.Services) == 0 {
		return nil, nil
	}

	base := strings.TrimSuffix(file.Desc.Path(), ".proto")
	suffix := "_xrpc.pb.go"
	finalFilename := base + suffix

	goPackageOpt := ""
	if file.Desc.Options() != nil {
		if fileOpts := file.Desc.Options().(*descriptorpb.FileOptions); fileOpts != nil {
			goPackageOpt = fileOpts.GetGoPackage()
		}
	}

	packageName := string(file.GoPackageName)
	importPath := string(file.GoImportPath)

	if goPackageOpt != "" {
		importPath = goPackageOpt
		packageName = filepath.Base(goPackageOpt)

		if strings.Contains(goPackageOpt, ";") {
			parts := strings.Split(goPackageOpt, ";")
			importPath = parts[0]
			packageName = parts[1]
		}
	}

	if !opts.relative {
		outputPath := importPath

		if opts.module != "" && strings.HasPrefix(outputPath, opts.module) && outputPath != opts.module {
			outputPath = strings.TrimPrefix(outputPath, opts.module+"/")
		}
		if opts.module != "" && outputPath == opts.module {
			outputPath = ""
		}

		if outputPath != "" {
			importDir := strings.ReplaceAll(outputPath, ".", "/")
			finalFilename = filepath.Join(importDir, filepath.Base(base)+suffix)
		}
	}

	generator := &generator{
		file:        file,
		genFile:     plugin.NewGeneratedFile(finalFilename, file.GoImportPath),
		opts:        opts,
		packageName: packageName,
		importPath:  importPath,
	}

	goFile, err := generator.generate(finalFilename)
	if err != nil {
		return nil, err
	}

	openAPIFile, err := generateOpenAPIFile(file, opts)
	if err != nil {
		return nil, err
	}

	return []*pluginpb.CodeGeneratorResponse_File{goFile, openAPIFile}, nil
}

func generateOpenAPIFile(file *protogen.File, opts *options) (*pluginpb.CodeGeneratorResponse_File, error) {
	paths := map[string]any{}
	schemas := map[string]any{}
	tags := []any{}
	for _, service := range file.Services {
		tag := map[string]any{"name": string(service.Desc.Name())}
		if description := openAPIComment(service.Comments); description != "" {
			tag["description"] = description
		}
		tags = append(tags, tag)
		for _, method := range service.Methods {
			addOpenAPISchema(schemas, method.Input)
			addOpenAPISchema(schemas, method.Output)
			path := "/" + string(service.Desc.Name()) + "/" + method.GoName
			operation := map[string]any{
				"operationId": string(service.Desc.FullName()) + "." + string(method.Desc.Name()),
				"tags":        []string{string(service.Desc.Name())},
			}
			if description := openAPIComment(method.Comments); description != "" {
				operation["summary"] = openAPISummary(description)
				operation["description"] = description
			}

			if method.Desc.IsStreamingClient() || method.Desc.IsStreamingServer() {
				paths[path] = map[string]any{"get": openAPIXRPCStreamOperation(operation, service, method)}
				continue
			}

			paths[path] = map[string]any{"post": openAPIXRPCUnaryOperation(operation, service, method)}
		}
	}

	schemas[xrpcErrorSchema] = openAPIXRPCErrorSchema()

	document := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   string(file.Desc.Package()),
			"version": openAPIVersion(opts),
		},
		"tags":  tags,
		"paths": paths,
		"components": map[string]any{
			"schemas": schemas,
		},
	}

	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal OpenAPI document for %s: %w", file.Desc.Path(), err)
	}

	filename := strings.TrimSuffix(file.Desc.Path(), ".proto") + ".openapi.json"
	if opts.openAPIDir != "" {
		filename = filepath.Join(opts.openAPIDir, filename)
	}

	return &pluginpb.CodeGeneratorResponse_File{
		Name:    proto.String(filename),
		Content: proto.String(string(content)),
	}, nil
}

// openAPIComment returns the leading comment of a proto element as plain text,
// or "" when it has none.
func openAPIComment(comments protogen.CommentSet) string {
	lines := strings.Split(strings.TrimSpace(string(comments.Leading)), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// openAPISummary returns the first sentence (or paragraph) of a description.
func openAPISummary(description string) string {
	summary, _, _ := strings.Cut(description, "\n\n")
	summary = strings.ReplaceAll(summary, "\n", " ")
	if i := strings.Index(summary, ". "); i >= 0 {
		summary = summary[:i+1]
	}
	return summary
}

func openAPIVersion(opts *options) string {
	if opts.openAPIVersion != "" {
		return opts.openAPIVersion
	}
	return "0.0.0"
}

func openAPISchemaRef(message *protogen.Message) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + string(message.Desc.FullName())}
}

func addOpenAPISchema(schemas map[string]any, message *protogen.Message) {
	name := string(message.Desc.FullName())
	if _, exists := schemas[name]; exists {
		return
	}

	if schema, ok := openAPIWellKnownSchema(name); ok {
		schemas[name] = schema
		return
	}

	// Insert before visiting fields so recursive message types terminate.
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	if description := openAPIComment(message.Comments); description != "" {
		schema["description"] = description
	}
	schemas[name] = schema
	properties := schema["properties"].(map[string]any)
	oneofFields := map[protoreflect.Name][]string{}

	for _, field := range message.Fields {
		fieldSchema := openAPIFieldSchema(schemas, field)
		if description := openAPIComment(field.Comments); description != "" {
			// Keep the enum legend built by openAPISingularFieldSchema.
			if existing, ok := fieldSchema["description"].(string); ok && existing != "" {
				description += "\n\n" + existing
			}
			fieldSchema["description"] = description
		}
		properties[field.Desc.JSONName()] = fieldSchema
		if oneof := field.Desc.ContainingOneof(); oneof != nil && !oneof.IsSynthetic() {
			oneofFields[oneof.Name()] = append(oneofFields[oneof.Name()], field.Desc.JSONName())
		}
	}

	if len(oneofFields) > 0 {
		allOf := make([]any, 0, len(oneofFields))
		oneofNames := make([]string, 0, len(oneofFields))
		for name := range oneofFields {
			oneofNames = append(oneofNames, string(name))
		}
		sort.Strings(oneofNames)

		for _, name := range oneofNames {
			fields := oneofFields[protoreflect.Name(name)]
			choices := make([]any, 0, len(fields)+1)
			requiredChoices := make([]any, 0, len(fields))
			for _, field := range fields {
				requiredChoices = append(requiredChoices, map[string]any{"required": []string{field}})
			}
			choices = append(choices, map[string]any{"not": map[string]any{"anyOf": requiredChoices}})
			for _, field := range fields {
				others := make([]any, 0, len(fields)-1)
				for _, other := range fields {
					if other != field {
						others = append(others, map[string]any{"required": []string{other}})
					}
				}
				choice := map[string]any{"required": []string{field}}
				if len(others) > 0 {
					choice["not"] = map[string]any{"anyOf": others}
				}
				choices = append(choices, choice)
			}
			allOf = append(allOf, map[string]any{"oneOf": choices})
		}
		schema["allOf"] = allOf
	}
}

func openAPIFieldSchema(schemas map[string]any, field *protogen.Field) map[string]any {
	if field.Desc.IsMap() {
		value := field.Message.Fields[1]
		return map[string]any{
			"type":                 "object",
			"additionalProperties": openAPIFieldSchema(schemas, value),
		}
	}

	schema := openAPISingularFieldSchema(schemas, field)
	if field.Desc.Cardinality() == protoreflect.Repeated {
		return map[string]any{"type": "array", "items": schema}
	}
	return schema
}

func openAPISingularFieldSchema(schemas map[string]any, field *protogen.Field) map[string]any {
	switch field.Desc.Kind() {
	case protoreflect.BoolKind:
		return map[string]any{"type": "boolean"}
	case protoreflect.StringKind:
		return map[string]any{"type": "string"}
	case protoreflect.BytesKind:
		return map[string]any{"type": "string", "contentEncoding": "base64"}
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		return map[string]any{"type": "integer", "format": "int32"}
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		return map[string]any{"type": "integer", "format": "int64"}
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return map[string]any{"type": "integer", "format": "uint32", "minimum": 0}
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return map[string]any{"type": "integer", "format": "uint64", "minimum": 0}
	case protoreflect.FloatKind:
		return map[string]any{"type": "number", "format": "float"}
	case protoreflect.DoubleKind:
		return map[string]any{"type": "number", "format": "double"}
	case protoreflect.EnumKind:
		values := make([]int32, 0, len(field.Enum.Values))
		names := make([]string, 0, len(field.Enum.Values))
		var legend strings.Builder
		if description := openAPIComment(field.Enum.Comments); description != "" {
			legend.WriteString(description)
			legend.WriteString("\n\n")
		}
		for _, value := range field.Enum.Values {
			values = append(values, int32(value.Desc.Number()))
			names = append(names, string(value.Desc.Name()))
			fmt.Fprintf(&legend, "- `%d` `%s`", value.Desc.Number(), value.Desc.Name())
			if description := openAPIComment(value.Comments); description != "" {
				legend.WriteString(": " + strings.ReplaceAll(description, "\n", " "))
			}
			legend.WriteString("\n")
		}
		schema := map[string]any{"type": "integer", "enum": values, "x-enum-varnames": names}
		if description := strings.TrimSpace(legend.String()); description != "" {
			schema["description"] = description
		}
		return schema
	case protoreflect.MessageKind, protoreflect.GroupKind:
		addOpenAPISchema(schemas, field.Message)
		return openAPISchemaRef(field.Message)
	default:
		return map[string]any{}
	}
}

func openAPIWellKnownSchema(name string) (map[string]any, bool) {
	switch name {
	case "google.protobuf.Timestamp":
		return map[string]any{"type": "string", "format": "date-time"}, true
	case "google.protobuf.Duration":
		return map[string]any{"type": "string", "format": "duration"}, true
	case "google.protobuf.Empty":
		return map[string]any{"type": "object"}, true
	case "google.protobuf.Any":
		return map[string]any{"type": "object"}, true
	default:
		return nil, false
	}
}
func (g *generator) generate(filename string) (*pluginpb.CodeGeneratorResponse_File, error) {
	g.genFile.P("// Code generated by protoc-gen-go-xrpc. DO NOT EDIT.")
	g.genFile.P("// versions:")
	g.genFile.P("// - protoc-gen-go-xrpc ", version.String())
	g.genFile.P("// source: ", g.file.Desc.Path())
	g.genFile.P()
	g.genFile.P("package ", g.packageName)
	g.genFile.P()

	for _, service := range g.file.Services {
		g.service(service)
	}

	content, err := g.genFile.Content()
	if err != nil {
		return nil, fmt.Errorf("failed to render generated code for %s: %w", filename, err)
	}

	return &pluginpb.CodeGeneratorResponse_File{
		Name:    proto.String(filename),
		Content: proto.String(string(content)),
	}, nil
}

func (g *generator) qualifiedGoIdent(ident protogen.GoIdent) string {
	return g.genFile.QualifiedGoIdent(ident)
}

func (g *generator) goType(message *protogen.Message) string {
	return g.qualifiedGoIdent(message.GoIdent)
}

func (g *generator) goTypeRef(message *protogen.Message) string {
	return "*" + g.goType(message)
}

func (g *generator) service(service *protogen.Service) {
	serviceName := string(service.Desc.Name())

	// Generate the service interface
	g.generateInterface(serviceName, service)

	// Generate the unimplemented server struct
	g.genFile.P("type Unimplemented", serviceName, "Server struct{}")
	g.genFile.P()

	// Generate Descriptor method
	g.genFile.P("func (Unimplemented", serviceName, "Server) Descriptor() ", g.qualifiedGoIdent(protoreflectFileDescriptorIdent), " {")
	g.genFile.P("return ", g.file.GoDescriptorIdent.GoName)
	g.genFile.P("}")
	g.genFile.P()

	// Generate methods for each RPC
	for _, method := range service.Methods {
		g.method(serviceName, method)
	}

	// Generate registration function
	g.generateRegistrationFunction(serviceName)

	// Generate client interface and implementation
	g.generateClientInterface(serviceName, service)
	g.generateClientStruct(serviceName, service)
}

func (g *generator) method(serviceName string, method *protogen.Method) {
	methodName := method.GoName
	inputType := g.goTypeRef(method.Input)
	outputType := g.goTypeRef(method.Output)
	contextType := g.qualifiedGoIdent(contextContextIdent)

	isClientStreaming := method.Desc.IsStreamingClient()
	isServerStreaming := method.Desc.IsStreamingServer()

	// Determine method signature based on streaming
	var signature, body string

	switch {
	case !isClientStreaming && !isServerStreaming:
		// Unary
		signature = fmt.Sprintf("func (Unimplemented%sServer) %s(ctx %s, in %s) (%s, error)",
			serviceName, methodName, contextType, inputType, outputType)
		body = g.errorReturn(serviceName, methodName, true)
	case isClientStreaming && !isServerStreaming:
		// Client streaming
		signature = fmt.Sprintf("func (Unimplemented%sServer) %s(ctx %s, in <-chan %s) (%s, error)",
			serviceName, methodName, contextType, inputType, outputType)
		body = g.errorReturn(serviceName, methodName, true)
	case !isClientStreaming && isServerStreaming:
		// Server streaming
		signature = fmt.Sprintf("func (Unimplemented%sServer) %s(ctx %s, in %s, out chan<- %s) error",
			serviceName, methodName, contextType, inputType, outputType)
		body = g.errorReturn(serviceName, methodName, false)
	case isClientStreaming && isServerStreaming:
		// Bidirectional streaming
		signature = fmt.Sprintf("func (Unimplemented%sServer) %s(ctx %s, in <-chan %s, out chan<- %s) error",
			serviceName, methodName, contextType, inputType, outputType)
		body = g.errorReturn(serviceName, methodName, false)
	}

	g.genFile.P(signature, " {")
	g.genFile.P(body)
	g.genFile.P("}")
	g.genFile.P()
}

func (g *generator) errorReturn(serviceName, methodName string, needsNilReturn bool) string {
	errorMsg := fmt.Sprintf("method %s.%s not implemented", serviceName, methodName)
	errorCall := fmt.Sprintf("%s(\"%s\")", g.qualifiedGoIdent(errorsNewIdent), errorMsg)

	if needsNilReturn {
		return fmt.Sprintf("return nil, %s", errorCall)
	} else {
		return fmt.Sprintf("return %s", errorCall)
	}
}

func (g *generator) generateInterface(serviceName string, service *protogen.Service) {
	g.genFile.P("// ", serviceName, "Server is the server API for ", serviceName, " service.")
	g.genFile.P("type ", serviceName, "Server interface {")

	// Add Descriptor method to interface
	g.genFile.P("Descriptor() ", g.qualifiedGoIdent(protoreflectFileDescriptorIdent))

	// Generate interface methods
	for _, method := range service.Methods {
		g.generateInterfaceMethod(method)
	}

	g.genFile.P("}")
	g.genFile.P()
}

func (g *generator) generateInterfaceMethod(method *protogen.Method) {
	methodName := method.GoName
	inputType := g.goTypeRef(method.Input)
	outputType := g.goTypeRef(method.Output)
	contextType := g.qualifiedGoIdent(contextContextIdent)

	isClientStreaming := method.Desc.IsStreamingClient()
	isServerStreaming := method.Desc.IsStreamingServer()

	// Generate method signature based on streaming type
	var signature string

	switch {
	case !isClientStreaming && !isServerStreaming:
		// Unary
		signature = fmt.Sprintf("%s(ctx %s, in %s) (%s, error)",
			methodName, contextType, inputType, outputType)
	case isClientStreaming && !isServerStreaming:
		// Client streaming
		signature = fmt.Sprintf("%s(ctx %s, in <-chan %s) (%s, error)",
			methodName, contextType, inputType, outputType)
	case !isClientStreaming && isServerStreaming:
		// Server streaming
		signature = fmt.Sprintf("%s(ctx %s, in %s, out chan<- %s) error",
			methodName, contextType, inputType, outputType)
	case isClientStreaming && isServerStreaming:
		// Bidirectional streaming
		signature = fmt.Sprintf("%s(ctx %s, in <-chan %s, out chan<- %s) error",
			methodName, contextType, inputType, outputType)
	}

	g.genFile.P(signature)
}

func (g *generator) generateClientInterface(serviceName string, service *protogen.Service) {
	g.genFile.P("// ", serviceName, "ClientDefinition is the client API for ", serviceName, " service.")
	g.genFile.P("type ", serviceName, "ClientDefinition interface {")

	// Generate interface methods for all streaming types
	for _, method := range service.Methods {
		g.generateClientInterfaceMethod(method)
	}

	g.genFile.P("}")
	g.genFile.P()
}

func (g *generator) generateClientInterfaceMethod(method *protogen.Method) {
	methodName := method.GoName
	inputType := g.goTypeRef(method.Input)
	outputType := g.goTypeRef(method.Output)
	contextType := g.qualifiedGoIdent(contextContextIdent)

	isClientStreaming := method.Desc.IsStreamingClient()
	isServerStreaming := method.Desc.IsStreamingServer()

	// Generate method signature based on streaming type
	var signature string

	switch {
	case !isClientStreaming && !isServerStreaming:
		// Unary
		signature = fmt.Sprintf("%s(ctx %s, in %s) (%s, error)",
			methodName, contextType, inputType, outputType)
	case isClientStreaming && !isServerStreaming:
		// Client streaming
		signature = fmt.Sprintf("%s(ctx %s, in <-chan %s) (%s, error)",
			methodName, contextType, inputType, outputType)
	case !isClientStreaming && isServerStreaming:
		// Server streaming
		signature = fmt.Sprintf("%s(ctx %s, in %s, out chan<- %s) error",
			methodName, contextType, inputType, outputType)
	case isClientStreaming && isServerStreaming:
		// Bidirectional streaming
		signature = fmt.Sprintf("%s(ctx %s, in <-chan %s, out chan<- %s) error",
			methodName, contextType, inputType, outputType)
	}

	g.genFile.P(signature)
}

func (g *generator) generateRegistrationFunction(serviceName string) {
	g.genFile.P("// Register", serviceName, "Server registers a ", serviceName, "Server with the ", g.runtimeIdent("Service"), " registry.")
	g.genFile.P("// It returns a service that can be mounted on a web server.")
	g.genFile.P("func Register", serviceName, "Server(server ", serviceName, "Server) *", g.runtimeIdent("Service"), " {")
	g.genFile.P("return ", g.runtimeIdent("Register"), "(server)")
	g.genFile.P("}")
	g.genFile.P()

	// Add compile-time interface check
	g.genFile.P("// Ensure Unimplemented", serviceName, "Server implements ", serviceName, "Server")
	g.genFile.P("var _ ", serviceName, "Server = (*Unimplemented", serviceName, "Server)(nil)")
	g.genFile.P()
}
