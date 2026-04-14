package tools

import (
	"context"
	"fmt"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
)

func registerJobTools(server *mcp.Server, nomadClient client.Facade) {
	addTool(server, &mcp.Tool{
		Name:        "list_jobs",
		Description: "List Nomad jobs with optional namespace, prefix, filter, and pagination. The response includes job metadata to support discovery questions such as finding financial jobs.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listQueryInput) (*mcp.CallToolResult, map[string]any, error) {
		jobs, queryMeta, err := nomadClient.ListJobs(input.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		items := make([]map[string]any, 0, len(jobs))
		for _, job := range jobs {
			if job == nil {
				continue
			}
			meta := stringMap(job.Meta)
			items = append(items, map[string]any{
				"id":            job.ID,
				"name":          job.Name,
				"namespace":     job.Namespace,
				"type":          job.Type,
				"priority":      job.Priority,
				"status":        job.Status,
				"datacenters":   job.Datacenters,
				"submit_time":   formatSubmitTime(job.SubmitTime),
				"stop":          job.Stop,
				"periodic":      job.Periodic,
				"parameterized": job.ParameterizedJob,
				"meta":          meta,
				"meta_summary":  metadataSummary(meta),
			})
		}

		output := map[string]any{
			"jobs": items,
			"meta": metaMap(queryMeta),
		}
		return okResult(summarizeList("jobs", len(items), queryMeta)), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_job",
		Description: "Get a Nomad job and its scheduler summary by job ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		JobID string `json:"job_id" jsonschema:"full Nomad job ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		query := input.objectQueryInput.queryOptions().WithContext(ctx)
		job, queryMeta, err := nomadClient.GetJob(input.JobID, query)
		if err != nil {
			return failResult(err), nil, nil
		}

		summary, summaryMeta, err := nomadClient.GetJobSummary(input.JobID, query)
		if err != nil {
			return failResult(err), nil, nil
		}

		deployments, deploymentsMeta, err := nomadClient.ListJobDeployments(input.JobID, false, query)
		if err != nil {
			return failResult(err), nil, nil
		}

		latestDeployments := make([]map[string]any, 0, len(deployments))
		for _, deployment := range deployments {
			if deployment == nil {
				continue
			}
			latestDeployments = append(latestDeployments, map[string]any{
				"id":                 deployment.ID,
				"status":             deployment.Status,
				"status_description": deployment.StatusDescription,
				"job_version":        deployment.JobVersion,
			})
		}

		output := map[string]any{
			"job": map[string]any{
				"id":               derefString(job.ID),
				"name":             derefString(job.Name),
				"namespace":        derefString(job.Namespace),
				"type":             derefString(job.Type),
				"priority":         derefInt(job.Priority),
				"version":          derefUint64(job.Version),
				"stable":           derefBool(job.Stable),
				"stop":             derefBool(job.Stop),
				"submit_time":      formatSubmitTime(derefInt64(job.SubmitTime)),
				"datacenters":      job.Datacenters,
				"meta":             job.Meta,
				"task_group_count": len(job.TaskGroups),
			},
			"summary": map[string]any{
				"job_id":    summary.JobID,
				"namespace": summary.Namespace,
				"groups":    summary.Summary,
				"children":  summary.Children,
				"meta":      metaMap(summaryMeta),
			},
			"deployments": map[string]any{
				"items": latestDeployments,
				"meta":  metaMap(deploymentsMeta),
			},
			"meta": metaMap(queryMeta),
		}

		return okResult(fmt.Sprintf("Job %s is %s with %d task groups.", derefString(job.ID), derefString(job.Status), len(job.TaskGroups))), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_job_scale_status",
		Description: "Get scaling status for a Nomad job by job ID.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		JobID string `json:"job_id" jsonschema:"full Nomad job ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		scaleStatus, queryMeta, err := nomadClient.GetJobScaleStatus(input.JobID, input.objectQueryInput.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		groupNames := make([]string, 0, len(scaleStatus.TaskGroups))
		for name := range scaleStatus.TaskGroups {
			groupNames = append(groupNames, name)
		}
		sort.Strings(groupNames)

		groups := make([]map[string]any, 0, len(groupNames))
		for _, name := range groupNames {
			group := scaleStatus.TaskGroups[name]
			recentEvents := make([]map[string]any, 0, len(group.Events))
			for _, event := range group.Events {
				recentEvents = append(recentEvents, map[string]any{
					"count":          event.Count,
					"previous_count": event.PreviousCount,
					"error":          event.Error,
					"message":        event.Message,
					"meta":           event.Meta,
					"eval_id":        event.EvalID,
					"create_index":   event.CreateIndex,
					"time":           event.Time,
				})
			}

			groups = append(groups, map[string]any{
				"name":        name,
				"desired":     group.Desired,
				"placed":      group.Placed,
				"running":     group.Running,
				"healthy":     group.Healthy,
				"unhealthy":   group.Unhealthy,
				"event_count": len(group.Events),
				"events":      recentEvents,
			})
		}

		output := map[string]any{
			"job_id":      scaleStatus.JobID,
			"namespace":   scaleStatus.Namespace,
			"job_stopped": scaleStatus.JobStopped,
			"task_groups": groups,
			"meta":        metaMap(queryMeta),
		}

		return okResult(fmt.Sprintf("Job %s has scaling status for %d task groups.", scaleStatus.JobID, len(groups))), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_job_evaluations",
		Description: "List evaluations for a specific Nomad job.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		JobID string `json:"job_id" jsonschema:"full Nomad job ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		evaluations, queryMeta, err := nomadClient.ListJobEvaluations(input.JobID, input.objectQueryInput.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		items := make([]map[string]any, 0, len(evaluations))
		for _, evaluation := range evaluations {
			if evaluation == nil {
				continue
			}

			failedTaskGroups := make([]string, 0, len(evaluation.FailedTGAllocs))
			for group := range evaluation.FailedTGAllocs {
				failedTaskGroups = append(failedTaskGroups, group)
			}
			sort.Strings(failedTaskGroups)

			items = append(items, map[string]any{
				"id":                 evaluation.ID,
				"status":             evaluation.Status,
				"status_description": evaluation.StatusDescription,
				"type":               evaluation.Type,
				"triggered_by":       evaluation.TriggeredBy,
				"namespace":          evaluation.Namespace,
				"job_id":             evaluation.JobID,
				"deployment_id":      evaluation.DeploymentID,
				"priority":           evaluation.Priority,
				"queued_allocations": evaluation.QueuedAllocations,
				"failed_task_groups": failedTaskGroups,
				"next_eval":          evaluation.NextEval,
				"previous_eval":      evaluation.PreviousEval,
				"blocked_eval":       evaluation.BlockedEval,
				"create_index":       evaluation.CreateIndex,
				"create_time":        formatUnixNanos(evaluation.CreateTime),
			})
		}

		output := map[string]any{
			"job_id":      input.JobID,
			"evaluations": items,
			"meta":        metaMap(queryMeta),
		}
		return okResult(summarizeList("evaluations", len(items), queryMeta)), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_job_allocations",
		Description: "List allocations for a specific job.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		JobID string `json:"job_id" jsonschema:"full Nomad job ID"`
		All   bool   `json:"all,omitempty" jsonschema:"include terminal allocations"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		allocations, queryMeta, err := nomadClient.ListJobAllocations(input.JobID, input.All, input.objectQueryInput.queryOptions().WithContext(ctx))
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
			"job_id":      input.JobID,
			"allocations": items,
			"meta":        metaMap(queryMeta),
		}
		return okResult(summarizeList("allocations", len(items), queryMeta)), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_job_services",
		Description: "List service registrations for a specific Nomad job.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		JobID string `json:"job_id" jsonschema:"full Nomad job ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		services, queryMeta, err := nomadClient.ListJobServices(input.JobID, input.objectQueryInput.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		items := make([]map[string]any, 0, len(services))
		for _, service := range services {
			if service == nil {
				continue
			}
			items = append(items, serviceRegistrationMap(service))
		}

		output := map[string]any{
			"job_id":   input.JobID,
			"services": items,
			"meta":     metaMap(queryMeta),
		}
		return okResult(summarizeList("services", len(items), queryMeta)), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "list_deployments",
		Description: "List Nomad deployments with optional namespace, prefix, filter, and pagination.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input listQueryInput) (*mcp.CallToolResult, map[string]any, error) {
		deployments, queryMeta, err := nomadClient.ListDeployments(input.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}

		items := make([]map[string]any, 0, len(deployments))
		for _, deployment := range deployments {
			if deployment == nil {
				continue
			}
			items = append(items, map[string]any{
				"id":                 deployment.ID,
				"namespace":          deployment.Namespace,
				"job_id":             deployment.JobID,
				"job_version":        deployment.JobVersion,
				"status":             deployment.Status,
				"status_description": deployment.StatusDescription,
				"multiregion":        deployment.IsMultiregion,
			})
		}

		output := map[string]any{
			"deployments": items,
			"meta":        metaMap(queryMeta),
		}
		return okResult(summarizeList("deployments", len(items), queryMeta)), output, nil
	})

	addTool(server, &mcp.Tool{
		Name:        "get_deployment",
		Description: "Get a Nomad deployment and the allocations attached to it.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input struct {
		DeploymentID string `json:"deployment_id" jsonschema:"full Nomad deployment ID"`
		objectQueryInput
	}) (*mcp.CallToolResult, map[string]any, error) {
		query := input.objectQueryInput.queryOptions().WithContext(ctx)
		deployment, queryMeta, err := nomadClient.GetDeployment(input.DeploymentID, query)
		if err != nil {
			return failResult(err), nil, nil
		}

		allocations, allocationsMeta, err := nomadClient.ListDeploymentAllocations(input.DeploymentID, query)
		if err != nil {
			return failResult(err), nil, nil
		}

		allocationItems := make([]map[string]any, 0, len(allocations))
		for _, allocation := range allocations {
			if allocation == nil {
				continue
			}
			allocationItems = append(allocationItems, map[string]any{
				"id":             allocation.ID,
				"namespace":      allocation.Namespace,
				"job_id":         allocation.JobID,
				"task_group":     allocation.TaskGroup,
				"desired_status": allocation.DesiredStatus,
				"client_status":  allocation.ClientStatus,
			})
		}

		output := map[string]any{
			"deployment": map[string]any{
				"id":                 deployment.ID,
				"namespace":          deployment.Namespace,
				"job_id":             deployment.JobID,
				"job_version":        deployment.JobVersion,
				"status":             deployment.Status,
				"status_description": deployment.StatusDescription,
				"multiregion":        deployment.IsMultiregion,
				"task_group_count":   len(deployment.TaskGroups),
			},
			"allocations": map[string]any{
				"items": allocationItems,
				"meta":  metaMap(allocationsMeta),
			},
			"meta": metaMap(queryMeta),
		}

		return okResult(fmt.Sprintf("Deployment %s is %s with %d allocations.", deployment.ID, deployment.Status, len(allocationItems))), output, nil
	})
}
