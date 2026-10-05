package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
)

func registerAllocationTools(server *mcp.Server, nomadClient client.Facade) {
	addTool(server, &mcp.Tool{
		Name:        "list_allocations",
		Description: "List allocations with optional namespace, prefix, filter, and pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listQueryInput) (*mcp.CallToolResult, map[string]any, error) {
		allocations, queryMeta, err := nomadClient.ListAllocations(input.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		items := make([]map[string]any, 0, len(allocations))
		for _, allocation := range allocations {
			if allocation == nil {
				continue
			}
			items = append(items, map[string]any{
				"id":             allocation.ID,
				"namespace":      allocation.Namespace,
				"eval_id":        allocation.EvalID,
				"node_id":        allocation.NodeID,
				"job_id":         allocation.JobID,
				"task_group":     allocation.TaskGroup,
				"desired_status": allocation.DesiredStatus,
				"client_status":  allocation.ClientStatus,
			})
		}

		output := map[string]any{
			"allocations": items,
			"meta":        metaMap(queryMeta),
		}
		return structuredResult(summarizeList("allocations", len(items), queryMeta), output), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_allocation",
		Description: "Get a Nomad allocation and its service registrations.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		AllocationID string `json:"allocation_id" jsonschema:"full Nomad allocation ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		if input.AllocationID == "" {
			return failResult(errors.New("allocation_id is required")), nil, nil
		}

		base := input.objectQueryInput.queryOptions()
		allocation, queryMeta, err := nomadClient.GetAllocation(input.AllocationID, base.WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		services, servicesMeta, err := nomadClient.ListAllocationServices(input.AllocationID, queryForAllocation(ctx, base, allocation))
		if err != nil {
			return failResult(err), nil, nil
		}

		serviceItems := make([]map[string]any, 0, len(services))
		for _, service := range services {
			if service == nil {
				continue
			}
			serviceItems = append(serviceItems, map[string]any{
				"service_name": service.ServiceName,
				"namespace":    service.Namespace,
				"job_id":       service.JobID,
				"alloc_id":     service.AllocID,
			})
		}

		output := map[string]any{
			"allocation": map[string]any{
				"id":               allocation.ID,
				"namespace":        allocation.Namespace,
				"eval_id":          allocation.EvalID,
				"node_id":          allocation.NodeID,
				"job_id":           allocation.JobID,
				"task_group":       allocation.TaskGroup,
				"desired_status":   allocation.DesiredStatus,
				"desired_desc":     allocation.DesiredDescription,
				"client_status":    allocation.ClientStatus,
				"client_desc":      allocation.ClientDescription,
				"task_state_names": sortedKeys(allocation.TaskStates),
			},
			"services": map[string]any{
				"items": serviceItems,
				"meta":  metaMap(servicesMeta),
			},
			"meta": metaMap(queryMeta),
		}

		return structuredResult(fmt.Sprintf("Allocation %s is %s on node %s.", allocation.ID, allocation.ClientStatus, allocation.NodeID), output), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_allocation_checks",
		Description: "Get Nomad service health checks for a specific allocation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		AllocationID string `json:"allocation_id" jsonschema:"full Nomad allocation ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		if input.AllocationID == "" {
			return failResult(errors.New("allocation_id is required")), nil, nil
		}

		output, passing, err := allocationChecksPayload(ctx, nomadClient, input.AllocationID, input.objectQueryInput.queryOptions())
		if err != nil {
			return failResult(err), nil, nil
		}

		checks, _ := output["checks"].([]map[string]any)
		summary := fmt.Sprintf("Allocation %s has %d checks, %d passing.", input.AllocationID, len(checks), passing)
		return structuredResult(summary, output), output, nil
	})
}
