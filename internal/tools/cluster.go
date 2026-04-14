package tools

import (
	"context"
	"fmt"

	"github.com/hashicorp/nomad/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
)

type clusterStatusInput struct {
	Region string `json:"region,omitempty" jsonschema:"optional region override for leader discovery"`
}

func registerClusterTools(server *mcp.Server, nomadClient client.Facade) {
	addTool(server, &mcp.Tool{
		Name:        "get_cluster_status",
		Description: "Return Nomad cluster leader, peers, and regions for the current connection.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input clusterStatusInput) (*mcp.CallToolResult, map[string]any, error) {
		leader, err := nomadClient.Leader(input.Region)
		if err != nil {
			return failResult(err), nil, nil
		}

		peers, err := nomadClient.Peers()
		if err != nil {
			return failResult(err), nil, nil
		}

		regions, err := nomadClient.Regions()
		if err != nil {
			return failResult(err), nil, nil
		}

		output := map[string]any{
			"address": nomadClient.Address(),
			"leader":  leader,
			"peers":   peers,
			"regions": regions,
		}

		return okResult(fmt.Sprintf("Leader %s with %d peers across %d known regions.", leader, len(peers), len(regions))), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "list_regions",
		Description: "List regions known to the connected Nomad cluster.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]any, error) {
		regions, err := nomadClient.Regions()
		if err != nil {
			return failResult(err), nil, nil
		}

		output := map[string]any{"regions": regions}
		return okResult(fmt.Sprintf("Returned %d regions.", len(regions))), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "list_namespaces",
		Description: "List Nomad namespaces with optional prefix, pagination, and filtering.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listQueryInput) (*mcp.CallToolResult, map[string]any, error) {
		namespaces, queryMeta, err := nomadClient.ListNamespaces(input.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		items := make([]map[string]any, 0, len(namespaces))
		for _, namespace := range namespaces {
			if namespace == nil {
				continue
			}
			items = append(items, map[string]any{
				"name":        namespace.Name,
				"description": namespace.Description,
				"quota":       namespace.Quota,
			})
		}

		output := map[string]any{
			"namespaces": items,
			"meta":       metaMap(queryMeta),
		}
		return okResult(summarizeList("namespaces", len(items), queryMeta)), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "list_nodes",
		Description: "List Nomad client nodes with optional prefix, pagination, and filtering.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listQueryInput) (*mcp.CallToolResult, map[string]any, error) {
		query := input.queryOptions().WithContext(ctx)
		if query.Params == nil {
			query.Params = map[string]string{}
		}

		nodes, queryMeta, err := nomadClient.ListNodes(query)
		if err != nil {
			return failResult(err), nil, nil
		}

		items := make([]map[string]any, 0, len(nodes))
		for _, node := range nodes {
			if node == nil {
				continue
			}
			items = append(items, map[string]any{
				"id":                     node.ID,
				"name":                   node.Name,
				"datacenter":             node.Datacenter,
				"node_class":             node.NodeClass,
				"node_pool":              node.NodePool,
				"status":                 node.Status,
				"scheduling_eligibility": node.SchedulingEligibility,
				"drain":                  node.Drain,
			})
		}

		output := map[string]any{
			"nodes": items,
			"meta":  metaMap(queryMeta),
		}
		return okResult(summarizeList("nodes", len(items), queryMeta)), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_node",
		Description: "Get detailed information for a Nomad node by ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		NodeID string `json:"node_id" jsonschema:"full Nomad node ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		node, queryMeta, err := nomadClient.GetNode(input.NodeID, input.objectQueryInput.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		allocations, allocationsMeta, err := nomadClient.ListNodeAllocations(input.NodeID, input.objectQueryInput.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		allocationIDs := make([]string, 0, len(allocations))
		for _, allocation := range allocations {
			if allocation == nil {
				continue
			}
			allocationIDs = append(allocationIDs, allocation.ID)
		}

		output := map[string]any{
			"node": map[string]any{
				"id":                     node.ID,
				"name":                   node.Name,
				"http_addr":              node.HTTPAddr,
				"datacenter":             node.Datacenter,
				"node_class":             node.NodeClass,
				"node_pool":              node.NodePool,
				"status":                 node.Status,
				"scheduling_eligibility": node.SchedulingEligibility,
				"drain":                  node.Drain,
				"drivers":                sortedKeys(node.Drivers),
				"attribute_count":        len(node.Attributes),
				"meta_count":             len(node.Meta),
			},
			"allocations": map[string]any{
				"count": allocationIDs,
				"ids":   allocationIDs,
				"meta":  metaMap(allocationsMeta),
			},
			"meta": metaMap(queryMeta),
		}

		return okResult(fmt.Sprintf("Node %s is %s with %d allocations.", node.ID, node.Status, len(allocationIDs))), output, nil
	})
}

var _ = api.AllNamespacesNamespace
