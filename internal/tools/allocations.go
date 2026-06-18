package tools

import (
	"context"
	"fmt"
	"sort"

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
		return okResult(summarizeList("allocations", len(items), queryMeta)), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_allocation",
		Description: "Get a Nomad allocation and its service registrations.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		AllocationID string `json:"allocation_id" jsonschema:"full Nomad allocation ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		query := input.objectQueryInput.queryOptions().WithContext(ctx)
		allocation, queryMeta, err := nomadClient.GetAllocation(input.AllocationID, query)
		if err != nil {
			return failResult(err), nil, nil
		}

		// The alloc lookup is namespace-agnostic (IDs are cluster-global), but the
		// services lookup enforces namespace. Use the allocation's real namespace so
		// a caller-supplied or wildcard namespace can't turn a found alloc into a
		// spurious 404.
		servicesQuery := input.objectQueryInput.queryOptions().WithContext(ctx)
		servicesQuery.Namespace = allocation.Namespace
		services, servicesMeta, err := nomadClient.ListAllocationServices(input.AllocationID, servicesQuery)
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

		return okResult(fmt.Sprintf("Allocation %s is %s on node %s.", allocation.ID, allocation.ClientStatus, allocation.NodeID)), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_allocation_checks",
		Description: "Get Nomad service health checks for a specific allocation.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		AllocationID string `json:"allocation_id" jsonschema:"full Nomad allocation ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		checks, err := nomadClient.GetAllocationChecks(input.AllocationID, input.objectQueryInput.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		checkIDs := make([]string, 0, len(checks))
		for id := range checks {
			checkIDs = append(checkIDs, id)
		}
		sort.Strings(checkIDs)

		items := make([]map[string]any, 0, len(checkIDs))
		passing := 0
		for _, id := range checkIDs {
			check := checks[id]
			if check.Status == "success" {
				passing++
			}

			items = append(items, map[string]any{
				"id":          check.ID,
				"check":       check.Check,
				"group":       check.Group,
				"task":        check.Task,
				"service":     check.Service,
				"mode":        check.Mode,
				"status":      check.Status,
				"status_code": check.StatusCode,
				"output":      check.Output,
				"timestamp":   check.Timestamp,
				"observed_at": formatUnixSeconds(check.Timestamp),
			})
		}

		output := map[string]any{
			"allocation_id": input.AllocationID,
			"passing":       passing,
			"failing":       len(items) - passing,
			"checks":        items,
		}

		return okResult(fmt.Sprintf("Allocation %s has %d checks, %d passing.", input.AllocationID, len(items), passing)), output, nil
	})
}
