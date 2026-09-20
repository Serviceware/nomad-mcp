package server

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
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
	// NodeResources is populated but ReservedResources is deliberately left nil to
	// exercise the no-reserved-resources path in nodeResourcesMap (regression guard).
	return &api.Node{
		ID: nodeID, Name: "node-1", Datacenter: "dc1", NodePool: "default", Status: "ready",
		Attributes: map[string]string{}, Meta: map[string]string{}, Drivers: map[string]*api.DriverInfo{},
		NodeResources: &api.NodeResources{
			Cpu:    api.NodeCpuResources{CpuShares: 4000},
			Memory: api.NodeMemoryResources{MemoryMB: 8192},
			Disk:   api.NodeDiskResources{DiskMB: 50000},
		},
	}, &api.QueryMeta{}, nil
}
func (fakeNomadClient) ListNodeAllocations(nodeID string, query *api.QueryOptions) ([]*api.Allocation, *api.QueryMeta, error) {
	return []*api.Allocation{{ID: "alloc-1", Namespace: "default", NodeID: nodeID, JobID: "example", ClientStatus: "running"}}, &api.QueryMeta{}, nil
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
	status := "running"
	return &api.Job{
		ID: &jobID, Name: &jobName, Namespace: &namespace, Type: &jobType, Status: &status,
		Priority: &priority, Version: &version, Stable: &stable, Stop: &stop, SubmitTime: &submitTime,
		TaskGroups: []*api.TaskGroup{fakeTaskGroup()},
	}, &api.QueryMeta{}, nil
}

// fakeTaskGroup carries the spec detail inspect_job projects: resource requests
// with and without memory_max, a sensitive env value, a literal secret nested in
// driver config, and a pure interpolation that must survive redaction.
func fakeTaskGroup() *api.TaskGroup {
	name := "cache"
	count := 2
	cpu := 500
	memory := 256
	memoryMax := 512
	sidecarCPU := 100
	sidecarMemory := 64
	destination := "local/config.json"
	changeMode := "restart"
	templateBody := strings.Join([]string{
		`LOG_LEVEL=info`,
		`REDIS_PASSWORD=hunter2`,
		`{{ with secret "kv/redis" }}`,
		`DB_PASSWORD='{{ .Data.password }}'`,
		`{{ end }}`,
		`SSL_KEY_STORE_TYPE=PKCS12`,
	}, "\n")

	return &api.TaskGroup{
		Name:  &name,
		Count: &count,
		Tasks: []*api.Task{
			{
				Name:   "redis",
				Driver: "docker",
				Env: map[string]string{
					"REDIS_ADDR": "127.0.0.1:6379",
					// Adjacent to a sensitive word but not one: must stay readable.
					"TOKENEXCHANGE_AUDIENCE": "platform-engine",
					"apiKeyHeader":           "X-Api-Key: abcdef",
					"REDIS_PASSWORD":         "hunter2",
					"VAULT_TOKEN":            "${VAULT_TOKEN}",
				},
				Config: map[string]any{
					"image": "redis:7",
					"auth":  map[string]any{"username": "deploy", "password": "hunter2"},
				},
				Resources: &api.Resources{CPU: &cpu, MemoryMB: &memory, MemoryMaxMB: &memoryMax},
				Templates: []*api.Template{{DestPath: &destination, ChangeMode: &changeMode, EmbeddedTmpl: &templateBody}},
			},
			{
				Name:      "log-shipper",
				Driver:    "docker",
				Lifecycle: &api.TaskLifecycle{Hook: "prestart", Sidecar: true},
				Resources: &api.Resources{CPU: &sidecarCPU, MemoryMB: &sidecarMemory},
			},
		},
	}
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

// newTestSession wires an in-process MCP client to a server backed by fakeNomadClient
// and returns a connected client session plus a context bounded to the test.
func newTestSession(t *testing.T) (*mcp.ClientSession, context.Context) {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := New(fakeNomadClient{}, logger)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	must.NoError(t, err)
	t.Cleanup(func() { serverSession.Close() })

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	must.NoError(t, err)
	t.Cleanup(func() { clientSession.Close() })

	return clientSession, ctx
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

func TestGetNodeAllocationCountIsNumeric(t *testing.T) {
	t.Parallel()

	clientSession, ctx := newTestSession(t)

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "get_node", Arguments: map[string]any{"node_id": "node-1"}})
	must.NoError(t, err)
	must.False(t, result.IsError)

	structured, ok := result.StructuredContent.(map[string]any)
	must.True(t, ok)

	allocations, ok := structured["allocations"].(map[string]any)
	must.True(t, ok)

	// count must be the number of allocations, not the slice of IDs (regression guard).
	count, ok := allocations["count"].(float64)
	must.True(t, ok)
	must.Eq(t, float64(1), count)

	ids, ok := allocations["ids"].([]any)
	must.True(t, ok)
	must.Len(t, 1, ids)
}

func TestNodeStatusResourceHandlesNilReservedResources(t *testing.T) {
	t.Parallel()

	clientSession, ctx := newTestSession(t)

	// The fake node has NodeResources but a nil ReservedResources; reading its status
	// must not panic and must report reserved as zero with available == total.
	resource, err := clientSession.ReadResource(ctx, &mcp.ReadResourceParams{URI: "nomad://nodes/node-1/status"})
	must.NoError(t, err)
	must.Positive(t, len(resource.Contents))

	var payload struct {
		Node struct {
			Resources struct {
				CPUShares          int64 `json:"cpu_shares"`
				ReservedCPUShares  int64 `json:"reserved_cpu_shares"`
				AvailableCPUShares int64 `json:"available_cpu_shares"`
			} `json:"resources"`
		} `json:"node"`
	}
	must.NoError(t, json.Unmarshal([]byte(resource.Contents[0].Text), &payload))

	must.Eq(t, int64(4000), payload.Node.Resources.CPUShares)
	must.Eq(t, int64(0), payload.Node.Resources.ReservedCPUShares)
	must.Eq(t, int64(4000), payload.Node.Resources.AvailableCPUShares)
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

// numberAt reads a JSON number field, which always decodes as float64.
func numberAt(t *testing.T, source map[string]any, key string) float64 {
	t.Helper()
	value, ok := source[key].(float64)
	must.True(t, ok)
	return value
}

func stringAt(t *testing.T, source map[string]any, key string) string {
	t.Helper()
	value, ok := source[key].(string)
	must.True(t, ok)
	return value
}

// callInspectJob runs inspect_job and returns its structured output.
func callInspectJob(t *testing.T, clientSession *mcp.ClientSession, ctx context.Context, arguments map[string]any) map[string]any {
	t.Helper()

	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "inspect_job", Arguments: arguments})
	must.NoError(t, err)
	must.False(t, result.IsError)

	structured, ok := result.StructuredContent.(map[string]any)
	must.True(t, ok)
	return structured
}

func firstTaskGroup(t *testing.T, structured map[string]any) map[string]any {
	t.Helper()

	groups, ok := structured["task_groups"].([]any)
	must.True(t, ok)
	must.Len(t, 1, groups)

	group, ok := groups[0].(map[string]any)
	must.True(t, ok)
	return group
}

func taskByName(t *testing.T, group map[string]any, name string) map[string]any {
	t.Helper()

	tasks, ok := group["tasks"].([]any)
	must.True(t, ok)

	for _, candidate := range tasks {
		task, ok := candidate.(map[string]any)
		must.True(t, ok)
		if task["name"] == name {
			return task
		}
	}

	t.Fatalf("task %s not found in task group %v", name, group["name"])
	return nil
}

func TestInspectJobReportsResourceTotals(t *testing.T) {
	t.Parallel()

	clientSession, ctx := newTestSession(t)
	structured := callInspectJob(t, clientSession, ctx, map[string]any{"job_id": "example"})

	group := firstTaskGroup(t, structured)
	must.Eq(t, float64(2), numberAt(t, group, "count"))

	resources, ok := group["resources"].(map[string]any)
	must.True(t, ok)

	perAllocation, ok := resources["per_allocation"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(320), numberAt(t, perAllocation, "memory_mb"))
	// The sidecar has no memory_max, so its reservation is also its ceiling.
	must.Eq(t, float64(576), numberAt(t, perAllocation, "memory_max_mb"))

	desired, ok := resources["desired"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(640), numberAt(t, desired, "memory_mb"))
	must.Eq(t, float64(1152), numberAt(t, desired, "memory_max_mb"))

	totals, ok := structured["totals"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(2), numberAt(t, totals, "allocations"))
	must.Eq(t, float64(2), numberAt(t, totals, "tasks"))

	jobTotals, ok := totals["desired"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(640), numberAt(t, jobTotals, "memory_mb"))
	must.Eq(t, float64(1200), numberAt(t, jobTotals, "cpu_mhz"))

	redis := taskByName(t, group, "redis")
	taskResources, ok := redis["resources"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(512), numberAt(t, taskResources, "memory_limit_mb"))

	sidecar := taskByName(t, group, "log-shipper")
	sidecarResources, ok := sidecar["resources"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(0), numberAt(t, sidecarResources, "memory_max_mb"))
	must.Eq(t, float64(64), numberAt(t, sidecarResources, "memory_limit_mb"))
}

func TestInspectJobOmitsUnrequestedSectionsButCountsThem(t *testing.T) {
	t.Parallel()

	clientSession, ctx := newTestSession(t)
	structured := callInspectJob(t, clientSession, ctx, map[string]any{"job_id": "example"})

	redis := taskByName(t, firstTaskGroup(t, structured), "redis")

	_, hasEnv := redis["env"]
	must.False(t, hasEnv)
	_, hasConfig := redis["config"]
	must.False(t, hasConfig)

	// The counts are what tell a caller which section is worth requesting.
	declares, ok := redis["declares"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(5), numberAt(t, declares, "env"))
	must.Eq(t, float64(1), numberAt(t, declares, "templates"))

	redaction, ok := structured["redaction"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(0), numberAt(t, redaction, "values_redacted"))
}

func TestInspectJobRedactsSensitiveValues(t *testing.T) {
	t.Parallel()

	clientSession, ctx := newTestSession(t)
	structured := callInspectJob(t, clientSession, ctx, map[string]any{
		"job_id":   "example",
		"sections": []any{"env", "config"},
	})

	redis := taskByName(t, firstTaskGroup(t, structured), "redis")

	env, ok := redis["env"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, "<redacted>", stringAt(t, env, "REDIS_PASSWORD"))
	must.Eq(t, "127.0.0.1:6379", stringAt(t, env, "REDIS_ADDR"))
	must.Eq(t, "platform-engine", stringAt(t, env, "TOKENEXCHANGE_AUDIENCE"))
	// camelCase humps split the same way separators do.
	must.Eq(t, "<redacted>", stringAt(t, env, "apiKeyHeader"))
	// A pure interpolation names a secret without carrying one.
	must.Eq(t, "${VAULT_TOKEN}", stringAt(t, env, "VAULT_TOKEN"))

	config, ok := redis["config"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, "redis:7", stringAt(t, config, "image"))
	// The whole auth block goes, not just the password inside it.
	must.Eq(t, "<redacted>", stringAt(t, config, "auth"))

	redaction, ok := structured["redaction"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(3), numberAt(t, redaction, "values_redacted"))
}

func TestInspectJobRedactsTemplateBodiesLineByLine(t *testing.T) {
	t.Parallel()

	clientSession, ctx := newTestSession(t)
	structured := callInspectJob(t, clientSession, ctx, map[string]any{
		"job_id":   "example",
		"task":     "redis",
		"sections": []any{"template_bodies"},
	})

	templates, ok := taskByName(t, firstTaskGroup(t, structured), "redis")["templates"].([]any)
	must.True(t, ok)
	must.Len(t, 1, templates)

	template, ok := templates[0].(map[string]any)
	must.True(t, ok)
	must.True(t, template["reads_vault"].(bool))

	body := stringAt(t, template, "body")
	// Only the literal secret goes; the structure that explains where the real
	// values come from stays intact.
	must.StrContains(t, body, "LOG_LEVEL=info")
	must.StrContains(t, body, "REDIS_PASSWORD=<redacted>")
	must.StrContains(t, body, `{{ with secret "kv/redis" }}`)
	must.StrContains(t, body, `DB_PASSWORD='{{ .Data.password }}'`)
	// A key ending in a locator word names a format, not a secret.
	must.StrContains(t, body, "SSL_KEY_STORE_TYPE=PKCS12")
}

func TestInspectJobFiltersAndReportsUnknownSections(t *testing.T) {
	t.Parallel()

	clientSession, ctx := newTestSession(t)
	structured := callInspectJob(t, clientSession, ctx, map[string]any{
		"job_id":   "example",
		"task":     "log-shipper",
		"sections": []any{"lifecycle", "nonsense"},
	})

	group := firstTaskGroup(t, structured)
	tasks, ok := group["tasks"].([]any)
	must.True(t, ok)
	must.Len(t, 1, tasks)

	sidecar := taskByName(t, group, "log-shipper")
	lifecycle, ok := sidecar["lifecycle"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, "prestart", stringAt(t, lifecycle, "hook"))

	sections, ok := structured["sections"].(map[string]any)
	must.True(t, ok)

	unknown, ok := sections["unknown"].([]any)
	must.True(t, ok)
	must.Len(t, 1, unknown)
	must.Eq(t, "nonsense", unknown[0])
}

func TestInspectJobFilterOnMissingTaskGroupReturnsNothing(t *testing.T) {
	t.Parallel()

	clientSession, ctx := newTestSession(t)
	structured := callInspectJob(t, clientSession, ctx, map[string]any{
		"job_id":     "example",
		"task_group": "does-not-exist",
	})

	groups, ok := structured["task_groups"].([]any)
	must.True(t, ok)
	must.Len(t, 0, groups)

	totals, ok := structured["totals"].(map[string]any)
	must.True(t, ok)
	must.Eq(t, float64(0), numberAt(t, totals, "task_groups"))
}
