package main

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
)

const xrpcImportPath = "github.com/valentin-kaiser/go-core/web/xrpc"

// runtimeIdent returns the qualified name of a symbol in the xrpc runtime package.
func (g *generator) runtimeIdent(name string) string {
	return g.qualifiedGoIdent(protogen.GoIdent{GoName: name, GoImportPath: xrpcImportPath})
}

func (g *generator) generateClientStruct(serviceName string, service *protogen.Service) {
	client := g.runtimeIdent("Client")
	option := g.runtimeIdent("ClientOption")
	newClient := g.runtimeIdent("NewClient")

	g.genFile.P("type ", serviceName, "Client struct {")
	g.genFile.P("client *", client)
	g.genFile.P("}")
	g.genFile.P()

	g.genFile.P("// New", serviceName, "Client creates a client for the ", serviceName, " service.")
	g.genFile.P("// The protocol (JSON, XML or gRPC) is selected with the client options.")
	g.genFile.P("func New", serviceName, "Client(endpoint string, opts ...", option, ") (*", serviceName, "Client, error) {")
	g.genFile.P("client, err := ", newClient, "(endpoint, opts...)")
	g.genFile.P("if err != nil {")
	g.genFile.P("return nil, err")
	g.genFile.P("}")
	g.genFile.P("return &", serviceName, "Client{client: client}, nil")
	g.genFile.P("}")
	g.genFile.P()

	g.genFile.P("// New", serviceName, "ClientFrom creates a ", serviceName, " client that shares an existing client.")
	g.genFile.P("func New", serviceName, "ClientFrom(client *", client, ") *", serviceName, "Client {")
	g.genFile.P("return &", serviceName, "Client{client: client}")
	g.genFile.P("}")
	g.genFile.P()

	for _, method := range service.Methods {
		g.generateXRPCClientMethod(serviceName, method)
	}

	g.genFile.P("// Ensure ", serviceName, "Client implements ", serviceName, "ClientDefinition")
	g.genFile.P("var _ ", serviceName, "ClientDefinition = (*", serviceName, "Client)(nil)")
	g.genFile.P()
}

func (g *generator) generateXRPCClientMethod(serviceName string, method *protogen.Method) {
	methodName := method.GoName
	inputType := g.goType(method.Input)
	outputType := g.goType(method.Output)
	contextType := g.qualifiedGoIdent(contextContextIdent)
	name := fmt.Sprintf("%q", string(method.Parent.Desc.FullName())+"/"+string(method.Desc.Name()))

	clientStream := g.runtimeIdent("ClientStream")
	serverStream := g.runtimeIdent("ServerStream")
	bidiStream := g.runtimeIdent("BidiStream")

	switch cs, ss := method.Desc.IsStreamingClient(), method.Desc.IsStreamingServer(); {
	case !cs && !ss:
		g.genFile.P(fmt.Sprintf("func (c *%sClient) %s(ctx %s, in *%s) (*%s, error) {", serviceName, methodName, contextType, inputType, outputType))
		g.genFile.P(fmt.Sprintf("out := &%s{}", outputType))
		g.genFile.P("err := c.client.Call(ctx, ", name, ", in, out)")
		g.genFile.P("if err != nil {")
		g.genFile.P("return nil, err")
		g.genFile.P("}")
		g.genFile.P("return out, nil")
		g.genFile.P("}")
	case cs && !ss:
		g.genFile.P(fmt.Sprintf("func (c *%sClient) %s(ctx %s, in <-chan *%s) (*%s, error) {", serviceName, methodName, contextType, inputType, outputType))
		g.genFile.P(fmt.Sprintf("out := &%s{}", outputType))
		g.genFile.P("err := ", clientStream, "(ctx, c.client, ", name, ", in, out)")
		g.genFile.P("if err != nil {")
		g.genFile.P("return nil, err")
		g.genFile.P("}")
		g.genFile.P("return out, nil")
		g.genFile.P("}")
	case !cs && ss:
		g.genFile.P(fmt.Sprintf("func (c *%sClient) %s(ctx %s, in *%s, out chan<- *%s) error {", serviceName, methodName, contextType, inputType, outputType))
		g.genFile.P(fmt.Sprintf("factory := func() *%s { return &%s{} }", outputType, outputType))
		g.genFile.P("return ", serverStream, "(ctx, c.client, ", name, ", in, out, factory)")
		g.genFile.P("}")
	default:
		g.genFile.P(fmt.Sprintf("func (c *%sClient) %s(ctx %s, in <-chan *%s, out chan<- *%s) error {", serviceName, methodName, contextType, inputType, outputType))
		g.genFile.P(fmt.Sprintf("factory := func() *%s { return &%s{} }", outputType, outputType))
		g.genFile.P("return ", bidiStream, "(ctx, c.client, ", name, ", in, out, factory)")
		g.genFile.P("}")
	}
	g.genFile.P()
}
