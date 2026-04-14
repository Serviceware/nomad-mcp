package tools

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
)

func RegisterPrompts(server *mcp.Server, nomadClient client.Facade) {
	server.AddPrompt(&mcp.Prompt{
		Name:        "debug_allocation",
		Title:       "Debug Allocation",
		Description: "Analyze an allocation failure or crash using status, checks, and a bounded log tail.",
		Arguments: []*mcp.PromptArgument{
			{Name: "alloc_id", Description: "Nomad allocation ID", Required: true},
			{Name: "task_name", Description: "Task name to use for logs"},
			{Name: "stream", Description: "Log stream: stdout or stderr"},
			{Name: "tail_lines", Description: "Approximate number of log lines to include"},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		allocID := req.Params.Arguments["alloc_id"]
		if allocID == "" {
			return nil, fmt.Errorf("alloc_id is required")
		}

		taskName := req.Params.Arguments["task_name"]
		stream := normalizeLogStream(req.Params.Arguments["stream"])
		tailLines := parsePromptTailLines(req.Params.Arguments["tail_lines"])

		if taskName == "" {
			allocation, _, err := nomadClient.GetAllocation(allocID, queryWithContext(ctx))
			if err == nil {
				taskName = defaultTaskName(allocation)
			}
		}

		messages := []*mcp.PromptMessage{{
			Role: "user",
			Content: &mcp.TextContent{Text: fmt.Sprintf(
				"You are an expert Nomad SRE. Analyze allocation %s using the attached status, checks, and recent logs. Identify the most likely failure mode, explain the evidence, and suggest the smallest safe fix to the job configuration.",
				allocID,
			)},
		}}

		for _, uri := range []string{
			fmt.Sprintf("nomad://allocs/%s/status", allocID),
			fmt.Sprintf("nomad://allocs/%s/checks", allocID),
		} {
			resource, err := embeddedResourceForURI(ctx, nomadClient, uri)
			if err != nil {
				return nil, err
			}
			messages = append(messages, &mcp.PromptMessage{Role: "user", Content: resource})
		}

		if taskName != "" {
			logURI := fmt.Sprintf("nomad://allocs/%s/logs/%s/%s", allocID, taskName, stream)
			logTail, err := nomadClient.GetAllocationLogs(allocID, taskName, stream, tailLines, queryWithContext(ctx))
			if err != nil {
				return nil, err
			}
			messages = append(messages, &mcp.PromptMessage{Role: "user", Content: &mcp.EmbeddedResource{Resource: &mcp.ResourceContents{
				URI:      logURI,
				MIMEType: "text/plain",
				Text:     logTail.Text,
				Meta: mcp.Meta{
					"requested_lines": logTail.RequestedLines,
					"applied_lines":   logTail.AppliedLines,
					"returned_bytes":  logTail.ReturnedBytes,
					"truncated":       logTail.Truncated,
				},
			}}})
		}

		return &mcp.GetPromptResult{Description: "Allocation debugging workflow", Messages: messages}, nil
	})

	server.AddPrompt(&mcp.Prompt{
		Name:        "explain_evaluation",
		Title:       "Explain Evaluation",
		Description: "Explain why a Nomad evaluation failed to place allocations.",
		Arguments:   []*mcp.PromptArgument{{Name: "eval_id", Description: "Nomad evaluation ID", Required: true}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		evalID := req.Params.Arguments["eval_id"]
		if evalID == "" {
			return nil, fmt.Errorf("eval_id is required")
		}

		resource, err := embeddedResourceForURI(ctx, nomadClient, fmt.Sprintf("nomad://evaluations/%s", evalID))
		if err != nil {
			return nil, err
		}

		return &mcp.GetPromptResult{
			Description: "Evaluation explanation workflow",
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: fmt.Sprintf("Explain exactly why evaluation %s could not place all allocations. Reference failed task groups, queued allocations, related evaluations, and placement evidence from the attached resource.", evalID)}},
				{Role: "user", Content: resource},
			},
		}, nil
	})

	server.AddPrompt(&mcp.Prompt{
		Name:        "diagnose_deployment",
		Title:       "Diagnose Deployment",
		Description: "Analyze a deployment rollout and identify blockers or regressions.",
		Arguments:   []*mcp.PromptArgument{{Name: "deployment_id", Description: "Nomad deployment ID", Required: true}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		deploymentID := req.Params.Arguments["deployment_id"]
		if deploymentID == "" {
			return nil, fmt.Errorf("deployment_id is required")
		}

		resource, err := embeddedResourceForURI(ctx, nomadClient, fmt.Sprintf("nomad://deployments/%s", deploymentID))
		if err != nil {
			return nil, err
		}

		return &mcp.GetPromptResult{
			Description: "Deployment diagnosis workflow",
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: fmt.Sprintf("Review deployment %s and explain whether the rollout is healthy, blocked, or regressing. Highlight the affected task groups and allocations, then suggest the safest next operator action.", deploymentID)}},
				{Role: "user", Content: resource},
			},
		}, nil
	})

	server.AddPrompt(&mcp.Prompt{
		Name:        "investigate_node",
		Title:       "Investigate Node",
		Description: "Review node health, drain status, and current placements.",
		Arguments:   []*mcp.PromptArgument{{Name: "node_id", Description: "Nomad node ID", Required: true}},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		nodeID := req.Params.Arguments["node_id"]
		if nodeID == "" {
			return nil, fmt.Errorf("node_id is required")
		}

		statusResource, err := embeddedResourceForURI(ctx, nomadClient, fmt.Sprintf("nomad://nodes/%s/status", nodeID))
		if err != nil {
			return nil, err
		}
		allocationsResource, err := embeddedResourceForURI(ctx, nomadClient, fmt.Sprintf("nomad://nodes/%s/allocations", nodeID))
		if err != nil {
			return nil, err
		}

		return &mcp.GetPromptResult{
			Description: "Node investigation workflow",
			Messages: []*mcp.PromptMessage{
				{Role: "user", Content: &mcp.TextContent{Text: fmt.Sprintf("Analyze node %s for scheduling, drain, or health problems. Use the attached node status and placement data to explain the node's condition and any likely operational risk.", nodeID)}},
				{Role: "user", Content: statusResource},
				{Role: "user", Content: allocationsResource},
			},
		}, nil
	})

	server.AddPrompt(&mcp.Prompt{
		Name:        "explain_job",
		Title:       "Explain Job",
		Description: "Summarize a Nomad job's configuration, runtime state, and recent scheduler activity.",
		Arguments: []*mcp.PromptArgument{
			{Name: "namespace", Description: "Nomad namespace", Required: true},
			{Name: "job_id", Description: "Nomad job ID", Required: true},
		},
	}, func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		namespace := req.Params.Arguments["namespace"]
		jobID := req.Params.Arguments["job_id"]
		if namespace == "" || jobID == "" {
			return nil, fmt.Errorf("namespace and job_id are required")
		}

		messages := []*mcp.PromptMessage{{
			Role: "user",
			Content: &mcp.TextContent{Text: fmt.Sprintf(
				"Explain how job %s in namespace %s is configured and behaving right now. Use the attached summary, canonical spec, and evaluation history to describe the runtime state and any obvious issues or risks.",
				jobID,
				namespace,
			)},
		}}

		for _, uri := range []string{
			fmt.Sprintf("nomad://jobs/%s/%s/summary", namespace, jobID),
			fmt.Sprintf("nomad://jobs/%s/%s/spec", namespace, jobID),
			fmt.Sprintf("nomad://jobs/%s/%s/evaluations", namespace, jobID),
		} {
			resource, err := embeddedResourceForURI(ctx, nomadClient, uri)
			if err != nil {
				return nil, err
			}
			messages = append(messages, &mcp.PromptMessage{Role: "user", Content: resource})
		}

		return &mcp.GetPromptResult{Description: "Job explanation workflow", Messages: messages}, nil
	})
}
