package tools

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/hashicorp/nomad/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listQueryInput struct {
	Namespace  string `json:"namespace,omitempty" jsonschema:"override namespace; use * to query all namespaces when permitted"`
	Region     string `json:"region,omitempty" jsonschema:"override region for the request"`
	Prefix     string `json:"prefix,omitempty" jsonschema:"optional server-side prefix filter"`
	Filter     string `json:"filter,omitempty" jsonschema:"optional Nomad filter expression; for example Name matches \"financial\" or Meta.department == \"financial\""`
	PerPage    int32  `json:"per_page,omitempty" jsonschema:"optional page size for supported list endpoints"`
	NextToken  string `json:"next_token,omitempty" jsonschema:"pagination token from a previous response"`
	Reverse    bool   `json:"reverse,omitempty" jsonschema:"reverse supported list ordering when the endpoint allows it"`
	AllowStale bool   `json:"allow_stale,omitempty" jsonschema:"allow stale reads from non-leader servers"`
}

type objectQueryInput struct {
	Namespace  string `json:"namespace,omitempty" jsonschema:"override namespace for the request"`
	Region     string `json:"region,omitempty" jsonschema:"override region for the request"`
	AllowStale bool   `json:"allow_stale,omitempty" jsonschema:"allow stale reads from non-leader servers"`
}

func (q listQueryInput) queryOptions() *api.QueryOptions {
	return &api.QueryOptions{
		Namespace:  q.Namespace,
		Region:     q.Region,
		Prefix:     q.Prefix,
		Filter:     q.Filter,
		PerPage:    q.PerPage,
		NextToken:  q.NextToken,
		Reverse:    q.Reverse,
		AllowStale: q.AllowStale,
	}
}

func (q objectQueryInput) queryOptions() *api.QueryOptions {
	return &api.QueryOptions{
		Namespace:  q.Namespace,
		Region:     q.Region,
		AllowStale: q.AllowStale,
	}
}

func addTool[In, Out any](server *mcp.Server, tool *mcp.Tool, handler mcp.ToolHandlerFor[In, Out]) {
	tool.InputSchema = mustToolInputSchema[In]()
	mcp.AddTool(server, tool, handler)
}

func mustToolInputSchema[T any]() map[string]any {
	schema, err := jsonschema.ForType(reflect.TypeFor[T](), &jsonschema.ForOptions{})
	if err != nil {
		panic(fmt.Errorf("build input schema for %s: %w", reflect.TypeFor[T](), err))
	}

	ensureObjectProperties(schema)

	encoded, err := json.Marshal(schema)
	if err != nil {
		panic(fmt.Errorf("marshal input schema for %s: %w", reflect.TypeFor[T](), err))
	}

	var result map[string]any
	if err := json.Unmarshal(encoded, &result); err != nil {
		panic(fmt.Errorf("unmarshal input schema for %s: %w", reflect.TypeFor[T](), err))
	}

	if result["properties"] == nil && isObjectSchema(result) {
		result["properties"] = map[string]any{}
	}

	return result
}

func ensureObjectProperties(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}

	if schema.Properties == nil && (schema.Type == "object" || slicesContains(schema.Types, "object")) {
		schema.Properties = map[string]*jsonschema.Schema{}
	}

	for _, property := range schema.Properties {
		ensureObjectProperties(property)
	}

	for _, item := range schema.PrefixItems {
		ensureObjectProperties(item)
	}

	for _, nested := range []*jsonschema.Schema{schema.Items, schema.AdditionalProperties, schema.PropertyNames, schema.UnevaluatedProperties, schema.Contains, schema.UnevaluatedItems, schema.Not, schema.If, schema.Then, schema.Else, schema.ContentSchema} {
		ensureObjectProperties(nested)
	}

	for _, nested := range schema.ItemsArray {
		ensureObjectProperties(nested)
	}

	for _, nested := range schema.PatternProperties {
		ensureObjectProperties(nested)
	}

	for _, nested := range schema.Defs {
		ensureObjectProperties(nested)
	}

	for _, nested := range schema.Definitions {
		ensureObjectProperties(nested)
	}

	for _, nested := range schema.DependencySchemas {
		ensureObjectProperties(nested)
	}

	for _, nested := range schema.DependentSchemas {
		ensureObjectProperties(nested)
	}

	for _, nested := range schema.AllOf {
		ensureObjectProperties(nested)
	}

	for _, nested := range schema.AnyOf {
		ensureObjectProperties(nested)
	}

	for _, nested := range schema.OneOf {
		ensureObjectProperties(nested)
	}
}

func isObjectSchema(schema map[string]any) bool {
	if schema["type"] == "object" {
		return true
	}

	types, ok := schema["type"].([]any)
	if !ok {
		return false
	}

	for _, candidate := range types {
		if candidate == "object" {
			return true
		}
	}

	return false
}

func slicesContains[T comparable](items []T, target T) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func okResult(summary string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: summary}},
	}
}

func failResult(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: userFacingError(err)}},
	}
}

func userFacingError(err error) string {
	if err == nil {
		return "unknown error"
	}

	message := err.Error()
	switch {
	case strings.Contains(message, api.PermissionDeniedErrorContent):
		return "Nomad rejected the request due to insufficient ACL permissions."
	case strings.Contains(strings.ToLower(message), "not found"):
		return message
	default:
		return message
	}
}

func metaMap(queryMeta *api.QueryMeta) map[string]any {
	if queryMeta == nil {
		return map[string]any{}
	}

	return map[string]any{
		"last_index":   queryMeta.LastIndex,
		"known_leader": queryMeta.KnownLeader,
		"last_contact": queryMeta.LastContact.String(),
		"request_time": queryMeta.RequestTime.String(),
		"next_token":   queryMeta.NextToken,
	}
}

func stringMap(items map[string]string) map[string]string {
	if len(items) == 0 {
		return map[string]string{}
	}

	clone := make(map[string]string, len(items))
	for key, value := range items {
		clone[key] = value
	}

	return clone
}

func metadataSummary(items map[string]string) string {
	if len(items) == 0 {
		return ""
	}

	keys := sortedKeys(items)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, items[key]))
	}

	return strings.Join(parts, ", ")
}

func formatSubmitTime(nanos int64) string {
	if nanos <= 0 {
		return ""
	}
	return time.Unix(0, nanos).UTC().Format(time.RFC3339)
}

func formatUnixNanos(nanos int64) string {
	if nanos <= 0 {
		return ""
	}
	return time.Unix(0, nanos).UTC().Format(time.RFC3339)
}

func formatUnixSeconds(seconds int64) string {
	if seconds <= 0 {
		return ""
	}
	return time.Unix(seconds, 0).UTC().Format(time.RFC3339)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func derefUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func derefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func sortedKeys[K ~string, V any](items map[K]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, string(key))
	}
	sort.Strings(keys)
	return keys
}

func summarizeList(kind string, count int, queryMeta *api.QueryMeta) string {
	if queryMeta != nil && queryMeta.NextToken != "" {
		return fmt.Sprintf("Returned %d %s. More results are available with next_token=%s.", count, kind, queryMeta.NextToken)
	}
	return fmt.Sprintf("Returned %d %s.", count, kind)
}

func taskGroupsMap(groups []*api.TaskGroup) []map[string]any {
	items := make([]map[string]any, 0, len(groups))
	for _, tg := range groups {
		if tg == nil {
			continue
		}
		items = append(items, map[string]any{
			"name":     derefString(tg.Name),
			"networks": networksMap(tg.Networks),
			"tasks":    tasksMap(tg.Tasks),
		})
	}
	return items
}

func networksMap(networks []*api.NetworkResource) []map[string]any {
	items := make([]map[string]any, 0, len(networks))
	for _, n := range networks {
		if n == nil {
			continue
		}
		items = append(items, map[string]any{
			"mode":           n.Mode,
			"reserved_ports": portsMap(n.ReservedPorts),
			"dynamic_ports":  portsMap(n.DynamicPorts),
		})
	}
	return items
}

func portsMap(ports []api.Port) []map[string]any {
	items := make([]map[string]any, 0, len(ports))
	for _, p := range ports {
		items = append(items, map[string]any{
			"label": p.Label,
			"value": p.Value,
			"to":    p.To,
		})
	}
	return items
}

func tasksMap(tasks []*api.Task) []map[string]any {
	items := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		item := map[string]any{
			"name":   t.Name,
			"driver": t.Driver,
		}
		if image, ok := t.Config["image"]; ok {
			item["image"] = image
		}
		items = append(items, item)
	}
	return items
}

func serviceRegistrationMap(service *api.ServiceRegistration) map[string]any {
	if service == nil {
		return map[string]any{}
	}

	return map[string]any{
		"id":           service.ID,
		"service_name": service.ServiceName,
		"namespace":    service.Namespace,
		"job_id":       service.JobID,
		"alloc_id":     service.AllocID,
		"node_id":      service.NodeID,
		"datacenter":   service.Datacenter,
		"address":      service.Address,
		"port":         service.Port,
		"tags":         service.Tags,
	}
}
