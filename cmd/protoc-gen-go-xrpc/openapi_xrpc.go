package main

import (
	"fmt"

	"google.golang.org/protobuf/compiler/protogen"
)

const xrpcErrorSchema = "xrpc.Error"

func xrpcMethodName(service *protogen.Service, method *protogen.Method) string {
	return string(service.Desc.Name()) + "." + string(method.Desc.Name())
}

func openAPIXRPCErrorSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "JSON-RPC 2.0 error object.",
		"required":    []string{"code", "message"},
		"properties": map[string]any{
			"code":    map[string]any{"type": "integer", "description": "-32700 parse error, -32600 invalid request, -32601 method not found, -32602 invalid params, -32603 internal error, -32000 server error."},
			"message": map[string]any{"type": "string"},
			"data":    map[string]any{"description": "Optional additional information."},
		},
	}
}

func openAPIXRPCIDSchema() map[string]any {
	return map[string]any{
		"type":        []string{"string", "integer"},
		"description": "Correlates the response with the request. A request without id is a notification and gets no response.",
	}
}

func openAPIXRPCRequestSchema(name string, params map[string]any) map[string]any {
	properties := map[string]any{
		"jsonrpc": map[string]any{"const": "2.0"},
		"method":  map[string]any{"const": name},
		"id":      openAPIXRPCIDSchema(),
	}
	if params != nil {
		properties["params"] = params
	}
	return map[string]any{
		"type":       "object",
		"required":   []string{"jsonrpc", "method"},
		"properties": properties,
	}
}

// openAPIXRPCUnaryOperation documents a unary method as a JSON-RPC 2.0 call.
func openAPIXRPCUnaryOperation(operation map[string]any, service *protogen.Service, method *protogen.Method) map[string]any {
	name := xrpcMethodName(service, method)
	note := "JSON-RPC 2.0 call of `" + name + "`. The URL path only identifies the operation; the method is taken from the `method` member of the request body."
	if description, ok := operation["description"].(string); ok {
		note = description + "\n\n" + note
	}
	operation["description"] = note

	operation["requestBody"] = map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{
				"schema":  openAPIXRPCRequestSchema(name, openAPISchemaRef(method.Input)),
				"example": map[string]any{"jsonrpc": "2.0", "method": name, "params": map[string]any{}, "id": 1},
			},
		},
	}
	operation["responses"] = map[string]any{
		"200": map[string]any{
			"description": "JSON-RPC 2.0 response with either `result` or `error`.",
			"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{
					"type":     "object",
					"required": []string{"jsonrpc", "id"},
					"properties": map[string]any{
						"jsonrpc": map[string]any{"const": "2.0"},
						"id":      openAPIXRPCIDSchema(),
						"result":  openAPISchemaRef(method.Output),
						"error":   map[string]any{"$ref": "#/components/schemas/" + xrpcErrorSchema},
					},
				}},
			},
		},
		"default": map[string]any{"description": "Error"},
	}
	return operation
}

// openAPIXRPCStreamOperation documents a streaming method as a JSON-RPC subscription over WebSocket.
func openAPIXRPCStreamOperation(operation map[string]any, service *protogen.Service, method *protogen.Method) map[string]any {
	name := xrpcMethodName(service, method)
	kind, flow := "server", "The client sends the JSON-RPC request `%s` with the single `%s` message as `params`; the server then sends any number of `xrpc.message` notifications carrying `%s` messages in `params.data`, and ends the stream with the response to the request (`result` is null)."
	args := []any{name, method.Input.Desc.Name(), method.Output.Desc.Name()}
	switch {
	case method.Desc.IsStreamingClient() && method.Desc.IsStreamingServer():
		kind, flow = "bidi", "The client sends the JSON-RPC request `%s` without params, then `xrpc.message` notifications carrying `%s` messages in `params.data` and finally an `xrpc.close` notification. The server sends `xrpc.message` notifications carrying `%s` messages and ends the stream with the response to the request (`result` is null)."
	case method.Desc.IsStreamingClient():
		kind, flow = "client", "The client sends the JSON-RPC request `%s` without params, then `xrpc.message` notifications carrying `%s` messages in `params.data` and finally an `xrpc.close` notification. The server answers with the response to the request, whose `result` is the `%s` message."
	}

	path := "/" + string(service.Desc.Name()) + "/" + method.GoName
	text := "**WebSocket endpoint.** Connect with a WebSocket upgrade request (`ws://` or `wss://`, sub-protocol `xrpc.json`). " +
		fmt.Sprintf(flow, args...) +
		" Notifications are JSON-RPC objects with `params` of the form `{\"id\": <request id>, \"data\": <message>}`; a client aborts a stream with an `xrpc.cancel` notification."
	if description, ok := operation["description"].(string); ok {
		text = description + "\n\n" + text
	}
	operation["description"] = text
	operation["x-stream"] = kind

	clientCount, serverCount := "one", "many"
	switch kind {
	case "bidi":
		clientCount = "many"
	case "client":
		clientCount, serverCount = "many", "one"
	}
	operation["x-stream-messages"] = map[string]any{
		"encoding": "application/json",
		"client": map[string]any{
			"description": "Message sent by the client, wrapped as described in the operation description.",
			"count":       clientCount,
			"schema":      openAPISchemaRef(method.Input),
		},
		"server": map[string]any{
			"description": "Message sent by the server, wrapped as described in the operation description.",
			"count":       serverCount,
			"schema":      openAPISchemaRef(method.Output),
		},
	}
	operation["responses"] = map[string]any{
		"101": map[string]any{
			"description": "Switching Protocols. The connection is upgraded to a WebSocket.",
		},
		"default": map[string]any{"description": "Error"},
	}
	operation["x-codeSamples"] = []any{map[string]any{
		"lang":  "JavaScript",
		"label": "WebSocket",
		"source": "// baseURL is the API base address with a ws:// or wss:// scheme; add authentication as your deployment requires.\n" +
			"const ws = new WebSocket(baseURL.replace(/\\/$/, \"\") + \"" + path + "\", \"xrpc.json\");\n" +
			"ws.onopen = () => ws.send(JSON.stringify({ jsonrpc: \"2.0\", method: \"" + name + "\", params: { /* " + string(method.Input.Desc.Name()) + " */ }, id: 1 }));\n" +
			"ws.onmessage = (event) => {\n" +
			"  const frame = JSON.parse(event.data);\n" +
			"  if (frame.method === \"xrpc.message\") console.log(frame.params.data); // " + string(method.Output.Desc.Name()) + "\n" +
			"};\n",
	}}
	return operation
}
