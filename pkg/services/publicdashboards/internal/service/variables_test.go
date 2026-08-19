package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/grafana/pkg/components/simplejson"
	"github.com/grafana/grafana/pkg/infra/log"
	"github.com/grafana/grafana/pkg/services/dashboards"
	"github.com/grafana/grafana/pkg/services/featuremgmt"
	"github.com/grafana/grafana/pkg/services/publicdashboards/internal/models"
	"github.com/grafana/grafana/pkg/services/publicdashboards/internal/service/intervalv2"
)

func devicesDashboardV1(t *testing.T) *simplejson.Json {
	t.Helper()

	data, err := simplejson.NewJson([]byte(`{
		"templating": {
			"list": [
				{
					"type": "custom",
					"name": "dev",
					"query": "01,02,03",
					"current": {"text": "01", "value": "01"},
					"options": [
						{"text": "01", "value": "01"},
						{"text": "02", "value": "02"},
						{"text": "03", "value": "03"}
					]
				},
				{
					"type": "constant",
					"name": "env",
					"query": "prod",
					"current": {"text": "prod", "value": "prod"}
				},
				{
					"type": "query",
					"name": "host",
					"current": {"text": "web-1", "value": "web-1"},
					"options": [{"text": "web-1", "value": "web-1"}]
				}
			]
		}
	}`))
	require.NoError(t, err)

	return data
}

func TestExtractVariablesV1(t *testing.T) {
	vars := extractVariablesV1(devicesDashboardV1(t), models.TemplateVariables{})
	require.Len(t, vars, 3)

	assert.Equal(t, "dev", vars[0].name)
	assert.True(t, vars[0].overridable)
	assert.Equal(t, []string{"01", "02", "03"}, vars[0].options)
	assert.Equal(t, []string{"01"}, vars[0].current)

	assert.Equal(t, "env", vars[1].name)
	assert.False(t, vars[1].overridable, "constant variables must stay frozen")
	assert.Equal(t, []string{"prod"}, vars[1].current)

	assert.Equal(t, "host", vars[2].name)
	assert.False(t, vars[2].overridable, "query variables must stay frozen")
	assert.Equal(t, []string{"web-1"}, vars[2].current)
}

func TestExtractVariablesV1_OptionsFallBackToQuery(t *testing.T) {
	data, err := simplejson.NewJson([]byte(`{
		"templating": {"list": [
			{"type": "custom", "name": "dev", "query": "Device 01 : 01, Device 02 : 02", "current": {"value": "01"}}
		]}
	}`))
	require.NoError(t, err)

	vars := extractVariablesV1(data, models.TemplateVariables{})
	require.Len(t, vars, 1)
	assert.Equal(t, []string{"01", "02"}, vars[0].options, "display:value pairs should contribute the value side")
}

func TestExtractVariablesV2(t *testing.T) {
	data, err := simplejson.NewJson([]byte(`{
		"variables": [
			{
				"kind": "CustomVariable",
				"spec": {
					"name": "dev",
					"query": "01,02",
					"current": {"text": "01", "value": "01"},
					"options": [{"text": "01", "value": "01"}, {"text": "02", "value": "02"}]
				}
			},
			{
				"kind": "QueryVariable",
				"spec": {"name": "host", "current": {"value": "web-1"}}
			}
		]
	}`))
	require.NoError(t, err)

	vars := extractVariablesV2(data, models.TemplateVariables{})
	require.Len(t, vars, 2)

	assert.Equal(t, "dev", vars[0].name)
	assert.True(t, vars[0].overridable)
	assert.Equal(t, []string{"01", "02"}, vars[0].options)

	assert.Equal(t, "host", vars[1].name)
	assert.False(t, vars[1].overridable)
}

func TestResolveVariables(t *testing.T) {
	vars := extractVariablesV1(devicesDashboardV1(t), models.TemplateVariables{})

	t.Run("falls back to the saved value when nothing is requested", func(t *testing.T) {
		values, err := resolveVariables(vars, nil)
		require.NoError(t, err)
		assert.Equal(t, map[string][]string{
			"dev":  {"01"},
			"env":  {"prod"},
			"host": {"web-1"},
		}, values)
	})

	t.Run("accepts a value from the saved options", func(t *testing.T) {
		values, err := resolveVariables(vars, map[string][]string{"dev": {"02"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"02"}, values["dev"])
		assert.Equal(t, []string{"prod"}, values["env"], "untouched variables keep their saved value")
	})

	t.Run("rejects a value that is not among the saved options", func(t *testing.T) {
		_, err := resolveVariables(vars, map[string][]string{"dev": {"99"}})
		require.Error(t, err)
		assert.True(t, models.ErrInvalidVariableValue.Is(err))
	})

	t.Run("does not echo the rejected value back to the caller", func(t *testing.T) {
		_, err := resolveVariables(vars, map[string][]string{"dev": {"<script>alert(1)</script>"}})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "script")
	})

	t.Run("ignores an override of a frozen variable", func(t *testing.T) {
		// Url sync emits ?var-host for a query variable too; honouring it would let a viewer pick
		// a value the author never enumerated, so the saved value wins silently.
		values, err := resolveVariables(vars, map[string][]string{"host": {"web-2"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"web-1"}, values["host"])

		values, err = resolveVariables(vars, map[string][]string{"env": {"staging"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"prod"}, values["env"])
	})

	t.Run("ignores an unknown variable name", func(t *testing.T) {
		values, err := resolveVariables(vars, map[string][]string{"nope": {"01"}})
		require.NoError(t, err)
		assert.NotContains(t, values, "nope")
		assert.Equal(t, []string{"01"}, values["dev"], "the other variables still resolve")
	})

	t.Run("rejects several values for a single-value variable", func(t *testing.T) {
		_, err := resolveVariables(vars, map[string][]string{"dev": {"01", "02"}})
		require.Error(t, err)
	})
}

func TestResolveVariables_Multi(t *testing.T) {
	data, err := simplejson.NewJson([]byte(`{
		"templating": {"list": [{
			"type": "custom",
			"name": "dev",
			"multi": true,
			"includeAll": true,
			"current": {"value": ["01"]},
			"options": [
				{"value": "$__all"},
				{"value": "01"},
				{"value": "02"},
				{"value": "03"}
			]
		}]}
	}`))
	require.NoError(t, err)
	vars := extractVariablesV1(data, models.TemplateVariables{})

	require.Equal(t, []string{"01", "02", "03"}, vars[0].options, "the All sentinel is not itself an option")

	t.Run("accepts several allowlisted values", func(t *testing.T) {
		values, err := resolveVariables(vars, map[string][]string{"dev": {"01", "03"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"01", "03"}, values["dev"])
	})

	t.Run("rejects the batch when one value is not allowlisted", func(t *testing.T) {
		_, err := resolveVariables(vars, map[string][]string{"dev": {"01", "99"}})
		require.Error(t, err)
	})

	t.Run("expands the All sentinel to every option", func(t *testing.T) {
		values, err := resolveVariables(vars, map[string][]string{"dev": {allValue}})
		require.NoError(t, err)
		assert.Equal(t, []string{"01", "02", "03"}, values["dev"])
	})

	t.Run("uses the custom all value when the author set one", func(t *testing.T) {
		withAllValue := vars
		withAllValue[0].allValue = ".*"
		values, err := resolveVariables(withAllValue, map[string][]string{"dev": {allValue}})
		require.NoError(t, err)
		assert.Equal(t, []string{".*"}, values["dev"])
	})
}

func TestResolveVariables_AllRejectedWhenNotEnabled(t *testing.T) {
	vars := extractVariablesV1(devicesDashboardV1(t), models.TemplateVariables{})
	_, err := resolveVariables(vars, map[string][]string{"dev": {allValue}})
	require.Error(t, err)
}

func TestInterpolateString(t *testing.T) {
	values := map[string][]string{
		"dev":  {"01"},
		"devs": {"01", "02"},
	}

	tests := []struct {
		name     string
		target   string
		expected string
	}{
		{"bare", "device = $dev", "device = 01"},
		{"braced", "device = ${dev}", "device = 01"},
		{"double bracket", "device = [[dev]]", "device = 01"},
		{"braced with format", "device = ${dev:csv}", "device = 01"},
		{"bracket with format", "device = [[dev:csv]]", "device = 01"},
		{"field path is ignored", "device = ${dev.text}", "device = 01"},
		{"multi default is a glob", "device = $devs", "device = {01,02}"},
		{"multi csv", "device = ${devs:csv}", "device = 01,02"},
		{"multi pipe", "device = ${devs:pipe}", "device = 01|02"},
		{"multi regex", "device =~ ${devs:regex}", "device =~ (01|02)"},
		{"multi json", "device = ${devs:json}", `device = ["01","02"]`},
		{"multi singlequote", "device in (${devs:singlequote})", "device in ('01','02')"},
		{"multi lucene", "device: ${devs:lucene}", `device: ("01" OR "02")`},
		{"unknown variable is left alone", "device = $nope", "device = $nope"},
		{"no variables", "rate(foo[5m])", "rate(foo[5m])"},
		{"repeated", "$dev-$dev", "01-01"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, interpolateString(tc.target, values))
		})
	}
}

func TestInterpolateString_EscapesQuotesInValues(t *testing.T) {
	values := map[string][]string{"dev": {"o'brien"}}
	assert.Equal(t, "'o''brien'", interpolateString("${dev:sqlstring}", values))
	assert.Equal(t, `'o\'brien'`, interpolateString("${dev:singlequote}", values))
}

func TestInterpolateQueries(t *testing.T) {
	query, err := simplejson.NewJson([]byte(`{
		"refId": "A",
		"expr": "up{device=\"$dev\"}",
		"nested": {"legendFormat": "device ${dev}"},
		"list": ["[[dev]]", 42],
		"maxDataPoints": 100
	}`))
	require.NoError(t, err)

	original := query.MustMap()["expr"]
	interpolated := interpolateQueries([]*simplejson.Json{query}, map[string][]string{"dev": {"01"}})
	require.Len(t, interpolated, 1)

	assert.Equal(t, `up{device="01"}`, interpolated[0].Get("expr").MustString())
	assert.Equal(t, "device 01", interpolated[0].GetPath("nested", "legendFormat").MustString())
	assert.Equal(t, "01", interpolated[0].Get("list").MustArray()[0])
	assert.EqualValues(t, 42, interpolated[0].Get("list").GetIndex(1).MustInt64(), "non-strings pass through untouched")
	assert.EqualValues(t, 100, interpolated[0].Get("maxDataPoints").MustInt64())

	assert.Equal(t, original, query.MustMap()["expr"], "the source dashboard data must not be mutated")
}

func TestInterpolateQueries_NoValuesIsAPassThrough(t *testing.T) {
	query, err := simplejson.NewJson([]byte(`{"expr": "up{device=\"$dev\"}"}`))
	require.NoError(t, err)

	queries := []*simplejson.Json{query}
	assert.Same(t, query, interpolateQueries(queries, nil)[0])
}

func serviceWithVariables(t *testing.T, enabled bool) *PublicDashboardServiceImpl {
	t.Helper()

	features := featuremgmt.WithFeatures()
	if enabled {
		features = featuremgmt.WithFeatures(featuremgmt.FlagPublicDashboardsVariables)
	}

	return &PublicDashboardServiceImpl{
		log:                log.New("test.publicdashboards"),
		features:           features,
		intervalCalculator: intervalv2.NewCalculator(),
	}
}

func TestInterpolateVariables_ToggleGate(t *testing.T) {
	vars := extractVariablesV1(devicesDashboardV1(t), models.TemplateVariables{})
	newQuery := func(t *testing.T) *simplejson.Json {
		t.Helper()
		q, err := simplejson.NewJson([]byte(`{"expr": "up{device=\"$dev\"}"}`))
		require.NoError(t, err)
		return q
	}

	t.Run("leaves queries untouched while the toggle is off", func(t *testing.T) {
		pd := serviceWithVariables(t, false)

		queries, err := pd.interpolateVariables([]*simplejson.Json{newQuery(t)}, vars, models.PublicDashboardQueryDTO{
			Variables: map[string][]string{"dev": {"02"}},
		})
		require.NoError(t, err)
		assert.Equal(t, `up{device="$dev"}`, queries[0].Get("expr").MustString())
	})

	t.Run("interpolates the requested value while the toggle is on", func(t *testing.T) {
		pd := serviceWithVariables(t, true)

		queries, err := pd.interpolateVariables([]*simplejson.Json{newQuery(t)}, vars, models.PublicDashboardQueryDTO{
			Variables: map[string][]string{"dev": {"02"}},
		})
		require.NoError(t, err)
		assert.Equal(t, `up{device="02"}`, queries[0].Get("expr").MustString())
	})

	t.Run("interpolates the saved value when nothing is requested", func(t *testing.T) {
		pd := serviceWithVariables(t, true)

		queries, err := pd.interpolateVariables([]*simplejson.Json{newQuery(t)}, vars, models.PublicDashboardQueryDTO{})
		require.NoError(t, err)
		assert.Equal(t, `up{device="01"}`, queries[0].Get("expr").MustString())
	})

	t.Run("fails the request on a value outside the saved options", func(t *testing.T) {
		pd := serviceWithVariables(t, true)

		_, err := pd.interpolateVariables([]*simplejson.Json{newQuery(t)}, vars, models.PublicDashboardQueryDTO{
			Variables: map[string][]string{"dev": {"99"}},
		})
		require.Error(t, err)
		assert.True(t, models.ErrInvalidVariableValue.Is(err))
	})
}

func TestBuildMetricRequestInterpolatesVariables(t *testing.T) {
	dashboardJSON := `{
		"time": {"from": "now-6h", "to": "now"},
		"templating": {"list": [{
			"type": "custom",
			"name": "dev",
			"current": {"value": "01"},
			"options": [{"value": "01"}, {"value": "02"}]
		}]},
		"panels": [{
			"id": 1,
			"datasource": {"type": "prometheus", "uid": "ds1"},
			"targets": [{"refId": "A", "expr": "up{device=\"$dev\"}"}]
		}]
	}`

	data, err := simplejson.NewJson([]byte(dashboardJSON))
	require.NoError(t, err)
	dashboard := &dashboards.Dashboard{UID: "dash", Data: data}
	pubdash := &models.PublicDashboard{Uid: "pubdash", DashboardUid: "dash"}

	pd := serviceWithVariables(t, true)

	req, err := pd.buildMetricRequest(dashboard, pubdash, 1, models.PublicDashboardQueryDTO{
		IntervalMs:    10000,
		MaxDataPoints: 200,
		Variables:     map[string][]string{"dev": {"02"}},
	})
	require.NoError(t, err)
	require.Len(t, req.Queries, 1)
	assert.Equal(t, `up{device="02"}`, req.Queries[0].Get("expr").MustString())

	// A second viewer must not see the first viewer's value: interpolation has to leave the stored
	// dashboard alone.
	assert.Equal(t, `up{device="$dev"}`, data.Get("panels").GetIndex(0).Get("targets").GetIndex(0).Get("expr").MustString())

	req, err = pd.buildMetricRequest(dashboard, pubdash, 1, models.PublicDashboardQueryDTO{
		IntervalMs:    10000,
		MaxDataPoints: 200,
	})
	require.NoError(t, err)
	assert.Equal(t, `up{device="01"}`, req.Queries[0].Get("expr").MustString())
}

func recorded(options map[string][]string) models.TemplateVariables {
	return models.TemplateVariables{Version: 1, Options: options}
}

func TestExtractVariables_QueryVariableUsesRecordedOptions(t *testing.T) {
	t.Run("stays frozen without recorded options", func(t *testing.T) {
		vars := extractVariablesV1(devicesDashboardV1(t), models.TemplateVariables{})

		host := vars[2]
		require.Equal(t, "host", host.name)
		assert.False(t, host.overridable)
		assert.Empty(t, host.options)
	})

	t.Run("becomes overridable from the recorded options", func(t *testing.T) {
		vars := extractVariablesV1(devicesDashboardV1(t), recorded(map[string][]string{
			"host": {"web-1", "web-2", "web-3"},
		}))

		host := vars[2]
		require.Equal(t, "host", host.name)
		assert.True(t, host.overridable)
		assert.Equal(t, []string{"web-1", "web-2", "web-3"}, host.options)
	})

	t.Run("ignores the dashboard's own stale option list", func(t *testing.T) {
		// The dashboard has only web-1 saved against `host`; whatever the author's browser last
		// happened to load is not an allowlist, so only the recorded options count.
		vars := extractVariablesV1(devicesDashboardV1(t), recorded(map[string][]string{
			"host": {"web-2"},
		}))

		assert.Equal(t, []string{"web-2"}, vars[2].options)
	})

	t.Run("does not let recorded options widen a custom variable", func(t *testing.T) {
		// The dashboard is the live source for custom variables, so an author who removes an option
		// there takes it away immediately even if a stale snapshot still lists it.
		vars := extractVariablesV1(devicesDashboardV1(t), recorded(map[string][]string{
			"dev": {"01", "02", "03", "99"},
		}))

		assert.Equal(t, []string{"01", "02", "03"}, vars[0].options)

		_, err := resolveVariables(vars, map[string][]string{"dev": {"99"}})
		require.Error(t, err)
	})

	t.Run("works for schema v2", func(t *testing.T) {
		data, err := simplejson.NewJson([]byte(`{
			"variables": [
				{"kind": "QueryVariable", "spec": {"name": "host", "current": {"value": "web-1"}}}
			]
		}`))
		require.NoError(t, err)

		vars := extractVariablesV2(data, recorded(map[string][]string{"host": {"web-1", "web-2"}}))
		require.Len(t, vars, 1)
		assert.True(t, vars[0].overridable)
		assert.Equal(t, []string{"web-1", "web-2"}, vars[0].options)
	})
}

func TestResolveVariables_QueryVariable(t *testing.T) {
	vars := extractVariablesV1(devicesDashboardV1(t), recorded(map[string][]string{
		"host": {"web-1", "web-2"},
	}))

	t.Run("accepts a recorded value", func(t *testing.T) {
		values, err := resolveVariables(vars, map[string][]string{"host": {"web-2"}})
		require.NoError(t, err)
		assert.Equal(t, []string{"web-2"}, values["host"])
	})

	t.Run("rejects a value that was never recorded", func(t *testing.T) {
		_, err := resolveVariables(vars, map[string][]string{"host": {"web-9"}})
		require.Error(t, err)
		assert.True(t, models.ErrInvalidVariableValue.Is(err))
	})

	t.Run("falls back to the saved value when nothing is requested", func(t *testing.T) {
		values, err := resolveVariables(vars, nil)
		require.NoError(t, err)
		assert.Equal(t, []string{"web-1"}, values["host"])
	})
}

func TestBuildMetricRequestInterpolatesQueryVariable(t *testing.T) {
	data, err := simplejson.NewJson([]byte(`{
		"time": {"from": "now-6h", "to": "now"},
		"templating": {"list": [
			{"type": "query", "name": "host", "current": {"value": "web-1"}}
		]},
		"panels": [{
			"id": 1,
			"targets": [{"refId": "A", "expr": "up{host=\"$host\"}"}]
		}]
	}`))
	require.NoError(t, err)

	dashboard := &dashboards.Dashboard{UID: "dash", Data: data}
	pubdash := &models.PublicDashboard{
		Uid:               "pubdash",
		DashboardUid:      "dash",
		TemplateVariables: recorded(map[string][]string{"host": {"web-1", "web-2"}}),
	}
	pd := serviceWithVariables(t, true)

	req, err := pd.buildMetricRequest(dashboard, pubdash, 1, models.PublicDashboardQueryDTO{
		IntervalMs: 10000, MaxDataPoints: 200,
		Variables: map[string][]string{"host": {"web-2"}},
	})
	require.NoError(t, err)
	assert.Equal(t, `up{host="web-2"}`, req.Queries[0].Get("expr").MustString())

	_, err = pd.buildMetricRequest(dashboard, pubdash, 1, models.PublicDashboardQueryDTO{
		IntervalMs: 10000, MaxDataPoints: 200,
		Variables: map[string][]string{"host": {"web-9"}},
	})
	require.Error(t, err, "a host that was never recorded must not reach the datasource")
}
