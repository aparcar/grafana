package models

import (
	"encoding/json"
	"time"

	"github.com/grafana/grafana/pkg/kinds/dashboard"
	"github.com/grafana/grafana/pkg/services/user"
)

// PublicDashboardErr represents a dashboard error.
type PublicDashboardErr struct {
	StatusCode int
	Status     string
	Reason     string
}

// Error returns the error message.
func (e PublicDashboardErr) Error() string {
	if e.Reason != "" {
		return e.Reason
	}
	return "Dashboard Error"
}

const (
	EmailShareType                      ShareType = "email"
	PublicShareType                     ShareType = "public"
	FeaturePublicDashboardsEmailSharing           = "publicDashboardsEmailSharing"
)

var (
	ValidShareTypes = []ShareType{EmailShareType, PublicShareType}
)

type ShareType string

type PublicDashboard struct {
	Uid          string    `json:"uid" xorm:"pk uid"`
	DashboardUid string    `json:"dashboardUid" xorm:"dashboard_uid"`
	OrgId        int64     `json:"-" xorm:"org_id"` // Don't ever marshal orgId to Json
	AccessToken  string    `json:"accessToken" xorm:"access_token"`
	CreatedBy    int64     `json:"createdBy" xorm:"created_by"`
	UpdatedBy    int64     `json:"updatedBy" xorm:"updated_by"`
	CreatedAt    time.Time `json:"createdAt" xorm:"created_at"`
	UpdatedAt    time.Time `json:"updatedAt" xorm:"updated_at"`
	//config fields
	TimeSettings         *TimeSettings `json:"-" xorm:"time_settings"`
	TimeSelectionEnabled bool          `json:"timeSelectionEnabled" xorm:"time_selection_enabled"`
	IsEnabled            bool          `json:"isEnabled" xorm:"is_enabled"`
	AnnotationsEnabled   bool          `json:"annotationsEnabled" xorm:"annotations_enabled"`
	Share                ShareType     `json:"share" xorm:"share"`
	Recipients           []EmailDTO    `json:"recipients,omitempty" xorm:"-"`
	// TemplateVariables holds option lists that cannot be derived from the dashboard itself,
	// captured while an authenticated author was present. See TemplateVariables.
	//
	// Not a pointer: xorm allocates the struct when reading, so a nil written on insert would come
	// back as a non-nil empty one and a public dashboard would not survive a round trip unchanged.
	TemplateVariables TemplateVariables `json:"templateVariables,omitempty" xorm:"template_variables"`
}

type PublicDashboardDTO struct {
	Uid                  string             `json:"uid"`
	AccessToken          string             `json:"accessToken"`
	TimeSelectionEnabled *bool              `json:"timeSelectionEnabled"`
	IsEnabled            *bool              `json:"isEnabled"`
	AnnotationsEnabled   *bool              `json:"annotationsEnabled"`
	Share                ShareType          `json:"share"`
	TemplateVariables    *TemplateVariables `json:"templateVariables,omitempty"`
}

type EmailDTO struct {
	Uid       string `json:"uid"`
	Recipient string `json:"recipient"`
}

// Alias the generated type
type DashAnnotation = dashboard.AnnotationQuery

type AnnotationsDto struct {
	Annotations struct {
		List []DashAnnotation `json:"list"`
	}
}

type AnnotationEvent struct {
	Id           int64                     `json:"id"`
	DashboardId  int64                     `json:"dashboardId"`
	DashboardUID string                    `json:"dashboardUID"`
	PanelId      int64                     `json:"panelId"`
	Tags         []string                  `json:"tags"`
	IsRegion     bool                      `json:"isRegion"`
	Text         string                    `json:"text"`
	Color        string                    `json:"color"`
	Time         int64                     `json:"time"`
	TimeEnd      int64                     `json:"timeEnd"`
	Source       dashboard.AnnotationQuery `json:"source"`
}

func (pd PublicDashboard) TableName() string {
	return "dashboard_public"
}

type PublicDashboardListQuery struct {
	OrgID  int64
	Query  string
	Page   int
	Limit  int
	Offset int
	User   *user.SignedInUser
}

type PublicDashboardListResponseWithPagination struct {
	PublicDashboards []*PublicDashboardListResponse `json:"publicDashboards"`
	TotalCount       int64                          `json:"totalCount"`
	Page             int                            `json:"page"`
	PerPage          int                            `json:"perPage"`
}

type PublicDashboardListResponse struct {
	Uid          string `json:"uid" xorm:"uid"`
	AccessToken  string `json:"accessToken" xorm:"access_token"`
	Title        string `json:"title" xorm:"title"`
	DashboardUid string `json:"dashboardUid" xorm:"dashboard_uid"`
	IsEnabled    bool   `json:"isEnabled" xorm:"is_enabled"`
	Slug         string `json:"slug" xorm:"slug"`
}

type TimeSettings struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

func (ts *TimeSettings) FromDB(data []byte) error {
	return json.Unmarshal(data, ts)
}

func (ts *TimeSettings) ToDB() ([]byte, error) {
	return json.Marshal(ts)
}

// TemplateVariables records the values a viewer is allowed to choose for variables whose options
// cannot be read from the dashboard.
//
// A query variable's options come from running its query against the datasource, which is done by
// the datasource plugin in the browser and which a public dashboard viewer must never be able to
// trigger. So the author's browser resolves them while it is rendering the variable picker, and
// sends them when the public dashboard is saved. This is a snapshot: it stops reflecting the
// datasource as soon as the underlying data changes, and is refreshed when the author saves again.
//
// Options are keyed by variable name. Variables absent here fall back to the dashboard, which is
// the live source for custom and interval variables.
type TemplateVariables struct {
	Version int                 `json:"version"`
	Options map[string][]string `json:"options"`
}

// OptionsFor returns the recorded options for a variable, or nil when it has none.
func (tv TemplateVariables) OptionsFor(name string) []string {
	return tv.Options[name]
}

// IsEmpty reports whether anything was recorded, so callers can store NULL instead of an empty
// JSON object.
func (tv TemplateVariables) IsEmpty() bool {
	return len(tv.Options) == 0
}

func (tv *TemplateVariables) FromDB(data []byte) error {
	return json.Unmarshal(data, tv)
}

func (tv *TemplateVariables) ToDB() ([]byte, error) {
	return json.Marshal(tv)
}

// DTO for transforming user input in the api
type SavePublicDashboardDTO struct {
	Uid             string
	DashboardUid    string
	OrgID           int64
	UserId          int64
	PublicDashboard *PublicDashboardDTO
}

type TimeRangeDTO struct {
	From     string
	To       string
	Timezone string
}

type PublicDashboardQueryDTO struct {
	IntervalMs      int64
	MaxDataPoints   int64
	QueryCachingTTL int64
	TimeRange       TimeRangeDTO
	// Variables holds template variable overrides requested by the viewer, keyed by variable name.
	// Values are never trusted: they are checked against the options the dashboard author saved on
	// the variable before any of them reach a query. See internal/service/variables.go.
	Variables map[string][]string
}

type AnnotationsQueryDTO struct {
	From int64
	To   int64
}

//
// COMMANDS
//

type SavePublicDashboardCommand struct {
	PublicDashboard PublicDashboard
}
