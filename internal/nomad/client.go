package nomad

import (
	"log/slog"
	"strings"

	"github.com/hashicorp/nomad/api"
)

const (
	defaultLogTailLines = 200
	maxLogTailLines     = 500
	logTailBytesPerLine = 120
)

type AllocationLogTail struct {
	Text           string
	RequestedLines int
	AppliedLines   int
	ReturnedBytes  int
	Truncated      bool
	LogType        string
	TaskName       string
}

type Facade interface {
	Close()
	Address() string
	Leader(region string) (string, error)
	Peers() ([]string, error)
	Regions() ([]string, error)
	ListNamespaces(query *api.QueryOptions) ([]*api.Namespace, *api.QueryMeta, error)
	ListNodes(query *api.QueryOptions) ([]*api.NodeListStub, *api.QueryMeta, error)
	GetNode(nodeID string, query *api.QueryOptions) (*api.Node, *api.QueryMeta, error)
	ListNodeAllocations(nodeID string, query *api.QueryOptions) ([]*api.Allocation, *api.QueryMeta, error)
	ListJobs(query *api.QueryOptions) ([]*api.JobListStub, *api.QueryMeta, error)
	GetJob(jobID string, query *api.QueryOptions) (*api.Job, *api.QueryMeta, error)
	GetJobScaleStatus(jobID string, query *api.QueryOptions) (*api.JobScaleStatusResponse, *api.QueryMeta, error)
	GetJobSummary(jobID string, query *api.QueryOptions) (*api.JobSummary, *api.QueryMeta, error)
	ListJobEvaluations(jobID string, query *api.QueryOptions) ([]*api.Evaluation, *api.QueryMeta, error)
	ListJobAllocations(jobID string, all bool, query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error)
	ListJobDeployments(jobID string, all bool, query *api.QueryOptions) ([]*api.Deployment, *api.QueryMeta, error)
	ListJobServices(jobID string, query *api.QueryOptions) ([]*api.ServiceRegistration, *api.QueryMeta, error)
	GetEvaluation(evalID string, query *api.QueryOptions) (*api.Evaluation, *api.QueryMeta, error)
	ListEvaluationAllocations(evalID string, query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error)
	ListDeployments(query *api.QueryOptions) ([]*api.Deployment, *api.QueryMeta, error)
	GetDeployment(deploymentID string, query *api.QueryOptions) (*api.Deployment, *api.QueryMeta, error)
	ListDeploymentAllocations(deploymentID string, query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error)
	ListAllocations(query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error)
	GetAllocation(allocationID string, query *api.QueryOptions) (*api.Allocation, *api.QueryMeta, error)
	GetAllocationChecks(allocationID string, query *api.QueryOptions) (api.AllocCheckStatuses, error)
	ListAllocationServices(allocationID string, query *api.QueryOptions) ([]*api.ServiceRegistration, *api.QueryMeta, error)
	GetAllocationLogs(allocationID string, taskName string, logType string, lines int, query *api.QueryOptions) (*AllocationLogTail, error)
}

type Client struct {
	raw *api.Client
}

func NewFromEnvironment() (*Client, error) {
	config := api.DefaultConfig()
	slog.Info("initializing Nomad client", "address", config.Address, "region", config.Region, "namespace", config.Namespace)

	raw, err := api.NewClient(config)
	if err != nil {
		slog.Error("failed to create Nomad client", "error", err)
		return nil, err
	}

	slog.Info("Nomad client initialized", "address", raw.Address())

	return &Client{raw: raw}, nil
}

func (c *Client) Close() {
	if c == nil || c.raw == nil {
		return
	}
	slog.Info("closing Nomad client", "address", c.raw.Address())
	c.raw.Close()
}

func (c *Client) Address() string {
	return c.raw.Address()
}

func (c *Client) Leader(region string) (string, error) {
	if region == "" {
		return c.raw.Status().Leader()
	}
	return c.raw.Status().RegionLeader(region)
}

func (c *Client) Peers() ([]string, error) {
	return c.raw.Status().Peers()
}

func (c *Client) Regions() ([]string, error) {
	return c.raw.Regions().List()
}

func (c *Client) ListNamespaces(query *api.QueryOptions) ([]*api.Namespace, *api.QueryMeta, error) {
	return c.raw.Namespaces().List(withContext(query))
}

func (c *Client) ListNodes(query *api.QueryOptions) ([]*api.NodeListStub, *api.QueryMeta, error) {
	return c.raw.Nodes().List(withContext(query))
}

func (c *Client) GetNode(nodeID string, query *api.QueryOptions) (*api.Node, *api.QueryMeta, error) {
	return c.raw.Nodes().Info(nodeID, withContext(query))
}

func (c *Client) ListNodeAllocations(nodeID string, query *api.QueryOptions) ([]*api.Allocation, *api.QueryMeta, error) {
	return c.raw.Nodes().Allocations(nodeID, withContext(query))
}

func (c *Client) ListJobs(query *api.QueryOptions) ([]*api.JobListStub, *api.QueryMeta, error) {
	return c.raw.Jobs().List(withContext(query))
}

func (c *Client) GetJob(jobID string, query *api.QueryOptions) (*api.Job, *api.QueryMeta, error) {
	return c.raw.Jobs().Info(jobID, withContext(query))
}

func (c *Client) GetJobScaleStatus(jobID string, query *api.QueryOptions) (*api.JobScaleStatusResponse, *api.QueryMeta, error) {
	return c.raw.Jobs().ScaleStatus(jobID, withContext(query))
}

func (c *Client) GetJobSummary(jobID string, query *api.QueryOptions) (*api.JobSummary, *api.QueryMeta, error) {
	return c.raw.Jobs().Summary(jobID, withContext(query))
}

func (c *Client) ListJobEvaluations(jobID string, query *api.QueryOptions) ([]*api.Evaluation, *api.QueryMeta, error) {
	return c.raw.Jobs().Evaluations(jobID, withContext(query))
}

func (c *Client) ListJobAllocations(jobID string, all bool, query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error) {
	return c.raw.Jobs().Allocations(jobID, all, withContext(query))
}

func (c *Client) ListJobDeployments(jobID string, all bool, query *api.QueryOptions) ([]*api.Deployment, *api.QueryMeta, error) {
	return c.raw.Jobs().Deployments(jobID, all, withContext(query))
}

func (c *Client) ListJobServices(jobID string, query *api.QueryOptions) ([]*api.ServiceRegistration, *api.QueryMeta, error) {
	return c.raw.Jobs().Services(jobID, withContext(query))
}

func (c *Client) GetEvaluation(evalID string, query *api.QueryOptions) (*api.Evaluation, *api.QueryMeta, error) {
	return c.raw.Evaluations().Info(evalID, withContext(query))
}

func (c *Client) ListEvaluationAllocations(evalID string, query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error) {
	return c.raw.Evaluations().Allocations(evalID, withContext(query))
}

func (c *Client) ListDeployments(query *api.QueryOptions) ([]*api.Deployment, *api.QueryMeta, error) {
	return c.raw.Deployments().List(withContext(query))
}

func (c *Client) GetDeployment(deploymentID string, query *api.QueryOptions) (*api.Deployment, *api.QueryMeta, error) {
	return c.raw.Deployments().Info(deploymentID, withContext(query))
}

func (c *Client) ListDeploymentAllocations(deploymentID string, query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error) {
	return c.raw.Deployments().Allocations(deploymentID, withContext(query))
}

func (c *Client) ListAllocations(query *api.QueryOptions) ([]*api.AllocationListStub, *api.QueryMeta, error) {
	return c.raw.Allocations().List(withContext(query))
}

func (c *Client) GetAllocation(allocationID string, query *api.QueryOptions) (*api.Allocation, *api.QueryMeta, error) {
	return c.raw.Allocations().Info(allocationID, withContext(query))
}

func (c *Client) GetAllocationChecks(allocationID string, query *api.QueryOptions) (api.AllocCheckStatuses, error) {
	return c.raw.Allocations().Checks(allocationID, withContext(query))
}

func (c *Client) ListAllocationServices(allocationID string, query *api.QueryOptions) ([]*api.ServiceRegistration, *api.QueryMeta, error) {
	return c.raw.Allocations().Services(allocationID, withContext(query))
}

func (c *Client) GetAllocationLogs(allocationID string, taskName string, logType string, lines int, query *api.QueryOptions) (*AllocationLogTail, error) {
	if lines <= 0 {
		lines = defaultLogTailLines
	}

	appliedLines := lines
	truncated := false
	if appliedLines > maxLogTailLines {
		appliedLines = maxLogTailLines
		truncated = true
	}

	if logType != api.FSLogNameStdout && logType != api.FSLogNameStderr {
		logType = api.FSLogNameStderr
	}

	alloc, _, err := c.GetAllocation(allocationID, query)
	if err != nil {
		return nil, err
	}

	cancel := make(chan struct{})
	defer close(cancel)

	frames, errCh := c.raw.AllocFS().Logs(
		alloc,
		false,
		taskName,
		logType,
		api.OriginEnd,
		int64(appliedLines)*int64(logTailBytesPerLine),
		cancel,
		withContext(query),
	)
	if frames == nil {
		if err := <-errCh; err != nil {
			return nil, err
		}
		return nil, nil
	}

	var builder strings.Builder
	for frame := range frames {
		if frame == nil {
			continue
		}
		builder.Write(frame.Data)
	}

	select {
	case err := <-errCh:
		if err != nil {
			return nil, err
		}
	default:
	}

	text := builder.String()
	return &AllocationLogTail{
		Text:           text,
		RequestedLines: lines,
		AppliedLines:   appliedLines,
		ReturnedBytes:  len(text),
		Truncated:      truncated,
		LogType:        logType,
		TaskName:       taskName,
	}, nil
}

func withContext(query *api.QueryOptions) *api.QueryOptions {
	return query
}
