package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
)

func Register(server *mcp.Server, nomadClient client.Facade) {
	registerClusterTools(server, nomadClient)
	registerJobTools(server, nomadClient)
	registerAllocationTools(server, nomadClient)
}
