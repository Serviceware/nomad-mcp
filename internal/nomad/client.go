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
	// ReturnedLines is how many lines Text actually holds. It can be well below
	// AppliedLines when long lines exhaust the byte budget first.
	ReturnedLines int
	ReturnedBytes int
	// Truncated reports that Text is not the complete requested tail: either the
	// request exceeded maxLogTailLines, or the byte budget clipped the start of it.
	Truncated bool
	LogType   string
	TaskName  string
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

	// The byte budget is the hard cap on how much is read; appliedLines is enforced
	// afterwards by trimLogTail, since the API offsets by bytes, not by lines.
	byteBudget := int64(appliedLines) * int64(logTailBytesPerLine)

	frames, errCh := c.raw.AllocFS().Logs(
		alloc,
		false,
		taskName,
		logType,
		api.OriginEnd,
		byteBudget,
		cancel,
		withContext(query),
	)

	newTail := func(text string, clipped bool) *AllocationLogTail {
		return &AllocationLogTail{
			Text:           text,
			RequestedLines: lines,
			AppliedLines:   appliedLines,
			ReturnedLines:  countLines(text),
			ReturnedBytes:  len(text),
			Truncated:      truncated || clipped,
			LogType:        logType,
			TaskName:       taskName,
		}
	}

	if frames == nil {
		if err := <-errCh; err != nil {
			return nil, err
		}
		return newTail("", false), nil
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

	raw := builder.String()
	clipped := int64(len(raw)) >= byteBudget
	return newTail(trimLogTail(raw, appliedLines, clipped), clipped), nil
}

// trimLogTail bounds a tail that was read backwards from the end of a log file.
// A byte-offset read almost always lands mid-line, so when the budget was
// exhausted the leading partial line is dropped; the result is then trimmed to
// the last lines entries.
func trimLogTail(text string, lines int, clipped bool) string {
	if clipped {
		// With no newline at all the whole read is one oversized partial line;
		// keep it rather than returning nothing.
		if index := strings.IndexByte(text, '\n'); index >= 0 {
			text = text[index+1:]
		}
	}
	return lastLines(text, lines)
}

// countLines counts lines in text, including a final line with no trailing newline.
func countLines(text string) int {
	if text == "" {
		return 0
	}
	count := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		count++
	}
	return count
}

// lastLines returns the trailing n lines of text, preserving whether the input
// ended with a newline. A single trailing newline does not count as a line.
func lastLines(text string, n int) string {
	if text == "" || n <= 0 {
		return text
	}

	body := strings.TrimSuffix(text, "\n")
	parts := strings.Split(body, "\n")
	if len(parts) <= n {
		return text
	}

	trimmed := strings.Join(parts[len(parts)-n:], "\n")
	if strings.HasSuffix(text, "\n") {
		trimmed += "\n"
	}
	return trimmed
}

func withContext(query *api.QueryOptions) *api.QueryOptions {
	return query
}
