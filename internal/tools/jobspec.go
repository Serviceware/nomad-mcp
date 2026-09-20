package tools

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/hashicorp/nomad/api"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	client "github.com/serviceware/nomad-mcp/internal/nomad"
)

// Sections selectable through inspect_job. The base skeleton — job identity,
// task groups, tasks, and their resource requests — is always returned; every
// section adds one more slice of the submitted specification on top of it.
const (
	sectionScheduling     = "scheduling"
	sectionNetworking     = "networking"
	sectionStorage        = "storage"
	sectionEnv            = "env"
	sectionConfig         = "config"
	sectionTemplates      = "templates"
	sectionTemplateBodies = "template_bodies"
	sectionArtifacts      = "artifacts"
	sectionIdentity       = "identity"
	sectionLifecycle      = "lifecycle"
	sectionMeta           = "meta"
	sectionAll            = "all"
)

var inspectJobSections = []string{
	sectionScheduling,
	sectionNetworking,
	sectionStorage,
	sectionEnv,
	sectionConfig,
	sectionTemplates,
	sectionTemplateBodies,
	sectionArtifacts,
	sectionIdentity,
	sectionLifecycle,
	sectionMeta,
}

// redactedValue replaces a value whose key marks it as sensitive. The key stays
// visible so the shape of the configuration is still answerable.
const redactedValue = "<redacted>"

// sensitiveKeyWords redact a value when they appear as a whole word in its key.
// Whole-word matching is deliberate: TOKENEXCHANGE_AUDIENCES and KEYCLOAK_URL are
// configuration, not secrets, and redacting them hides the answer to ordinary
// questions about the spec.
var sensitiveKeyWords = map[string]struct{}{
	"accesskey":   {},
	"apikey":      {},
	"auth":        {},
	"credential":  {},
	"credentials": {},
	"key":         {},
	"keys":        {},
	"pass":        {},
	"passphrase":  {},
	"passwd":      {},
	"password":    {},
	"private":     {},
	"privatekey":  {},
	"salt":        {},
	"secret":      {},
	"secrets":     {},
	"seed":        {},
	"signingkey":  {},
	"token":       {},
	"tokens":      {},
}

// locatorKeySuffixes end a key that points at secret material rather than
// holding it: SSL_KEY_STORE_TYPE is "PKCS12" and TOKEN_URL is an endpoint, while
// SSL_KEY_STORE_PASSWORD still redacts on its final word.
var locatorKeySuffixes = map[string]struct{}{
	"alias":     {},
	"dir":       {},
	"directory": {},
	"enabled":   {},
	"file":      {},
	"format":    {},
	"host":      {},
	"name":      {},
	"path":      {},
	"paths":     {},
	"port":      {},
	"store":     {},
	"type":      {},
	"uri":       {},
	"url":       {},
}

type sectionSet map[string]struct{}

func (s sectionSet) has(name string) bool {
	if _, ok := s[sectionAll]; ok {
		return true
	}
	_, ok := s[name]
	return ok
}

func (s sectionSet) names() []string {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// parseSections splits the requested sections into the recognized set and the
// leftovers, which are reported back so a typo does not silently return less.
func parseSections(requested []string) (sectionSet, []string) {
	known := make(map[string]struct{}, len(inspectJobSections)+1)
	known[sectionAll] = struct{}{}
	for _, section := range inspectJobSections {
		known[section] = struct{}{}
	}

	selected := sectionSet{}
	unknown := make([]string, 0)
	for _, candidate := range requested {
		normalized := strings.ToLower(strings.TrimSpace(candidate))
		if normalized == "" {
			continue
		}
		if _, ok := known[normalized]; !ok {
			unknown = append(unknown, candidate)
			continue
		}
		selected[normalized] = struct{}{}
	}

	// template_bodies only makes sense alongside the template metadata.
	if selected.has(sectionTemplateBodies) {
		selected[sectionTemplates] = struct{}{}
	}

	return selected, unknown
}

// redactor rewrites values whose key looks sensitive and counts how often it
// did, so the caller can tell "no secrets here" from "secrets hidden".
type redactor struct {
	count int
}

func (r *redactor) shouldRedact(key string, value string) bool {
	if !isSensitiveKey(key) {
		return false
	}
	// A pure ${NOMAD_*} or {{ ... }} reference carries no secret material and is
	// usually the most interesting part of the line, so it stays readable.
	return !isInterpolationOnly(value)
}

func (r *redactor) stringValue(key string, value string) string {
	if !r.shouldRedact(key, value) {
		return value
	}
	r.count++
	return redactedValue
}

func (r *redactor) stringMap(items map[string]string) map[string]string {
	if len(items) == 0 {
		return map[string]string{}
	}

	clone := make(map[string]string, len(items))
	for key, value := range items {
		clone[key] = r.stringValue(key, value)
	}
	return clone
}

// assignmentPattern matches a KEY=value or "key": value line inside a template
// body, so only the right-hand side has to give way to redaction.
var assignmentPattern = regexp.MustCompile(`^(\s*(?:export\s+)?"?)([A-Za-z_][A-Za-z0-9_.\-]*)("?\s*[:=]\s*)(.+?)(,?\s*)$`)

// vaultTemplatePattern matches a consul-template call to Vault, which is what
// makes a template worth flagging: the spec holds the path, never the value.
var vaultTemplatePattern = regexp.MustCompile(`\{\{[^}]*\bsecret[\s("]`)

// templateBody redacts assignment values under sensitive keys line by line.
// Redacting the whole body would hide the Vault paths and rendering logic, which
// is the part of a template an operator actually needs to read.
func (r *redactor) templateBody(body string) string {
	if body == "" {
		return body
	}

	lines := strings.Split(body, "\n")
	for index, line := range lines {
		match := assignmentPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if !r.shouldRedact(match[2], match[4]) {
			continue
		}
		r.count++
		lines[index] = match[1] + match[2] + match[3] + redactedValue + match[5]
	}

	return strings.Join(lines, "\n")
}

// anyValue walks driver config, which is an untyped tree, and redacts whole
// subtrees under a sensitive key (a docker `auth` block, for example).
func (r *redactor) anyValue(key string, value any) any {
	if isSensitiveKey(key) {
		if text, ok := value.(string); ok && isInterpolationOnly(text) {
			return text
		}
		r.count++
		return redactedValue
	}

	switch typed := value.(type) {
	case map[string]any:
		clone := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			clone[nestedKey] = r.anyValue(nestedKey, nestedValue)
		}
		return clone
	case []any:
		clone := make([]any, 0, len(typed))
		for _, item := range typed {
			// Items inherit the key of the slice they live in.
			clone = append(clone, r.anyValue(key, item))
		}
		return clone
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	tokens := keyTokens(key)
	if len(tokens) == 0 {
		return false
	}

	if _, ok := locatorKeySuffixes[tokens[len(tokens)-1]]; ok {
		return false
	}

	for _, token := range tokens {
		if _, ok := sensitiveKeyWords[token]; ok {
			return true
		}
	}
	return false
}

// keyTokens splits a configuration key into lowercase words, understanding both
// SNAKE_CASE separators and camelCase humps, so API_KEY, api-key and apiKey all
// yield the same tokens.
func keyTokens(key string) []string {
	runes := []rune(key)
	tokens := make([]string, 0, 4)

	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, strings.ToLower(current.String()))
			current.Reset()
		}
	}

	for index, symbol := range runes {
		switch {
		case !isAlphanumeric(symbol):
			flush()
		case unicode.IsUpper(symbol):
			if index > 0 && isAlphanumeric(runes[index-1]) && startsNewWord(runes, index) {
				flush()
			}
			current.WriteRune(unicode.ToLower(symbol))
		default:
			current.WriteRune(symbol)
		}
	}
	flush()

	return tokens
}

// startsNewWord decides whether an uppercase rune opens a new camelCase word:
// after a lowercase or digit (apiKey), or as the last capital of an acronym run
// that runs into a lowercase word (JSONKey).
func startsNewWord(runes []rune, index int) bool {
	previous := runes[index-1]
	if unicode.IsLower(previous) || unicode.IsDigit(previous) {
		return true
	}
	return unicode.IsUpper(previous) && index+1 < len(runes) && unicode.IsLower(runes[index+1])
}

func isAlphanumeric(symbol rune) bool {
	return unicode.IsLetter(symbol) || unicode.IsDigit(symbol)
}

// isInterpolationOnly reports whether the value is nothing but ${...} and
// {{...}} references — a pointer to a value rather than the value itself.
func isInterpolationOnly(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true
	}

	remainder := stripDelimited(trimmed, "${", "}")
	remainder = stripDelimited(remainder, "{{", "}}")

	// Quoting an interpolation ('{{ .Data.password }}') does not make it a
	// literal, so the quotes left behind do not count as remaining content.
	remainder = strings.Map(func(symbol rune) rune {
		if symbol == '\'' || symbol == '"' {
			return -1
		}
		return symbol
	}, remainder)

	return strings.TrimSpace(remainder) == ""
}

func stripDelimited(value string, open string, close string) string {
	var builder strings.Builder
	for {
		start := strings.Index(value, open)
		if start < 0 {
			builder.WriteString(value)
			return builder.String()
		}
		end := strings.Index(value[start+len(open):], close)
		if end < 0 {
			builder.WriteString(value)
			return builder.String()
		}
		builder.WriteString(value[:start])
		value = value[start+len(open)+end+len(close):]
	}
}

// resourceTotals is the summed resource request of a set of tasks.
type resourceTotals struct {
	CPUMHz      int
	Cores       int
	MemoryMB    int
	MemoryMaxMB int
	DiskMB      int
	SecretsMB   int
}

func (t *resourceTotals) add(resources *api.Resources) {
	if resources == nil {
		return
	}

	memory := derefInt(resources.MemoryMB)
	memoryMax := derefInt(resources.MemoryMaxMB)
	if memoryMax <= 0 {
		// An unset memory_max means the task may not exceed its reservation.
		memoryMax = memory
	}

	t.CPUMHz += derefInt(resources.CPU)
	t.Cores += derefInt(resources.Cores)
	t.MemoryMB += memory
	t.MemoryMaxMB += memoryMax
	t.DiskMB += derefInt(resources.DiskMB)
	t.SecretsMB += derefInt(resources.SecretsMB)
}

func (t resourceTotals) scaled(factor int) resourceTotals {
	if factor < 0 {
		factor = 0
	}
	return resourceTotals{
		CPUMHz:      t.CPUMHz * factor,
		Cores:       t.Cores * factor,
		MemoryMB:    t.MemoryMB * factor,
		MemoryMaxMB: t.MemoryMaxMB * factor,
		DiskMB:      t.DiskMB * factor,
		SecretsMB:   t.SecretsMB * factor,
	}
}

func (t *resourceTotals) addTotals(other resourceTotals) {
	t.CPUMHz += other.CPUMHz
	t.Cores += other.Cores
	t.MemoryMB += other.MemoryMB
	t.MemoryMaxMB += other.MemoryMaxMB
	t.DiskMB += other.DiskMB
	t.SecretsMB += other.SecretsMB
}

func (t resourceTotals) asMap() map[string]any {
	return map[string]any{
		"cpu_mhz":       t.CPUMHz,
		"cores":         t.Cores,
		"memory_mb":     t.MemoryMB,
		"memory_max_mb": t.MemoryMaxMB,
		"disk_mb":       t.DiskMB,
		"secrets_mb":    t.SecretsMB,
	}
}

type inspectJobInput struct {
	JobID     string   `json:"job_id" jsonschema:"full Nomad job ID"`
	TaskGroup string   `json:"task_group,omitempty" jsonschema:"only inspect this task group"`
	Task      string   `json:"task,omitempty" jsonschema:"only inspect this task; task groups without it are dropped"`
	Sections  []string `json:"sections,omitempty" jsonschema:"extra spec sections to include: scheduling, networking, storage, env, config, templates, template_bodies, artifacts, identity, lifecycle, meta, or all. Job, task group, task and resource details are always returned"`
	objectQueryInput
}

func registerJobSpecTools(server *mcp.Server, nomadClient client.Facade) {
	addTool(server, &mcp.Tool{
		Name: "inspect_job",
		Description: "Inspect the submitted specification of a Nomad job: the equivalent of `nomad job inspect`, projected into a navigable shape. " +
			"Answers questions about what a job declares rather than how it is currently running — resource requests (cpu, memory, memory_max, disk), " +
			"constraints, update and restart strategy, networks, ports, services and checks, volumes, environment variables, driver config, templates, " +
			"artifacts, Vault and workload identity, and per-group and per-job resource totals. " +
			"Narrow large jobs with task_group/task, and request only the sections you need; values under sensitive keys are redacted.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input inspectJobInput) (*mcp.CallToolResult, map[string]any, error) {
		sections, unknownSections := parseSections(input.Sections)

		job, queryMeta, err := nomadClient.GetJob(input.JobID, input.objectQueryInput.queryOptions().WithContext(ctx))
		if err != nil {
			return failResult(err), nil, nil
		}
		if job == nil {
			return failResult(fmt.Errorf("job %s not found", input.JobID)), nil, nil
		}

		redact := &redactor{}

		groups := make([]map[string]any, 0, len(job.TaskGroups))
		jobTotals := resourceTotals{}
		matchedGroups := 0
		matchedTasks := 0
		totalAllocations := 0

		for _, group := range job.TaskGroups {
			if group == nil {
				continue
			}
			groupName := derefString(group.Name)
			if input.TaskGroup != "" && groupName != input.TaskGroup {
				continue
			}

			tasks := make([]map[string]any, 0, len(group.Tasks))
			perAllocation := resourceTotals{}
			for _, task := range group.Tasks {
				if task == nil {
					continue
				}
				if input.Task != "" && task.Name != input.Task {
					continue
				}
				perAllocation.add(task.Resources)
				tasks = append(tasks, taskMap(task, sections, redact))
			}

			if input.Task != "" && len(tasks) == 0 {
				continue
			}

			count := derefInt(group.Count)
			desired := perAllocation.scaled(count)
			jobTotals.addTotals(desired)
			matchedGroups++
			matchedTasks += len(tasks)
			totalAllocations += count

			groups = append(groups, taskGroupMap(group, groupName, count, tasks, perAllocation, desired, sections, redact))
		}

		output := map[string]any{
			"job":         jobMap(job, sections, len(groups), matchedTasks),
			"task_groups": groups,
			"totals": map[string]any{
				"task_groups": matchedGroups,
				"tasks":       matchedTasks,
				"allocations": totalAllocations,
				"desired":     jobTotals.asMap(),
			},
			"sections": map[string]any{
				"requested": sections.names(),
				"available": inspectJobSections,
				"unknown":   unknownSections,
			},
			"redaction": map[string]any{
				"values_redacted": redact.count,
				"policy":          "values under sensitive-looking keys are replaced with " + redactedValue + "; keys and pure ${...}/{{...}} references are preserved",
			},
			"meta": metaMap(queryMeta),
		}

		return okResult(inspectJobSummary(job, matchedGroups, matchedTasks, totalAllocations, jobTotals, redact.count, unknownSections)), output, nil
	})
}

func inspectJobSummary(job *api.Job, groupCount int, taskCount int, allocations int, totals resourceTotals, redacted int, unknownSections []string) string {
	var builder strings.Builder

	fmt.Fprintf(&builder, "Job %s (%s, %s) version %d: %d task group(s), %d task(s), %d desired allocation(s). ",
		derefString(job.ID), derefString(job.Type), derefString(job.Status), derefUint64(job.Version), groupCount, taskCount, allocations)
	fmt.Fprintf(&builder, "Declared totals across all allocations: %d MHz CPU, %d MiB memory (%d MiB with memory_max), %d MiB disk.",
		totals.CPUMHz, totals.MemoryMB, totals.MemoryMaxMB, totals.DiskMB)

	if redacted > 0 {
		fmt.Fprintf(&builder, " %d value(s) redacted.", redacted)
	}
	if len(unknownSections) > 0 {
		fmt.Fprintf(&builder, " Ignored unknown section(s): %s. Available: %s.",
			strings.Join(unknownSections, ", "), strings.Join(inspectJobSections, ", "))
	}

	return builder.String()
}

func jobMap(job *api.Job, sections sectionSet, groupCount int, taskCount int) map[string]any {
	result := map[string]any{
		"id":                 derefString(job.ID),
		"name":               derefString(job.Name),
		"namespace":          derefString(job.Namespace),
		"region":             derefString(job.Region),
		"type":               derefString(job.Type),
		"status":             derefString(job.Status),
		"status_description": derefString(job.StatusDescription),
		"priority":           derefInt(job.Priority),
		"version":            derefUint64(job.Version),
		"stable":             derefBool(job.Stable),
		"stop":               derefBool(job.Stop),
		"submit_time":        formatSubmitTime(derefInt64(job.SubmitTime)),
		"datacenters":        job.Datacenters,
		"node_pool":          derefString(job.NodePool),
		"all_at_once":        derefBool(job.AllAtOnce),
		"dispatched":         job.Dispatched,
		"parent_id":          derefString(job.ParentID),
		"task_group_count":   groupCount,
		"task_count":         taskCount,
	}

	if sections.has(sectionMeta) {
		result["meta"] = stringMap(job.Meta)
	} else {
		result["meta_summary"] = metadataSummary(job.Meta)
	}

	if sections.has(sectionScheduling) {
		result["constraints"] = constraintList(job.Constraints)
		result["affinities"] = affinityList(job.Affinities)
		result["spreads"] = spreadList(job.Spreads)
		result["update"] = updateStrategyMap(job.Update)
		result["migrate"] = migrateStrategyMap(job.Migrate)
		result["reschedule"] = reschedulePolicyMap(job.Reschedule)
		result["periodic"] = periodicMap(job.Periodic)
		result["parameterized"] = parameterizedMap(job.ParameterizedJob)
		result["multiregion"] = multiregionMap(job.Multiregion)
	}

	if sections.has(sectionIdentity) {
		result["consul_namespace"] = job.ConsulNamespace
		result["vault_namespace"] = job.VaultNamespace
		result["has_nomad_token"] = derefString(job.NomadTokenID) != ""
	}

	return result
}

func taskGroupMap(group *api.TaskGroup, name string, count int, tasks []map[string]any, perAllocation resourceTotals, desired resourceTotals, sections sectionSet, redact *redactor) map[string]any {
	result := map[string]any{
		"name":       name,
		"count":      count,
		"task_count": len(tasks),
		"tasks":      tasks,
		"resources": map[string]any{
			"per_allocation": perAllocation.asMap(),
			"desired":        desired.asMap(),
		},
	}

	if group.EphemeralDisk != nil {
		result["ephemeral_disk_mb"] = derefInt(group.EphemeralDisk.SizeMB)
	}

	if sections.has(sectionMeta) {
		result["meta"] = stringMap(group.Meta)
	}

	if sections.has(sectionScheduling) {
		result["constraints"] = constraintList(group.Constraints)
		result["affinities"] = affinityList(group.Affinities)
		result["spreads"] = spreadList(group.Spreads)
		result["restart_policy"] = restartPolicyMap(group.RestartPolicy)
		result["reschedule_policy"] = reschedulePolicyMap(group.ReschedulePolicy)
		result["update"] = updateStrategyMap(group.Update)
		result["migrate"] = migrateStrategyMap(group.Migrate)
		result["disconnect"] = disconnectMap(group.Disconnect)
		result["shutdown_delay"] = durationString(group.ShutdownDelay)
		result["scaling"] = scalingPolicyMap(group.Scaling)
	}

	if sections.has(sectionNetworking) {
		result["networks"] = networkList(group.Networks)
		result["services"] = serviceList(group.Services)
	}

	if sections.has(sectionStorage) {
		result["ephemeral_disk"] = ephemeralDiskMap(group.EphemeralDisk)
		result["volumes"] = volumeRequestList(group.Volumes)
	}

	if sections.has(sectionIdentity) {
		result["consul"] = consulMap(group.Consul)
	}

	return result
}

func taskMap(task *api.Task, sections sectionSet, redact *redactor) map[string]any {
	result := map[string]any{
		"name":      task.Name,
		"driver":    task.Driver,
		"user":      task.User,
		"leader":    task.Leader,
		"kind":      task.Kind,
		"lifecycle": lifecycleMap(task.Lifecycle),
		"resources": taskResourcesMap(task.Resources),
		// Counts let a caller see what else the task declares and ask for the
		// matching section, instead of guessing whether a section is empty.
		"declares": map[string]any{
			"env":           len(task.Env),
			"templates":     len(task.Templates),
			"artifacts":     len(task.Artifacts),
			"services":      len(task.Services),
			"volume_mounts": len(task.VolumeMounts),
			"constraints":   len(task.Constraints),
			"actions":       len(task.Actions),
			"identities":    len(task.Identities),
		},
	}

	if sections.has(sectionMeta) {
		result["meta"] = stringMap(task.Meta)
	}

	if sections.has(sectionEnv) {
		result["env"] = redact.stringMap(task.Env)
	}

	if sections.has(sectionConfig) {
		result["config"] = configMap(task.Config, redact)
	}

	if sections.has(sectionTemplates) {
		result["templates"] = templateList(task.Templates, sections.has(sectionTemplateBodies), redact)
	}

	if sections.has(sectionArtifacts) {
		result["artifacts"] = artifactList(task.Artifacts, redact)
	}

	if sections.has(sectionNetworking) {
		result["services"] = serviceList(task.Services)
	}

	if sections.has(sectionStorage) {
		result["volume_mounts"] = volumeMountList(task.VolumeMounts)
	}

	if sections.has(sectionScheduling) {
		result["constraints"] = constraintList(task.Constraints)
		result["affinities"] = affinityList(task.Affinities)
		result["restart_policy"] = restartPolicyMap(task.RestartPolicy)
	}

	if sections.has(sectionIdentity) {
		result["vault"] = vaultMap(task.Vault)
		result["consul"] = consulMap(task.Consul)
		result["identities"] = identityList(task.Identity, task.Identities)
	}

	if sections.has(sectionLifecycle) {
		result["kill_timeout"] = durationString(task.KillTimeout)
		result["kill_signal"] = task.KillSignal
		result["shutdown_delay"] = task.ShutdownDelay.String()
		result["log_config"] = logConfigMap(task.LogConfig)
		result["actions"] = actionList(task.Actions)
	}

	return result
}

func taskResourcesMap(resources *api.Resources) map[string]any {
	if resources == nil {
		return map[string]any{}
	}

	memory := derefInt(resources.MemoryMB)
	memoryMax := derefInt(resources.MemoryMaxMB)

	result := map[string]any{
		"cpu_mhz":       derefInt(resources.CPU),
		"cores":         derefInt(resources.Cores),
		"memory_mb":     memory,
		"memory_max_mb": memoryMax,
		// memory_max of 0 means oversubscription is off: the hard limit is the
		// reservation itself.
		"memory_limit_mb": func() int {
			if memoryMax > 0 {
				return memoryMax
			}
			return memory
		}(),
		"disk_mb":    derefInt(resources.DiskMB),
		"secrets_mb": derefInt(resources.SecretsMB),
	}

	if len(resources.Devices) > 0 {
		devices := make([]map[string]any, 0, len(resources.Devices))
		for _, device := range resources.Devices {
			if device == nil {
				continue
			}
			devices = append(devices, map[string]any{
				"name":        device.Name,
				"count":       derefUint64(device.Count),
				"constraints": constraintList(device.Constraints),
			})
		}
		result["devices"] = devices
	}

	if resources.NUMA != nil {
		result["numa"] = map[string]any{
			"affinity": resources.NUMA.Affinity,
			"devices":  resources.NUMA.Devices,
		}
	}

	return result
}

func constraintList(constraints []*api.Constraint) []map[string]any {
	items := make([]map[string]any, 0, len(constraints))
	for _, constraint := range constraints {
		if constraint == nil {
			continue
		}
		items = append(items, map[string]any{
			"attribute": constraint.LTarget,
			"operator":  constraint.Operand,
			"value":     constraint.RTarget,
		})
	}
	return items
}

func affinityList(affinities []*api.Affinity) []map[string]any {
	items := make([]map[string]any, 0, len(affinities))
	for _, affinity := range affinities {
		if affinity == nil {
			continue
		}
		items = append(items, map[string]any{
			"attribute": affinity.LTarget,
			"operator":  affinity.Operand,
			"value":     affinity.RTarget,
			"weight":    deref(affinity.Weight),
		})
	}
	return items
}

func spreadList(spreads []*api.Spread) []map[string]any {
	items := make([]map[string]any, 0, len(spreads))
	for _, spread := range spreads {
		if spread == nil {
			continue
		}
		targets := make([]map[string]any, 0, len(spread.SpreadTarget))
		for _, target := range spread.SpreadTarget {
			if target == nil {
				continue
			}
			targets = append(targets, map[string]any{
				"value":   target.Value,
				"percent": target.Percent,
			})
		}
		items = append(items, map[string]any{
			"attribute": spread.Attribute,
			"weight":    deref(spread.Weight),
			"targets":   targets,
		})
	}
	return items
}

func restartPolicyMap(policy *api.RestartPolicy) map[string]any {
	if policy == nil {
		return map[string]any{}
	}
	return map[string]any{
		"attempts":         derefInt(policy.Attempts),
		"interval":         durationString(policy.Interval),
		"delay":            durationString(policy.Delay),
		"mode":             derefString(policy.Mode),
		"render_templates": derefBool(policy.RenderTemplates),
	}
}

func reschedulePolicyMap(policy *api.ReschedulePolicy) map[string]any {
	if policy == nil {
		return map[string]any{}
	}
	return map[string]any{
		"attempts":       derefInt(policy.Attempts),
		"interval":       durationString(policy.Interval),
		"delay":          durationString(policy.Delay),
		"delay_function": derefString(policy.DelayFunction),
		"max_delay":      durationString(policy.MaxDelay),
		"unlimited":      derefBool(policy.Unlimited),
	}
}

func updateStrategyMap(update *api.UpdateStrategy) map[string]any {
	if update == nil {
		return map[string]any{}
	}
	return map[string]any{
		"stagger":           durationString(update.Stagger),
		"max_parallel":      derefInt(update.MaxParallel),
		"health_check":      derefString(update.HealthCheck),
		"min_healthy_time":  durationString(update.MinHealthyTime),
		"healthy_deadline":  durationString(update.HealthyDeadline),
		"progress_deadline": durationString(update.ProgressDeadline),
		"canary":            derefInt(update.Canary),
		"auto_revert":       derefBool(update.AutoRevert),
		"auto_promote":      derefBool(update.AutoPromote),
	}
}

func migrateStrategyMap(migrate *api.MigrateStrategy) map[string]any {
	if migrate == nil {
		return map[string]any{}
	}
	return map[string]any{
		"max_parallel":     derefInt(migrate.MaxParallel),
		"health_check":     derefString(migrate.HealthCheck),
		"min_healthy_time": durationString(migrate.MinHealthyTime),
		"healthy_deadline": durationString(migrate.HealthyDeadline),
	}
}

func disconnectMap(disconnect *api.DisconnectStrategy) map[string]any {
	if disconnect == nil {
		return map[string]any{}
	}

	result := map[string]any{
		"lost_after":           durationString(disconnect.LostAfter),
		"stop_on_client_after": durationString(disconnect.StopOnClientAfter),
		"replace":              derefBool(disconnect.Replace),
	}
	if disconnect.Reconcile != nil {
		result["reconcile"] = string(*disconnect.Reconcile)
	}
	return result
}

func periodicMap(periodic *api.PeriodicConfig) map[string]any {
	if periodic == nil {
		return map[string]any{}
	}
	return map[string]any{
		"enabled":          derefBool(periodic.Enabled),
		"cron":             derefString(periodic.Spec),
		"crons":            periodic.Specs,
		"prohibit_overlap": derefBool(periodic.ProhibitOverlap),
		"time_zone":        derefString(periodic.TimeZone),
	}
}

func parameterizedMap(parameterized *api.ParameterizedJobConfig) map[string]any {
	if parameterized == nil {
		return map[string]any{}
	}
	return map[string]any{
		"payload":       parameterized.Payload,
		"meta_required": parameterized.MetaRequired,
		"meta_optional": parameterized.MetaOptional,
	}
}

func multiregionMap(multiregion *api.Multiregion) map[string]any {
	if multiregion == nil {
		return map[string]any{}
	}

	regions := make([]map[string]any, 0, len(multiregion.Regions))
	for _, region := range multiregion.Regions {
		if region == nil {
			continue
		}
		regions = append(regions, map[string]any{
			"name":        region.Name,
			"count":       derefInt(region.Count),
			"datacenters": region.Datacenters,
			"node_pool":   region.NodePool,
		})
	}

	result := map[string]any{"regions": regions}
	if multiregion.Strategy != nil {
		result["strategy"] = map[string]any{
			"max_parallel": derefInt(multiregion.Strategy.MaxParallel),
			"on_failure":   derefString(multiregion.Strategy.OnFailure),
		}
	}
	return result
}

func scalingPolicyMap(policy *api.ScalingPolicy) map[string]any {
	if policy == nil {
		return map[string]any{}
	}
	return map[string]any{
		"min":     derefInt64(policy.Min),
		"max":     derefInt64(policy.Max),
		"enabled": derefBool(policy.Enabled),
		"type":    policy.Type,
		// The policy body is operator-defined and can be large; report only that
		// it exists.
		"has_policy": len(policy.Policy) > 0,
	}
}

func ephemeralDiskMap(disk *api.EphemeralDisk) map[string]any {
	if disk == nil {
		return map[string]any{}
	}
	return map[string]any{
		"size_mb": derefInt(disk.SizeMB),
		"sticky":  derefBool(disk.Sticky),
		"migrate": derefBool(disk.Migrate),
	}
}

func networkList(networks []*api.NetworkResource) []map[string]any {
	items := make([]map[string]any, 0, len(networks))
	for _, network := range networks {
		if network == nil {
			continue
		}

		item := map[string]any{
			"mode":           network.Mode,
			"device":         network.Device,
			"hostname":       network.Hostname,
			"dynamic_ports":  portList(network.DynamicPorts),
			"reserved_ports": portList(network.ReservedPorts),
		}
		if network.DNS != nil {
			item["dns"] = map[string]any{
				"servers":  network.DNS.Servers,
				"searches": network.DNS.Searches,
				"options":  network.DNS.Options,
			}
		}
		items = append(items, item)
	}
	return items
}

func portList(ports []api.Port) []map[string]any {
	items := make([]map[string]any, 0, len(ports))
	for _, port := range ports {
		items = append(items, map[string]any{
			"label":        port.Label,
			"static":       port.Value,
			"to":           port.To,
			"host_network": port.HostNetwork,
		})
	}
	return items
}

func serviceList(services []*api.Service) []map[string]any {
	items := make([]map[string]any, 0, len(services))
	for _, service := range services {
		if service == nil {
			continue
		}

		checks := make([]map[string]any, 0, len(service.Checks))
		for _, check := range service.Checks {
			checks = append(checks, map[string]any{
				"name":            check.Name,
				"type":            check.Type,
				"path":            check.Path,
				"protocol":        check.Protocol,
				"port":            check.PortLabel,
				"interval":        check.Interval.String(),
				"timeout":         check.Timeout.String(),
				"method":          check.Method,
				"task":            check.TaskName,
				"on_update":       check.OnUpdate,
				"tls_skip_verify": check.TLSSkipVerify,
			})
		}

		items = append(items, map[string]any{
			"name":         service.Name,
			"provider":     service.Provider,
			"port":         service.PortLabel,
			"address_mode": service.AddressMode,
			"tags":         service.Tags,
			"canary_tags":  service.CanaryTags,
			"task":         service.TaskName,
			"on_update":    service.OnUpdate,
			"meta":         stringMap(service.Meta),
			"checks":       checks,
			"connect":      service.Connect != nil,
		})
	}
	return items
}

func volumeRequestList(volumes map[string]*api.VolumeRequest) []map[string]any {
	items := make([]map[string]any, 0, len(volumes))
	for _, name := range sortedKeys(volumes) {
		volume := volumes[name]
		if volume == nil {
			continue
		}
		items = append(items, map[string]any{
			"name":            name,
			"type":            volume.Type,
			"source":          volume.Source,
			"read_only":       volume.ReadOnly,
			"sticky":          volume.Sticky,
			"access_mode":     volume.AccessMode,
			"attachment_mode": volume.AttachmentMode,
			"per_alloc":       volume.PerAlloc,
		})
	}
	return items
}

func volumeMountList(mounts []*api.VolumeMount) []map[string]any {
	items := make([]map[string]any, 0, len(mounts))
	for _, mount := range mounts {
		if mount == nil {
			continue
		}
		items = append(items, map[string]any{
			"volume":           derefString(mount.Volume),
			"destination":      derefString(mount.Destination),
			"read_only":        derefBool(mount.ReadOnly),
			"propagation_mode": derefString(mount.PropagationMode),
			"selinux_label":    derefString(mount.SELinuxLabel),
		})
	}
	return items
}

func configMap(config map[string]any, redact *redactor) map[string]any {
	if len(config) == 0 {
		return map[string]any{}
	}

	clone := make(map[string]any, len(config))
	for key, value := range config {
		clone[key] = redact.anyValue(key, value)
	}
	return clone
}

func templateList(templates []*api.Template, includeBodies bool, redact *redactor) []map[string]any {
	items := make([]map[string]any, 0, len(templates))
	for _, template := range templates {
		if template == nil {
			continue
		}

		body := derefString(template.EmbeddedTmpl)
		item := map[string]any{
			"source":         derefString(template.SourcePath),
			"destination":    derefString(template.DestPath),
			"change_mode":    derefString(template.ChangeMode),
			"change_signal":  derefString(template.ChangeSignal),
			"perms":          derefString(template.Perms),
			"splay":          durationString(template.Splay),
			"env":            derefBool(template.Envvars),
			"once":           derefBool(template.Once),
			"left_delimiter": derefString(template.LeftDelim),
			"embedded_bytes": len(body),
			// Templates render Vault secrets on the client at runtime; the spec
			// holds only the paths, which is what makes them worth reporting.
			"reads_vault": vaultTemplatePattern.MatchString(body),
		}
		if includeBodies {
			item["body"] = redact.templateBody(body)
		}
		items = append(items, item)
	}
	return items
}

func artifactList(artifacts []*api.TaskArtifact, redact *redactor) []map[string]any {
	items := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		if artifact == nil {
			continue
		}
		items = append(items, map[string]any{
			"source":      derefString(artifact.GetterSource),
			"destination": derefString(artifact.RelativeDest),
			"mode":        derefString(artifact.GetterMode),
			"insecure":    derefBool(artifact.GetterInsecure),
			"options":     redact.stringMap(artifact.GetterOptions),
			"headers":     redact.stringMap(artifact.GetterHeaders),
		})
	}
	return items
}

func vaultMap(vault *api.Vault) map[string]any {
	if vault == nil {
		return map[string]any{}
	}
	return map[string]any{
		"policies":      vault.Policies,
		"role":          vault.Role,
		"namespace":     derefString(vault.Namespace),
		"cluster":       vault.Cluster,
		"env":           derefBool(vault.Env),
		"change_mode":   derefString(vault.ChangeMode),
		"change_signal": derefString(vault.ChangeSignal),
	}
}

func consulMap(consul *api.Consul) map[string]any {
	if consul == nil {
		return map[string]any{}
	}
	return map[string]any{
		"namespace": consul.Namespace,
		"cluster":   consul.Cluster,
		"partition": consul.Partition,
	}
}

func identityList(identity *api.WorkloadIdentity, identities []*api.WorkloadIdentity) []map[string]any {
	all := make([]*api.WorkloadIdentity, 0, len(identities)+1)
	if identity != nil {
		all = append(all, identity)
	}
	all = append(all, identities...)

	items := make([]map[string]any, 0, len(all))
	for _, candidate := range all {
		if candidate == nil {
			continue
		}
		items = append(items, map[string]any{
			"name":         candidate.Name,
			"audience":     candidate.Audience,
			"env":          candidate.Env,
			"file":         candidate.File,
			"filepath":     candidate.Filepath,
			"service_name": candidate.ServiceName,
			"ttl":          candidate.TTL.String(),
			"change_mode":  candidate.ChangeMode,
		})
	}
	return items
}

func lifecycleMap(lifecycle *api.TaskLifecycle) map[string]any {
	if lifecycle == nil {
		return map[string]any{}
	}
	return map[string]any{
		"hook":    lifecycle.Hook,
		"sidecar": lifecycle.Sidecar,
	}
}

func logConfigMap(config *api.LogConfig) map[string]any {
	if config == nil {
		return map[string]any{}
	}
	return map[string]any{
		"max_files":        derefInt(config.MaxFiles),
		"max_file_size_mb": derefInt(config.MaxFileSizeMB),
		"disabled":         derefBool(config.Disabled),
	}
}

func actionList(actions []*api.Action) []map[string]any {
	items := make([]map[string]any, 0, len(actions))
	for _, action := range actions {
		if action == nil {
			continue
		}
		items = append(items, map[string]any{
			"name":    action.Name,
			"command": action.Command,
			"args":    action.Args,
		})
	}
	return items
}

func durationString(value *time.Duration) string {
	if value == nil {
		return ""
	}
	return value.String()
}

func deref[T any](value *T) T {
	if value == nil {
		var zero T
		return zero
	}
	return *value
}
