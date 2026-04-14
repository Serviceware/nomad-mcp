package server

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/hashicorp/nomad/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
	"github.com/shoenig/test/must"
)

type fakeNomadClient struct{}

func (fakeNomadClient) Close()                               {}
func (fakeNomadClient) Address() string                      { return "http://127.0.0.1:4646" }
func (fakeNomadClient) Leader(region string) (string, error) { return "127.0.0.1:4647", nil }
func (fakeNomadClient) Peers() ([]string, error)             { return []string{"127.0.0.1:4647"}, nil }
func (fakeNomadClient) Regions() ([]string, error)           { return []string{"global"}, nil }
func (fakeNomadClient) ListNamespaces(query *api.QueryOptions) ([]*api.Namespace, *api.QueryMeta, error) {
	return []*api.Namespace{{Name: "default", Description: "Default namespace"}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListNodes(query *api.QueryOptions) ([]*api.NodeListStub, *api.QueryMeta, error) {
	return []*api.NodeListStub{{ID: "node-1", Name: "node-1", Datacenter: "dc1", NodePool: "default", Status: "ready"}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) GetNode(nodeID string, query *api.QueryOptions) (*api.Node, *api.QueryMeta, error) {
	return &api.Node{ID: nodeID, Name: "node-1", Datacenter: "dc1", NodePool: "default", Status: "ready", Attributes: map[string]string{}, Meta: map[string]string{}, Drivers: map[string]*api.DriverInfo{}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListNodeAllocations(nodeID string, query *api.QueryOptions) ([]*api.Allocation, *api.QueryMeta, error) {
	return []*api.Allocation{}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListJobs(query *api.QueryOptions) ([]*api.JobListStub, *api.QueryMeta, error) {
	return []*api.JobListStub{{ID: "example", Name: "example", Namespace: "default", Type: "service", Status: "running", Meta: map[string]string{"department": "financial", "team": "platform"}}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) GetJob(jobID string, query *api.QueryOptions) (*api.Job, *api.QueryMeta, error) {
	jobName := "example"
	namespace := "default"
	jobType := "service"
	priority := 50
	version := uint64(1)
	stable := true
	stop := false
	submitTime := int64(1)
	return &api.Job{ID: &jobID, Name: &jobName, Namespace: &namespace, Type: &jobType, Priority: &priority, Version: &version, Stable: &stable, Stop: &stop, SubmitTime: &submitTime}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) GetJobScaleStatus(jobID string, query *api.QueryOptions) (*api.JobScaleStatusResponse, *api.QueryMeta, error) {
	return &api.JobScaleStatusResponse{JobID: jobID, Namespace: "default", TaskGroups: map[string]api.TaskGroupScaleStatus{"cache": {Desired: 1, Placed: 1, Running: 1, Healthy: 1, Unhealthy: 0}}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) GetJobSummary(jobID string, query *api.QueryOptions) (*api.JobSummary, *api.QueryMeta, error) {
	return &api.JobSummary{JobID: jobID, Namespace: "default", Summary: map[string]api.TaskGroupSummary{}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListJobEvaluations(jobID string, query *api.QueryOptions) ([]*api.Evaluation, *api.QueryMeta, error) {
	return []*api.Evaluation{{ID: "eval-1", JobID: jobID, Namespace: "default", Status: api.EvalStatusComplete, TriggeredBy: "job-register", CreateIndex: 1, CreateTime: 1}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListJobAllocations(jobID string, all bool, query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error) {
	return []*api.AllocationListStub{}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListJobDeployments(jobID string, all bool, query *api.QueryOptions) ([]*api.Deployment, *api.QueryMeta, error) {
	return []*api.Deployment{}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListJobServices(jobID string, query *api.QueryOptions) ([]*api.ServiceRegistration, *api.QueryMeta, error) {
	return []*api.ServiceRegistration{{ID: "svc-1", ServiceName: "example-http", Namespace: "default", JobID: jobID, AllocID: "alloc-1", NodeID: "node-1", Datacenter: "dc1", Address: "127.0.0.1", Port: 8080}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) GetEvaluation(evalID string, query *api.QueryOptions) (*api.Evaluation, *api.QueryMeta, error) {
	return &api.Evaluation{ID: evalID, Namespace: "default", JobID: "example", Status: api.EvalStatusFailed, StatusDescription: "insufficient resources", TriggeredBy: "job-register", FailedTGAllocs: map[string]*api.AllocationMetric{"cache": {}}, QueuedAllocations: map[string]int{"cache": 1}, CreateIndex: 2, CreateTime: 2}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListEvaluationAllocations(evalID string, query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error) {
	return []*api.AllocationListStub{{ID: "alloc-1", EvalID: evalID, Namespace: "default", NodeID: "node-1", JobID: "example", TaskGroup: "cache", DesiredStatus: "run", ClientStatus: "failed"}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListDeployments(query *api.QueryOptions) ([]*api.Deployment, *api.QueryMeta, error) {
	return []*api.Deployment{}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) GetDeployment(deploymentID string, query *api.QueryOptions) (*api.Deployment, *api.QueryMeta, error) {
	return &api.Deployment{ID: deploymentID, Namespace: "default", JobID: "example", Status: "successful", StatusDescription: "Deployment is successful", TaskGroups: map[string]*api.DeploymentState{"cache": {DesiredTotal: 1, PlacedAllocs: 1, HealthyAllocs: 1}}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListDeploymentAllocations(deploymentID string, query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error) {
	return []*api.AllocationListStub{}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListAllocations(query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error) {
	return []*api.AllocationListStub{}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) GetAllocation(allocationID string, query *api.QueryOptions) (*api.Allocation, *api.QueryMeta, error) {
	return &api.Allocation{ID: allocationID, Namespace: "default", NodeID: "node-1", NodeName: "node-1", JobID: "example", ClientStatus: "running", TaskStates: map[string]*api.TaskState{"web": {State: "running"}}}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) GetAllocationChecks(allocationID string, query *api.QueryOptions) (api.AllocCheckStatuses, error) {
	return api.AllocCheckStatuses{"check-1": {ID: "check-1", Check: "http", Task: "web", Service: "example-http", Status: "success", Timestamp: 1}}, nil
}
func (fakeNomadClient) ListAllocationServices(allocationID string, query *api.QueryOptions) ([]*api.ServiceRegistration, *api.QueryMeta, error) {
	return []*api.ServiceRegistration{}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) GetAllocationLogs(allocationID string, taskName string, logType string, lines int, query *api.QueryOptions) (*client.AllocationLogTail, error) {
	return &client.AllocationLogTail{Text: "example log line\n", RequestedLines: lines, AppliedLines: lines, ReturnedBytes: len("example log line\n"), Truncated: false, LogType: logType, TaskName: taskName}, nil
}

func TestServerListsToolsAndCallsClusterStatus(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(fakeNomadClient{}, logger)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	must.NoError(t, err)
	defer serverSession.Close()

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	must.NoError(t, err)
	defer clientSession.Close()

	toolsResult, err := clientSession.ListTools(ctx, nil)
	must.NoError(t, err)
	must.Positive(t, len(toolsResult.Tools))

	resourcesResult, err := clientSession.ListResources(ctx, nil)
	must.NoError(t, err)
	must.Positive(t, len(resourcesResult.Resources))

	resourceTemplates, err := clientSession.ListResourceTemplates(ctx, nil)
	must.NoError(t, err)
	must.Positive(t, len(resourceTemplates.ResourceTemplates))

	promptsResult, err := clientSession.ListPrompts(ctx, nil)
	must.NoError(t, err)
	must.Positive(t, len(promptsResult.Prompts))

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_cluster_status"})
	must.NoError(t, err)
	must.False(t, result.IsError)
	must.Positive(t, len(result.Content))

	scaleStatus, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_job_scale_status", Arguments: map[string]any{"job_id": "example"}})
	must.NoError(t, err)
	must.False(t, scaleStatus.IsError)
	must.Positive(t, len(scaleStatus.Content))

	resource, err := clientSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: "nomad://jobs/default/example/summary"})
	must.NoError(t, err)
	must.Positive(t, len(resource.Contents))

	prompt, err := clientSession.GetPrompt(ctx, &mcp.GetPromptParams{Name: "debug_allocation", Arguments: map[string]string{"alloc_id": "alloc-1", "task_name": "web"}})
	must.NoError(t, err)
	must.Positive(t, len(prompt.Messages))
}

func TestListJobsIncludesMetadataForDiscovery(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(fakeNomadClient{}, logger)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	must.NoError(t, err)
	defer serverSession.Close()

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	must.NoError(t, err)
	defer clientSession.Close()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "list_jobs"})
	must.NoError(t, err)
	must.False(t, result.IsError)

	structured, ok := result.StructuredContent.(map[string]any)
	must.True(t, ok)

	jobs, ok := structured["jobs"].([]any)
	must.True(t, ok)
	must.Len(t, 1, jobs)

	job, ok := jobs[0].(map[string]any)
	must.True(t, ok)
	must.Eq(t, "department=financial, team=platform", job["meta_summary"])

	meta, ok := job["meta"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, "financial", meta["department"])
	must.Eq(t, "platform", meta["team"])
}

func TestListRegionsToolSchemaIncludesProperties(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(fakeNomadClient{}, logger)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	must.NoError(t, err)
	defer serverSession.Close()

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	must.NoError(t, err)
	defer clientSession.Close()

	toolsResult, err := clientSession.ListTools(ctx, nil)
	must.NoError(t, err)

	if len(toolsResult.Tools) == 0 {
		t.Fatal("expected registered tools")
	}

	toolByName := make(map[string]*mcp.Tool, len(toolsResult.Tools))
	for _, tool := range toolsResult.Tools {
		if tool == nil {
			continue
		}

		toolByName[tool.Name] = tool

		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("tool %s input schema has unexpected type %T", tool.Name, tool.InputSchema)
		}
		must.Eq(t, "object", schema["type"])

		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("tool %s properties has unexpected type %T", tool.Name, schema["properties"])
		}
		_ = properties
	}

	listRegions := toolByName["list_regions"]
	for _, tool := range toolsResult.Tools {
		if tool != nil && tool.Name == "list_regions" {
			listRegions = tool
			break
		}
	}
	if listRegions == nil {
		t.Fatal("list_regions tool not found")
	}

	schema, ok := listRegions.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("list_regions input schema has unexpected type %T", listRegions.InputSchema)
	}
	must.Eq(t, "object", schema["type"])
	if _, ok := schema["properties"]; !ok {
		t.Fatalf("list_regions input schema is missing properties: %#v", schema)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("list_regions properties has unexpected type %T", schema["properties"])
	}
	must.Eq(t, 0, len(properties))
	_, ok = schema["required"]
	must.False(t, ok)

	getJob := toolByName["get_job"]
	if getJob == nil {
		t.Fatal("get_job tool not found")
	}

	getJobSchema, ok := getJob.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("get_job input schema has unexpected type %T", getJob.InputSchema)
	}

	getJobProperties, ok := getJobSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("get_job properties has unexpected type %T", getJobSchema["properties"])
	}

	jobIDProperty, ok := getJobProperties["job_id"].(map[string]any)
	if !ok {
		t.Fatalf("get_job job_id property has unexpected type %T", getJobProperties["job_id"])
	}
	must.Eq(t, "full Nomad job ID", jobIDProperty["description"])
}
