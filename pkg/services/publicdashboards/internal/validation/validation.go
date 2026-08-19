package validation

import (
	"github.com/google/uuid"
	"github.com/grafana/grafana-plugin-sdk-go/backend/gtime"
	"github.com/grafana/grafana/pkg/services/publicdashboards/internal/models"
	"github.com/grafana/grafana/pkg/util"
)

// Bounds on the recorded variable options. They are generous enough for a realistic variable and
// exist so that a buggy or hostile client cannot fill the template_variables column, which is a
// mediumtext and would otherwise accept megabytes of JSON per public dashboard.
const (
	maxRecordedVariables     = 100
	maxRecordedOptionsPerVar = 5000
	maxRecordedOptionLength  = 1000
)

func ValidatePublicDashboard(dto *models.SavePublicDashboardDTO) error {
	// if it is empty we override it in the service with public for retro compatibility
	if dto.PublicDashboard.Share != "" && !IsValidShareType(dto.PublicDashboard.Share) {
		return models.ErrInvalidShareType.Errorf("ValidateSavePublicDashboard: invalid share type")
	}

	return ValidateTemplateVariables(dto.PublicDashboard.TemplateVariables)
}

// ValidateTemplateVariables bounds the option lists a client asks us to persist.
func ValidateTemplateVariables(tv *models.TemplateVariables) error {
	if tv == nil {
		return nil
	}

	if len(tv.Options) > maxRecordedVariables {
		return models.ErrInvalidTemplateVariables.Errorf("ValidateTemplateVariables: too many variables, got %d and the limit is %d", len(tv.Options), maxRecordedVariables)
	}

	for name, options := range tv.Options {
		if len(options) > maxRecordedOptionsPerVar {
			return models.ErrInvalidTemplateVariables.Errorf("ValidateTemplateVariables: variable %q has %d options and the limit is %d", name, len(options), maxRecordedOptionsPerVar)
		}

		for _, option := range options {
			if len(option) > maxRecordedOptionLength {
				return models.ErrInvalidTemplateVariables.Errorf("ValidateTemplateVariables: an option for variable %q is longer than the limit of %d", name, maxRecordedOptionLength)
			}
		}
	}

	return nil
}

func ValidateQueryPublicDashboardRequest(req models.PublicDashboardQueryDTO, pd *models.PublicDashboard) error {
	if req.IntervalMs < 0 {
		return models.ErrInvalidInterval.Errorf("ValidateQueryPublicDashboardRequest: intervalMS should be greater than 0")
	}

	if req.MaxDataPoints < 0 {
		return models.ErrInvalidMaxDataPoints.Errorf("ValidateQueryPublicDashboardRequest: maxDataPoints should be greater than 0")
	}

	if pd.TimeSelectionEnabled {
		timeRange := gtime.NewTimeRange(req.TimeRange.From, req.TimeRange.To)

		_, err := timeRange.ParseFrom()
		if err != nil {
			return models.ErrInvalidTimeRange.Errorf("ValidateQueryPublicDashboardRequest: time range from is invalid")
		}
		_, err = timeRange.ParseTo()
		if err != nil {
			return models.ErrInvalidTimeRange.Errorf("ValidateQueryPublicDashboardRequest: time range to is invalid")
		}
	}

	return nil
}

// IsValidAccessToken asserts that an accessToken is a valid uuid
func IsValidAccessToken(token string) bool {
	_, err := uuid.Parse(token)
	return err == nil
}

// IsValidShortUID checks that the uid is not blank and contains valid
// characters. Wraps utils.IsValidShortUID
func IsValidShortUID(uid string) bool {
	return uid != "" && util.IsValidShortUID(uid)
}

func IsValidShareType(shareType models.ShareType) bool {
	for _, t := range models.ValidShareTypes {
		if t == shareType {
			return true
		}
	}
	return false
}
