package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/grafana/grafana/pkg/components/simplejson"
	"github.com/grafana/grafana/pkg/services/publicdashboards/internal/models"
)

// Public dashboards are viewed anonymously, so a viewer must never be able to put an arbitrary
// string into a query. A variable can only be overridden from the request when its options are
// known in advance, and only with a value drawn from that set. Every other variable is frozen at
// the value the dashboard was saved with.
//
// Custom and interval variables carry their options in the dashboard. Query variables do not:
// resolving those means running the variable query, which is the browser's job and which a public
// viewer must never trigger, so their options come from the snapshot the author's browser recorded
// when the public dashboard was saved.
const (
	variableTypeCustom   = "custom"
	variableTypeInterval = "interval"
	variableTypeQuery    = "query"
)

// allValue is the sentinel Grafana uses for the "All" option on a multi-value variable.
const allValue = "$__all"

// publicVariable is a template variable normalised across schema V1 and V2.
type publicVariable struct {
	name string
	// options are the values the dashboard author enumerated. Empty for variable types whose
	// options are not knowable without running a query.
	options []string
	// overridable is true when a viewer may pick a different value via the request.
	overridable bool
	multi       bool
	includeAll  bool
	allValue    string
	// current is the value the dashboard was saved with, used when the viewer asks for nothing.
	current []string
}

// resolveVariables reconciles the variables declared on a dashboard with the overrides a viewer
// requested, and returns the values to interpolate keyed by variable name.
//
// Overrides the dashboard cannot honour are ignored rather than rejected. The frontend's url sync
// writes ?var-<name> for every variable it holds, not only the overridable ones, so rejecting
// those would break every panel on a dashboard that mixes variable types. The case that does fail
// the request is a value chosen for an overridable variable that is not one of the author's
// options: quietly rendering a different device's data there would be misleading.
func resolveVariables(vars []publicVariable, requested map[string][]string) (map[string][]string, error) {
	resolved := make(map[string][]string, len(vars))

	for _, v := range vars {
		values, ok := requested[v.name]
		if !ok || len(values) == 0 || !v.overridable {
			resolved[v.name] = v.current
			continue
		}

		if !v.multi && len(values) > 1 {
			return nil, models.ErrInvalidVariableValue.Errorf("resolveVariables: variable %q is not multi-value but got %d values", v.name, len(values))
		}

		accepted, err := v.accept(values)
		if err != nil {
			return nil, err
		}
		resolved[v.name] = accepted
	}

	return resolved, nil
}

// accept checks every requested value against the variable's option list, expanding the "All"
// sentinel when the author enabled it.
func (v publicVariable) accept(values []string) ([]string, error) {
	allowed := make(map[string]struct{}, len(v.options))
	for _, o := range v.options {
		allowed[o] = struct{}{}
	}

	accepted := make([]string, 0, len(values))
	for _, value := range values {
		if value == allValue {
			if !v.includeAll {
				return nil, models.ErrInvalidVariableValue.Errorf("resolveVariables: variable %q does not have an All option", v.name)
			}
			if v.allValue != "" {
				return []string{v.allValue}, nil
			}
			return v.options, nil
		}
		if _, ok := allowed[value]; !ok {
			// The rejected value is deliberately left out of the message: it is attacker-controlled
			// and would be echoed back to the browser.
			return nil, models.ErrInvalidVariableValue.Errorf("resolveVariables: value not among the options saved for variable %q", v.name)
		}
		accepted = append(accepted, value)
	}

	return accepted, nil
}

// extractVariablesV1 reads templating.list from a classic dashboard.
func extractVariablesV1(data *simplejson.Json, recorded models.TemplateVariables) []publicVariable {
	list := data.GetPath("templating", "list").MustArray()
	vars := make([]publicVariable, 0, len(list))

	for _, item := range list {
		spec := simplejson.NewFromAny(item)
		if v, ok := newPublicVariable(spec.Get("type").MustString(), spec, recorded); ok {
			vars = append(vars, v)
		}
	}

	return vars
}

// extractVariablesV2 reads the variables array from a schema V2 dashboard, where each entry is a
// kind wrapper such as {"kind": "CustomVariable", "spec": {...}}.
func extractVariablesV2(data *simplejson.Json, recorded models.TemplateVariables) []publicVariable {
	list := data.Get("variables").MustArray()
	vars := make([]publicVariable, 0, len(list))

	for _, item := range list {
		kind := simplejson.NewFromAny(item)
		// "CustomVariable" -> "custom", so the two schemas share one overridability rule.
		varType := strings.ToLower(strings.TrimSuffix(kind.Get("kind").MustString(), "Variable"))
		if v, ok := newPublicVariable(varType, kind.Get("spec"), recorded); ok {
			vars = append(vars, v)
		}
	}

	return vars
}

// newPublicVariable normalises one variable and decides whether a viewer may choose its value.
func newPublicVariable(varType string, spec *simplejson.Json, recorded models.TemplateVariables) (publicVariable, bool) {
	name := spec.Get("name").MustString()
	if name == "" {
		return publicVariable{}, false
	}

	v := publicVariable{
		name:       name,
		multi:      spec.Get("multi").MustBool(),
		includeAll: spec.Get("includeAll").MustBool(),
		allValue:   spec.Get("allValue").MustString(),
		current:    currentValues(spec.Get("current")),
	}

	switch varType {
	case variableTypeCustom, variableTypeInterval:
		// The dashboard is the live source, so an author who edits the option list narrows what
		// viewers can ask for immediately.
		v.options = optionValues(spec.Get("options").MustArray())
		if len(v.options) == 0 {
			v.options = parseOptionsQuery(spec.Get("query"))
		}
		v.overridable = len(v.options) > 0

	case variableTypeQuery:
		// The dashboard's own options for a query variable are whatever the last author to save it
		// happened to have loaded, so they are not a deliberate allowlist. Use only the options
		// recorded against the public dashboard; without them the variable stays frozen.
		// The sentinel is dropped here as well as in the client, since this is the trust boundary.
		v.options = withoutAllSentinel(recorded.OptionsFor(name))
		v.overridable = len(v.options) > 0
	}

	return v, true
}

// withoutAllSentinel drops the "All" pseudo-option, which is expanded by accept() rather than
// being a value in its own right.
func withoutAllSentinel(options []string) []string {
	filtered := make([]string, 0, len(options))
	for _, option := range options {
		if option != allValue {
			filtered = append(filtered, option)
		}
	}
	return filtered
}

func optionValues(options []interface{}) []string {
	values := make([]string, 0, len(options))
	for _, o := range options {
		opt := simplejson.NewFromAny(o)
		for _, value := range toStrings(opt.Get("value")) {
			// The "All" option is stored alongside the real ones but is not a value in its own
			// right; accept() expands it instead.
			if value != allValue {
				values = append(values, value)
			}
		}
	}
	return values
}

// parseOptionsQuery reads the option list out of a custom or interval variable's query, which is
// a comma separated list and may use "display : value" pairs.
func parseOptionsQuery(query *simplejson.Json) []string {
	raw, err := query.String()
	if err != nil {
		return nil
	}

	values := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, value, found := strings.Cut(part, " : "); found {
			part = strings.TrimSpace(value)
		}
		values = append(values, part)
	}

	return values
}

func currentValues(current *simplejson.Json) []string {
	return toStrings(current.Get("value"))
}

func toStrings(value *simplejson.Json) []string {
	if s, err := value.String(); err == nil {
		return []string{s}
	}
	if arr, err := value.Array(); err == nil {
		values := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
		return values
	}
	return nil
}

// Mirrors the syntax the frontend template service accepts: $name, [[name:format]] and
// ${name.fieldPath:format}. The field path is matched so that it does not break the expression,
// but it is ignored because public dashboard variable values are always plain strings.
var variableRegex = regexp.MustCompile(`\$(\w+)|\[\[(\w+?)(?::(\w+))?\]\]|\$\{(\w+)(?:\.[^:^\}]+)?(?::([^\}]+))?\}`)

// interpolateQueries replaces variable expressions throughout each panel query. It returns fresh
// queries rather than editing in place, so the caller's dashboard data is left untouched.
func interpolateQueries(queries []*simplejson.Json, values map[string][]string) []*simplejson.Json {
	if len(values) == 0 {
		return queries
	}

	interpolated := make([]*simplejson.Json, len(queries))
	for i, query := range queries {
		interpolated[i] = simplejson.NewFromAny(interpolateAny(query.Interface(), values))
	}
	return interpolated
}

func interpolateAny(value interface{}, values map[string][]string) interface{} {
	switch typed := value.(type) {
	case string:
		return interpolateString(typed, values)
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, item := range typed {
			out[i] = interpolateAny(item, values)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, item := range typed {
			out[k] = interpolateAny(item, values)
		}
		return out
	default:
		return value
	}
}

func interpolateString(target string, values map[string][]string) string {
	if !strings.ContainsAny(target, "$[") {
		return target
	}

	return variableRegex.ReplaceAllStringFunc(target, func(match string) string {
		groups := variableRegex.FindStringSubmatch(match)
		// The three alternatives put the name in groups 1, 2 and 4, and the format in 3 and 5.
		name, format := groups[1], ""
		if groups[2] != "" {
			name, format = groups[2], groups[3]
		} else if groups[4] != "" {
			name, format = groups[4], groups[5]
		}

		value, ok := values[name]
		if !ok {
			// Leave unknown expressions alone; they may be datasource syntax rather than a variable.
			return match
		}

		return formatValue(value, format)
	})
}

// formatValue renders a variable value using the format specifier from the expression, matching
// the frontend's formatVariableValue.
func formatValue(values []string, format string) string {
	switch format {
	case "csv":
		return strings.Join(values, ",")
	case "pipe":
		return strings.Join(values, "|")
	case "raw":
		return strings.Join(values, ",")
	case "singlequote":
		return joinQuoted(values, "'", `\'`, ",")
	case "doublequote":
		return joinQuoted(values, `"`, `\"`, ",")
	case "sqlstring":
		return joinQuoted(values, "'", "''", ",")
	case "json":
		quoted := make([]string, len(values))
		for i, v := range values {
			quoted[i] = fmt.Sprintf("%q", v)
		}
		if len(values) == 1 {
			return quoted[0]
		}
		return "[" + strings.Join(quoted, ",") + "]"
	case "regex":
		escaped := make([]string, len(values))
		for i, v := range values {
			escaped[i] = regexp.QuoteMeta(v)
		}
		if len(escaped) == 1 {
			return escaped[0]
		}
		return "(" + strings.Join(escaped, "|") + ")"
	case "percentencode":
		return percentEncode(glob(values))
	case "lucene":
		return luceneFormat(values)
	default:
		return glob(values)
	}
}

// glob is the default rendering: a single value as-is, several as a brace expansion.
func glob(values []string) string {
	if len(values) == 1 {
		return values[0]
	}
	return "{" + strings.Join(values, ",") + "}"
}

func luceneFormat(values []string) string {
	if len(values) == 1 {
		return `"` + values[0] + `"`
	}
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = `"` + v + `"`
	}
	return "(" + strings.Join(quoted, " OR ") + ")"
}

func joinQuoted(values []string, quote, escaped, sep string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = quote + strings.ReplaceAll(v, quote, escaped) + quote
	}
	return strings.Join(out, sep)
}

func percentEncode(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case strings.ContainsRune("-_.!~*'()", r):
			b.WriteRune(r)
		default:
			for _, c := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", c)
			}
		}
	}
	return b.String()
}
