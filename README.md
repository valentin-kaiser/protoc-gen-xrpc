# protoc-gen-go-xrpc

A protoc compiler plugin for Go that generates server stubs and client implementations for the xrpc transport.
## Overview

This plugin generates Go server stub interfaces and client implementations from protobuf service definitions that match specific method signatures expected by the go-core xrpc router.

> **Important**: This plugin only works in combination with the `protoc-gen-go` plugin and is meant to be used with the Go package `github.com/valentin-kaiser/go-core/web/xrpc`.

## Features

- ✅ **Server Stub Generation**: Generate server interfaces with proper method signatures
- ✅ **Client Implementation**: Generate typed HTTP/WebSocket clients for making RPC calls
- ✅ **Streaming Support**: Server streaming, client streaming, and bidirectional streaming
- ✅ **Protocol Buffer JSON**: Automatic marshaling/unmarshaling
- ✅ **Context Support**: Full context propagation for cancellation and timeouts
- ✅ **OpenAPI 3.1 Generation**: Generate a JSON OpenAPI document for every proto that defines services

## Installation

### Prerequisites

- [Protocol Buffers compiler (protoc)](https://protobuf.dev/installation/)
- [protoc-gen-go plugin](https://github.com/protocolbuffers/protobuf-go) 

### Install protoc-gen-go-xrpc

```bash
go install github.com/valentin-kaiser/protoc-gen-go-xrpc/cmd/protoc-gen-go-xrpc@latest
```

Make sure your `$GOPATH/bin` (or `$GOBIN`) is in your `$PATH` so protoc can find the plugin.

## Usage

### Basic Example

1. Define your service in a `.proto` file:

```protobuf
syntax = "proto3";

package api;
option go_package = "github.com/example/myapp/gen/go/api";

service UserService {
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
  rpc ListUsers(ListUsersRequest) returns (stream ListUsersResponse);
}

message GetUserRequest {
  string id = 1;
}

message GetUserResponse {
  string id = 1;
  string name = 2;
  string email = 3;
}

message ListUsersRequest {
  int32 limit = 1;
}

message ListUsersResponse {
  repeated User users = 1;
}

message User {
  string id = 1;
  string name = 2;
  string email = 3;
}
```

2. Generate Go code using both plugins:

```bash
protoc -I . \
  --go_out=./gen/go \
  --go_opt=module=github.com/example/myapp \
  --go-xrpc_out=./gen/go \
    --go-xrpc_opt=module=github.com/example/myapp,openapi_dir=./gen/openapi \
  api.proto
```

The plugin always generates one OpenAPI 3.1 JSON document per input proto with services. By default, the document is written as `{proto path without .proto}.openapi.json` alongside the proto path. Use `openapi_dir=<directory>` to place the document below a separate directory while retaining proto subdirectories. For example, `api/v1/users.proto` becomes `./gen/openapi/api/v1/users.openapi.json` with `openapi_dir=./gen/openapi`. Set `openapi_version=<version>` to control `info.version`; it defaults to `0.0.0`.

Unary operations are documented as `POST /{Service}/{Method}` with Protocol Buffer JSON request and response schemas. The route names match the generated client.

3. Implement the generated interface in your Go application:

```go
package main

import (
    "context"
    "log"
    
    "github.com/valentin-kaiser/go-core/web/xrpc"
    "github.com/example/myapp/gen/go/api"
)

// Embed the generated UnimplementedUserServiceServer
type userService struct {
    api.UnimplementedUserServiceServer
}

func (s *userService) GetUser(ctx context.Context, req *api.GetUserRequest) (*api.GetUserResponse, error) {
    return &api.GetUserResponse{
        Id:    req.Id,
        Name:  "John Doe",
        Email: "john@example.com",
    }, nil
}

func (s *userService) ListUsers(ctx context.Context, req *api.ListUsersRequest, out chan *api.ListUsersResponse) error {
    defer close(out)
    
    // Send users to the output channel
    out <- api.ListUsersResponse{
        Users: []*api.User{
            {Id: "1", Name: "Alice", Email: "alice@example.com"},
            {Id: "2", Name: "Bob", Email: "bob@example.com"},
        },
    }
    
    return nil
}

func main() {
    service := &userService{}
    
    // Create xRPC server with the service implementation
    server := xrpc.New(service)
    
    // Start your server (example using go-core/web)
    log.Println("Server starting on :8080...")
    // Use your preferred HTTP server setup here
}
```

### Plugin parameters

- `module=<path>` / `paths=source_relative` control the output location.
- `openapi_dir=<dir>` writes an OpenAPI 3.1 document per proto describing the JSON-RPC 2.0 envelopes and the WebSocket subscription protocol of streaming methods.
- `openapi_version=<version>` sets the version in the OpenAPI document.
- `schema_ts=<file>` writes a TypeScript schema of all messages and methods.

## Method Signatures

The plugin generates methods with the following signatures based on streaming types:

### Server Methods

- **Unary**: `func(ctx context.Context, in *In) (*Out, error)`
- **Client stream**: `func(ctx context.Context, in chan *In) (*Out, error)`
- **Server stream**: `func(ctx context.Context, in *In, out chan *Out) error`
- **Bidi stream**: `func(ctx context.Context, in chan *In, out chan *Out) error`

### Client Methods

- **Unary**: `func(ctx context.Context, in *In) (*Out, error)` - HTTP POST
- **Server streaming**: `func(ctx context.Context, in *In, out chan *Out) error` - WebSocket
- **Client streaming**: `func(ctx context.Context, in chan *In) (*Out, error)` - WebSocket
- **Bidirectional streaming**: `func(ctx context.Context, in chan *In, out chan *Out) error` - WebSocket

## Client Usage

The plugin automatically generates client interfaces and implementations for your services. Clients automatically use HTTP for unary calls and WebSocket for streaming calls.
The plugin automatically generates client interfaces and implementations for unary HTTP RPCs.

### Unary Call

```go
package main

import (
    "context"
    "log"
    
    "github.com/valentin-kaiser/go-core/web/xrpc"
    "github.com/example/myapp/gen/go/api"
)

func main() {
    // Create a client with custom timeout
    client, err := api.NewUserServiceClient(
        "http://localhost:8080",
        xrpc.WithUserAgent("myapp/1.0"),
    )
    if err != nil {
        log.Fatal(err)
    }
    
    // Make a unary call (uses HTTP)
    ctx := context.Background()
    resp, err := client.GetUser(ctx, &api.GetUserRequest{
        Id: "123",
    })
    if err != nil {
        log.Fatalf("Failed to get user: %v", err)
    }
    
    log.Printf("User: %s (%s)", resp.Name, resp.Email)
}
```

### Server Streaming

```go
func main() {
    client, err := api.NewUserServiceClient("http://localhost:8080")
    if err != nil {
        log.Fatal(err)
    }
    
    // Create output channel
    out := make(chan *api.ListUsersResponse, 10)
    
    // Consume stream
    go func() {
        for resp := range out {
            for _, user := range resp.Users {
                fmt.Printf("User: %s\n", user.Name)
            }
        }
    }()
    
    // Start streaming (uses WebSocket)
    ctx := context.Background()
    err := client.ListUsers(ctx, &api.ListUsersRequest{Limit: 10}, out)
    if err != nil {
        log.Fatalf("Stream failed: %v", err)
    }
}
```

## Generated Code

For each service defined in your `.proto` file, the plugin generates:

### Server Side
- `{Service}Server` interface with all RPC methods
- `Unimplemented{Service}Server` struct for forward compatibility
- `Register{Service}Server` function to register with the xRPC service

### Client Side
- `{Service}ClientDefinition` interface with all RPC methods
- `{Service}Client` struct implementing the client interface
- `New{Service}Client` constructor function

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

## License

This project is licensed under the BSD 3-Clause License - see the [LICENSE](LICENSE) file for details.
