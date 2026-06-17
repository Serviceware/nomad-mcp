package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/nomad/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
)

const (
	nomadResourceScheme     = "nomad"
	nomadClusterSummaryURI  = "nomad://cluster/summary"
	defaultPromptTailLines  = 200
	maxPromptEmbeddedEvents = 10
	logStreamStdout         = "stdout"
	logStreamStderr         = "stderr"
)

func RegisterResources(server *mcp.Server, nomadClient client.Facade) {
	server.AddResource(&mcp.Resource{
		Name:        "cluster-summary",
		Title:       "Cluster Summary",
		Description: "Nomad cluster address, leader, peers, and regions.",
		MIMEType:    "application/json",
		URI:         nomadClusterSummaryURI,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		return readNomadResource(ctx, nomadClient, req.Params.URI)
	})

	for _, template := range []*mcp.ResourceTemplate{
		{
			Name:        "job-summary",
			Title:       "Job Summary",
			Description: "Job metadata, scheduler summary, and latest deployments.",
			MIMEType:    "application/json",
			URITemplate: "nomad://jobs/{namespace}/{job_id}/summary",
		},
		{
			Name:        "job-spec",
			Title:       "Job Spec",
			Description: "Canonical Nomad job specification returned by the API.",
			MIMEType:    "application/json",
			URITemplate: "nomad://jobs/{namespace}/{job_id}/spec",
		},
		{
			Name:        "job-evaluations",
			Title:       "Job Evaluations",
			Description: "Recent evaluations for a job.",
			MIMEType:    "application/json",
			URITemplate: "nomad://jobs/{namespace}/{job_id}/evaluations",
		},
		{
			Name:        "allocation-status",
			Title:       "Allocation Status",
			Description: "Allocation status, task states, and service registrations.",
			MIMEType:    "application/json",
			URITemplate: "nomad://allocs/{allocation_id}/status",
		},
		{
			Name:        "allocation-checks",
			Title:       "Allocation Checks",
			Description: "Service discovery health checks for an allocation.",
			MIMEType:    "application/json",
			URITemplate: "nomad://allocs/{allocation_id}/checks",
		},
		{
			Name:        "allocation-logs",
			Title:       "Allocation Logs",
			Description: "Bounded static tail of allocation task logs.",
			MIMEType:    "text/plain",
			URITemplate: "nomad://allocs/{allocation_id}/logs/{task_name}/{stream}",
		},
		{
			Name:        "node-status",
			Title:       "Node Status",
			Description: "Node health, resources, drivers, and allocation count.",
			MIMEType:    "application/json",
			URITemplate: "nomad://nodes/{node_id}/status",
		},
		{
			Name:        "node-allocations",
			Title:       "Node Allocations",
			Description: "Allocations currently placed on a node.",
			MIMEType:    "application/json",
			URITemplate: "nomad://nodes/{node_id}/allocations",
		},
		{
			Name:        "deployment-detail",
			Title:       "Deployment Detail",
			Description: "Deployment rollout status and attached allocations.",
			MIMEType:    "application/json",
			URITemplate: "nomad://deployments/{deployment_id}",
		},
		{
			Name:        "evaluation-detail",
			Title:       "Evaluation Detail",
			Description: "Evaluation status, failed task groups, and related allocations.",
			MIMEType:    "application/json",
			URITemplate: "nomad://evaluations/{evaluation_id}",
		},
	} {
		server.AddResourceTemplate(template, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return readNomadResource(ctx, nomadClient, req.Params.URI)
		})
	}
}

func readNomadResource(ctx context.Context, nomadClient client.Facade, uri string) (*mcp.ReadResourceResult, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	if parsed.Scheme != nomadResourceScheme {
		return nil, mcp.ResourceNotFoundError(uri)
	}

	segments, err := resourcePathSegments(parsed)
	if err != nil {
		return nil, err
	}

	switch parsed.Host {
	case "cluster":
		if len(segments) == 1 && segments[0] == "summary" {
			return clusterSummaryResource(ctx, nomadClient, uri)
		}
	case "jobs":
		if len(segments) != 3 {
			break
		}
		namespace, jobID, kind := segments[0], segments[1], segments[2]
		switch kind {
		case "summary":
			return jobSummaryResource(ctx, nomadClient, uri, namespace, jobID)
		case "spec":
			return jobSpecResource(ctx, nomadClient, uri, namespace, jobID)
		case "evaluations":
			return jobEvaluationsResource(ctx, nomadClient, uri, namespace, jobID)
		}
	case "allocs":
		if len(segments) == 2 {
			allocationID, kind := segments[0], segments[1]
			switch kind {
			case "status":
				return allocationStatusResource(ctx, nomadClient, uri, allocationID)
			case "checks":
				return allocationChecksResource(ctx, nomadClient, uri, allocationID)
			}
		}
		if len(segments) == 4 && segments[1] == "logs" {
			return allocationLogsResource(ctx, nomadClient, uri, segments[0], segments[2], segments[3])
		}
	case "nodes":
		if len(segments) == 2 {
			nodeID, kind := segments[0], segments[1]
			switch kind {
			case "status":
				return nodeStatusResource(ctx, nomadClient, uri, nodeID)
			case "allocations":
				return nodeAllocationsResource(ctx, nomadClient, uri, nodeID)
			}
		}
	case "deployments":
		if len(segments) == 1 {
			return deploymentResource(ctx, nomadClient, uri, segments[0])
		}
	case "evaluations":
		if len(segments) == 1 {
			return evaluationResource(ctx, nomadClient, uri, segments[0])
		}
	}

	return nil, mcp.ResourceNotFoundError(uri)
}

func clusterSummaryResource(ctx context.Context, nomadClient client.Facade, uri string) (*mcp.ReadResourceResult, error) {
	leader, err := nomadClient.Leader("")
	if err != nil {
		return nil, err
	}
	peers, err := nomadClient.Peers()
	if err != nil {
		return nil, err
	}
	regions, err := nomadClient.Regions()
	if err != nil {
		return nil, err
	}

	return jsonResource(uri, map[string]any{
		"address": nomadClient.Address(),
		"leader":  leader,
		"peers":   peers,
		"regions": regions,
	})
}

func jobSummaryResource(ctx context.Context, nomadClient client.Facade, uri string, namespace string, jobID string) (*mcp.ReadResourceResult, error) {
	query := queryWithNamespace(ctx, namespace)
	job, queryMeta, err := nomadClient.GetJob(jobID, query)
	if err != nil {
		return nil, err
	}
	summary, summaryMeta, err := nomadClient.GetJobSummary(jobID, query)
	if err != nil {
		return nil, err
	}
	deployments, deploymentsMeta, err := nomadClient.ListJobDeployments(jobID, false, query)
	if err != nil {
		return nil, err
	}

	items := make([]map[string]any, 0, len(deployments))
	for _, deployment := range deployments {
		if deployment == nil {
			continue
		}
		items = append(items, deploymentMap(deployment))
	}

	return jsonResource(uri, map[string]any{
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
			"items": items,
			"meta":  metaMap(deploymentsMeta),
		},
		"meta": metaMap(queryMeta),
	})
}

func jobSpecResource(ctx context.Context, nomadClient client.Facade, uri string, namespace string, jobID string) (*mcp.ReadResourceResult, error) {
	job, queryMeta, err := nomadClient.GetJob(jobID, queryWithNamespace(ctx, namespace))
	if err != nil {
		return nil, err
	}
	return jsonResource(uri, map[string]any{
		"job":  job,
		"meta": metaMap(queryMeta),
	})
}

func jobEvaluationsResource(ctx context.Context, nomadClient client.Facade, uri string, namespace string, jobID string) (*mcp.ReadResourceResult, error) {
	evaluations, queryMeta, err := nomadClient.ListJobEvaluations(jobID, queryWithNamespace(ctx, namespace))
	if err != nil {
		return nil, err
	}

	items := make([]map[string]any, 0, len(evaluations))
	for _, evaluation := range evaluations {
		if evaluation == nil {
			continue
		}
		items = append(items, evaluationMap(evaluation))
	}

	return jsonResource(uri, map[string]any{
		"job_id":      jobID,
		"namespace":   namespace,
		"evaluations": items,
		"meta":        metaMap(queryMeta),
	})
}

func allocationStatusResource(ctx context.Context, nomadClient client.Facade, uri string, allocationID string) (*mcp.ReadResourceResult, error) {
	query := queryWithContext(ctx)
	allocation, queryMeta, err := nomadClient.GetAllocation(allocationID, query)
	if err != nil {
		return nil, err
	}
	services, servicesMeta, err := nomadClient.ListAllocationServices(allocationID, query)
	if err != nil {
		return nil, err
	}

	serviceItems := make([]map[string]any, 0, len(services))
	for _, service := range services {
		if service == nil {
			continue
		}
		serviceItems = append(serviceItems, serviceRegistrationMap(service))
	}

	return jsonResource(uri, map[string]any{
		"allocation": map[string]any{
			"id":                    allocation.ID,
			"namespace":             allocation.Namespace,
			"eval_id":               allocation.EvalID,
			"node_id":               allocation.NodeID,
			"node_name":             allocation.NodeName,
			"job_id":                allocation.JobID,
			"task_group":            allocation.TaskGroup,
			"desired_status":        allocation.DesiredStatus,
			"desired_description":   allocation.DesiredDescription,
			"client_status":         allocation.ClientStatus,
			"client_description":    allocation.ClientDescription,
			"deployment_id":         allocation.DeploymentID,
			"followup_eval_id":      allocation.FollowupEvalID,
			"previous_allocation":   allocation.PreviousAllocation,
			"next_allocation":       allocation.NextAllocation,
			"create_time":           formatUnixNanos(allocation.CreateTime),
			"modify_time":           formatUnixNanos(allocation.ModifyTime),
			"network_status":        allocationNetworkMap(allocation.NetworkStatus),
			"deployment_status":     allocationDeploymentStatusMap(allocation.DeploymentStatus),
			"task_states":           taskStatesMap(allocation.TaskStates),
			"task_state_names":      sortedKeys(allocation.TaskStates),
			"preempted_allocations": allocation.PreemptedAllocations,
			"preempted_by":          allocation.PreemptedByAllocation,
		},
		"services": map[string]any{
			"items": serviceItems,
			"meta":  metaMap(servicesMeta),
		},
		"meta": metaMap(queryMeta),
	})
}

func allocationChecksResource(ctx context.Context, nomadClient client.Facade, uri string, allocationID string) (*mcp.ReadResourceResult, error) {
	checks, err := nomadClient.GetAllocationChecks(allocationID, queryWithContext(ctx))
	if err != nil {
		return nil, err
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

	return jsonResource(uri, map[string]any{
		"allocation_id": allocationID,
		"passing":       passing,
		"failing":       len(items) - passing,
		"checks":        items,
	})
}

func allocationLogsResource(ctx context.Context, nomadClient client.Facade, uri string, allocationID string, taskName string, stream string) (*mcp.ReadResourceResult, error) {
	logTail, err := nomadClient.GetAllocationLogs(allocationID, taskName, normalizeLogStream(stream), defaultPromptTailLines, queryWithContext(ctx))
	if err != nil {
		return nil, err
	}

	text := ""
	meta := mcp.Meta{}
	if logTail != nil {
		text = logTail.Text
		meta = mcp.Meta{
			"allocation_id":   allocationID,
			"task_name":       logTail.TaskName,
			"stream":          logTail.LogType,
			"requested_lines": logTail.RequestedLines,
			"applied_lines":   logTail.AppliedLines,
			"returned_bytes":  logTail.ReturnedBytes,
			"truncated":       logTail.Truncated,
		}
	}

	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: "text/plain",
			Text:     text,
			Meta:     meta,
		}},
	}, nil
}

func nodeStatusResource(ctx context.Context, nomadClient client.Facade, uri string, nodeID string) (*mcp.ReadResourceResult, error) {
	query := queryWithContext(ctx)
	node, queryMeta, err := nomadClient.GetNode(nodeID, query)
	if err != nil {
		return nil, err
	}
	allocations, allocationsMeta, err := nomadClient.ListNodeAllocations(nodeID, query)
	if err != nil {
		return nil, err
	}

	allocationItems := make([]map[string]any, 0, len(allocations))
	for _, allocation := range allocations {
		if allocation == nil {
			continue
		}
		allocationItems = append(allocationItems, allocationSummaryMap(allocation.Stub()))
	}

	return jsonResource(uri, map[string]any{
		"node": map[string]any{
			"id":                     node.ID,
			"name":                   node.Name,
			"http_addr":              node.HTTPAddr,
			"tls_enabled":            node.TLSEnabled,
			"datacenter":             node.Datacenter,
			"node_class":             node.NodeClass,
			"node_pool":              node.NodePool,
			"status":                 node.Status,
			"status_description":     node.StatusDescription,
			"scheduling_eligibility": node.SchedulingEligibility,
			"drain":                  node.Drain,
			"drain_strategy":         nodeDrainStrategyMap(node.DrainStrategy),
			"last_drain":             node.LastDrain,
			"attributes":             node.Attributes,
			"meta":                   node.Meta,
			"drivers":                nodeDriversMap(node.Drivers),
			"resources":              nodeResourcesMap(node),
			"events":                 nodeEventsMap(node.Events),
		},
		"allocations": map[string]any{
			"count": len(allocationItems),
			"items": allocationItems,
			"meta":  metaMap(allocationsMeta),
		},
		"meta": metaMap(queryMeta),
	})
}

func nodeAllocationsResource(ctx context.Context, nomadClient client.Facade, uri string, nodeID string) (*mcp.ReadResourceResult, error) {
	allocations, queryMeta, err := nomadClient.ListNodeAllocations(nodeID, queryWithContext(ctx))
	if err != nil {
		return nil, err
	}

	items := make([]map[string]any, 0, len(allocations))
	for _, allocation := range allocations {
		if allocation == nil {
			continue
		}
		items = append(items, allocationSummaryMap(allocation.Stub()))
	}

	return jsonResource(uri, map[string]any{
		"node_id":     nodeID,
		"allocations": items,
		"meta":        metaMap(queryMeta),
	})
}

func deploymentResource(ctx context.Context, nomadClient client.Facade, uri string, deploymentID string) (*mcp.ReadResourceResult, error) {
	query := queryWithContext(ctx)
	deployment, queryMeta, err := nomadClient.GetDeployment(deploymentID, query)
	if err != nil {
		return nil, err
	}
	allocations, allocationsMeta, err := nomadClient.ListDeploymentAllocations(deploymentID, query)
	if err != nil {
		return nil, err
	}

	allocationItems := make([]map[string]any, 0, len(allocations))
	for _, allocation := range allocations {
		if allocation == nil {
			continue
		}
		allocationItems = append(allocationItems, allocationListStubMap(allocation))
	}

	return jsonResource(uri, map[string]any{
		"deployment":  deploymentMap(deployment),
		"task_groups": deploymentTaskGroupsMap(deployment.TaskGroups),
		"allocations": map[string]any{
			"items": allocationItems,
			"meta":  metaMap(allocationsMeta),
		},
		"meta": metaMap(queryMeta),
	})
}

func evaluationResource(ctx context.Context, nomadClient client.Facade, uri string, evaluationID string) (*mcp.ReadResourceResult, error) {
	query := queryWithContext(ctx)
	evaluation, queryMeta, err := nomadClient.GetEvaluation(evaluationID, query)
	if err != nil {
		return nil, err
	}
	allocations, allocationsMeta, err := nomadClient.ListEvaluationAllocations(evaluationID, query)
	if err != nil {
		return nil, err
	}

	allocationItems := make([]map[string]any, 0, len(allocations))
	for _, allocation := range allocations {
		if allocation == nil {
			continue
		}
		allocationItems = append(allocationItems, allocationListStubMap(allocation))
	}

	return jsonResource(uri, map[string]any{
		"evaluation":          evaluationMap(evaluation),
		"related_evaluations": evaluationRelatedMap(evaluation.RelatedEvals),
		"failed_task_groups":  evaluationFailedTaskGroupsMap(evaluation.FailedTGAllocs),
		"allocations": map[string]any{
			"items": allocationItems,
			"meta":  metaMap(allocationsMeta),
		},
		"meta": metaMap(queryMeta),
	})
}

func jsonResource(uri string, payload any) (*mcp.ReadResourceResult, error) {
	body, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, err
	}
	return &mcp.ReadResourceResult{
		Contents: []*mcp.ResourceContents{{
			URI:      uri,
			MIMEType: "application/json",
			Text:     string(body),
		}},
	}, nil
}

func queryWithNamespace(ctx context.Context, namespace string) *api.QueryOptions {
	query := &api.QueryOptions{Namespace: namespace}
	return query.WithContext(ctx)
}

func queryWithContext(ctx context.Context) *api.QueryOptions {
	return (&api.QueryOptions{}).WithContext(ctx)
}

func resourcePathSegments(parsed *url.URL) ([]string, error) {
	trimmed := strings.Trim(parsed.Path, "/")
	if trimmed == "" {
		return nil, nil
	}
	parts := strings.Split(trimmed, "/")
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		decoded, err := url.PathUnescape(part)
		if err != nil {
			return nil, err
		}
		segments = append(segments, decoded)
	}
	return segments, nil
}

func taskStatesMap(taskStates map[string]*api.TaskState) map[string]any {
	items := make(map[string]any, len(taskStates))
	for _, name := range sortedKeys(taskStates) {
		state := taskStates[name]
		if state == nil {
			continue
		}
		recentEvents := state.Events
		if len(recentEvents) > maxPromptEmbeddedEvents {
			recentEvents = recentEvents[len(recentEvents)-maxPromptEmbeddedEvents:]
		}

		events := make([]map[string]any, 0, len(recentEvents))
		for _, event := range recentEvents {
			if event == nil {
				continue
			}
			description := event.DisplayMessage
			if description == "" {
				description = event.Message
			}
			events = append(events, map[string]any{
				"type":        event.Type,
				"time":        formatUnixNanos(event.Time),
				"description": description,
				"message":     event.Message,
			})
		}

		items[name] = map[string]any{
			"state":        state.State,
			"failed":       state.Failed,
			"restarts":     state.Restarts,
			"last_restart": formatTime(state.LastRestart),
			"started_at":   formatTime(state.StartedAt),
			"finished_at":  formatTime(state.FinishedAt),
			"events":       events,
		}
	}
	return items
}

func nodeDriversMap(drivers map[string]*api.DriverInfo) map[string]any {
	items := make(map[string]any, len(drivers))
	for _, name := range sortedKeys(drivers) {
		driver := drivers[name]
		if driver == nil {
			continue
		}
		items[name] = map[string]any{
			"detected":           driver.Detected,
			"healthy":            driver.Healthy,
			"health_description": driver.HealthDescription,
			"attributes":         driver.Attributes,
			"update_time":        formatTime(driver.UpdateTime),
		}
	}
	return items
}

func nodeResourcesMap(node *api.Node) map[string]any {
	if node == nil || node.NodeResources == nil {
		return map[string]any{}
	}

	// ReservedResources is nil on nodes with no reserved resources; treat reserved as 0.
	var reservedCPU, reservedMemoryMB, reservedDiskMB uint64
	if node.ReservedResources != nil {
		reservedCPU = node.ReservedResources.Cpu.CpuShares
		reservedMemoryMB = node.ReservedResources.Memory.MemoryMB
		reservedDiskMB = node.ReservedResources.Disk.DiskMB
	}

	availableCPU := node.NodeResources.Cpu.CpuShares - int64(reservedCPU)
	availableMemoryMB := node.NodeResources.Memory.MemoryMB - int64(reservedMemoryMB)
	availableDiskMB := node.NodeResources.Disk.DiskMB - int64(reservedDiskMB)

	return map[string]any{
		"cpu_shares":           node.NodeResources.Cpu.CpuShares,
		"memory_mb":            node.NodeResources.Memory.MemoryMB,
		"disk_mb":              node.NodeResources.Disk.DiskMB,
		"reserved_cpu_shares":  reservedCPU,
		"reserved_memory_mb":   reservedMemoryMB,
		"reserved_disk_mb":     reservedDiskMB,
		"available_cpu_shares": availableCPU,
		"available_memory_mb":  availableMemoryMB,
		"available_disk_mb":    availableDiskMB,
		"network_count":        len(node.NodeResources.Networks),
		"device_count":         len(node.NodeResources.Devices),
	}
}

func nodeEventsMap(events []*api.NodeEvent) []map[string]any {
	items := make([]map[string]any, 0, len(events))
	for _, event := range events {
		if event == nil {
			continue
		}
		items = append(items, map[string]any{
			"message":   event.Message,
			"subsystem": event.Subsystem,
			"details":   event.Details,
			"timestamp": formatTime(event.Timestamp),
		})
	}
	return items
}

func nodeDrainStrategyMap(strategy *api.DrainStrategy) map[string]any {
	if strategy == nil {
		return map[string]any{}
	}
	return map[string]any{
		"deadline":           strategy.Deadline.String(),
		"force_deadline":     formatTime(strategy.ForceDeadline),
		"ignore_system_jobs": strategy.IgnoreSystemJobs,
		"started_at":         formatTime(strategy.StartedAt),
	}
}

func allocationNetworkMap(status *api.AllocNetworkStatus) map[string]any {
	if status == nil {
		return map[string]any{}
	}
	return map[string]any{
		"interface_name": status.InterfaceName,
		"address":        status.Address,
		"address_ipv6":   status.AddressIPv6,
		"dns":            status.DNS,
	}
}

func allocationDeploymentStatusMap(status *api.AllocDeploymentStatus) map[string]any {
	if status == nil {
		return map[string]any{}
	}
	return map[string]any{
		"healthy":      derefBool(status.Healthy),
		"timestamp":    formatTime(status.Timestamp),
		"canary":       status.Canary,
		"modify_index": status.ModifyIndex,
	}
}

func allocationSummaryMap(allocation *api.AllocationListStub) map[string]any {
	if allocation == nil {
		return map[string]any{}
	}
	return allocationListStubMap(allocation)
}

func allocationListStubMap(allocation *api.AllocationListStub) map[string]any {
	return map[string]any{
		"id":                  allocation.ID,
		"namespace":           allocation.Namespace,
		"eval_id":             allocation.EvalID,
		"node_id":             allocation.NodeID,
		"node_name":           allocation.NodeName,
		"job_id":              allocation.JobID,
		"task_group":          allocation.TaskGroup,
		"desired_status":      allocation.DesiredStatus,
		"desired_description": allocation.DesiredDescription,
		"client_status":       allocation.ClientStatus,
		"client_description":  allocation.ClientDescription,
		"followup_eval_id":    allocation.FollowupEvalID,
		"next_allocation":     allocation.NextAllocation,
		"task_state_names":    sortedKeys(allocation.TaskStates),
	}
}

func deploymentMap(deployment *api.Deployment) map[string]any {
	if deployment == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                 deployment.ID,
		"namespace":          deployment.Namespace,
		"job_id":             deployment.JobID,
		"job_version":        deployment.JobVersion,
		"status":             deployment.Status,
		"status_description": deployment.StatusDescription,
		"multiregion":        deployment.IsMultiregion,
		"create_time":        formatUnixNanos(deployment.CreateTime),
		"modify_time":        formatUnixNanos(deployment.ModifyTime),
	}
}

func deploymentTaskGroupsMap(taskGroups map[string]*api.DeploymentState) map[string]any {
	items := make(map[string]any, len(taskGroups))
	for _, name := range sortedKeys(taskGroups) {
		state := taskGroups[name]
		if state == nil {
			continue
		}
		items[name] = map[string]any{
			"auto_revert":         state.AutoRevert,
			"progress_deadline":   state.ProgressDeadline.String(),
			"require_progress_by": formatTime(state.RequireProgressBy),
			"promoted":            state.Promoted,
			"desired_canaries":    state.DesiredCanaries,
			"desired_total":       state.DesiredTotal,
			"placed_allocs":       state.PlacedAllocs,
			"healthy_allocs":      state.HealthyAllocs,
			"unhealthy_allocs":    state.UnhealthyAllocs,
			"placed_canaries":     state.PlacedCanaries,
		}
	}
	return items
}

func evaluationMap(evaluation *api.Evaluation) map[string]any {
	if evaluation == nil {
		return map[string]any{}
	}
	return map[string]any{
		"id":                     evaluation.ID,
		"priority":               evaluation.Priority,
		"type":                   evaluation.Type,
		"triggered_by":           evaluation.TriggeredBy,
		"namespace":              evaluation.Namespace,
		"job_id":                 evaluation.JobID,
		"node_id":                evaluation.NodeID,
		"deployment_id":          evaluation.DeploymentID,
		"status":                 evaluation.Status,
		"status_description":     evaluation.StatusDescription,
		"wait_until":             formatTime(evaluation.WaitUntil),
		"next_eval":              evaluation.NextEval,
		"previous_eval":          evaluation.PreviousEval,
		"blocked_eval":           evaluation.BlockedEval,
		"queued_allocations":     evaluation.QueuedAllocations,
		"class_eligibility":      evaluation.ClassEligibility,
		"escaped_computed_class": evaluation.EscapedComputedClass,
		"quota_limit_reached":    evaluation.QuotaLimitReached,
		"snapshot_index":         evaluation.SnapshotIndex,
		"create_index":           evaluation.CreateIndex,
		"modify_index":           evaluation.ModifyIndex,
		"create_time":            formatUnixNanos(evaluation.CreateTime),
		"modify_time":            formatUnixNanos(evaluation.ModifyTime),
		"plan_annotations":       evaluation.PlanAnnotations,
	}
}

func evaluationRelatedMap(related []*api.EvaluationStub) []map[string]any {
	items := make([]map[string]any, 0, len(related))
	for _, evaluation := range related {
		if evaluation == nil {
			continue
		}
		items = append(items, map[string]any{
			"id":                 evaluation.ID,
			"priority":           evaluation.Priority,
			"type":               evaluation.Type,
			"triggered_by":       evaluation.TriggeredBy,
			"namespace":          evaluation.Namespace,
			"job_id":             evaluation.JobID,
			"node_id":            evaluation.NodeID,
			"deployment_id":      evaluation.DeploymentID,
			"status":             evaluation.Status,
			"status_description": evaluation.StatusDescription,
			"wait_until":         formatTime(evaluation.WaitUntil),
			"next_eval":          evaluation.NextEval,
			"previous_eval":      evaluation.PreviousEval,
			"blocked_eval":       evaluation.BlockedEval,
			"create_index":       evaluation.CreateIndex,
			"modify_index":       evaluation.ModifyIndex,
			"create_time":        formatUnixNanos(evaluation.CreateTime),
			"modify_time":        formatUnixNanos(evaluation.ModifyTime),
		})
	}
	return items
}

func evaluationFailedTaskGroupsMap(groups map[string]*api.AllocationMetric) map[string]any {
	items := make(map[string]any, len(groups))
	for _, name := range sortedKeys(groups) {
		metric := groups[name]
		if metric == nil {
			continue
		}
		items[name] = metric
	}
	return items
}

func formatTime(value interface {
	IsZero() bool
	String() string
}) string {
	if value.IsZero() {
		return ""
	}
	return value.String()
}

func normalizeLogStream(stream string) string {
	if stream == logStreamStdout {
		return logStreamStdout
	}
	return logStreamStderr
}

func parsePromptTailLines(raw string) int {
	if raw == "" {
		return defaultPromptTailLines
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultPromptTailLines
	}
	return value
}

func embeddedResourceForURI(ctx context.Context, nomadClient client.Facade, uri string) (*mcp.EmbeddedResource, error) {
	result, err := readNomadResource(ctx, nomadClient, uri)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Contents) == 0 || result.Contents[0] == nil {
		return nil, fmt.Errorf("resource %s returned no content", uri)
	}
	return &mcp.EmbeddedResource{Resource: result.Contents[0]}, nil
}

func defaultTaskName(allocation *api.Allocation) string {
	if allocation == nil || len(allocation.TaskStates) == 0 {
		return ""
	}
	names := sortedKeys(allocation.TaskStates)
	if len(names) == 0 {
		return ""
	}
	return names[0]
}
