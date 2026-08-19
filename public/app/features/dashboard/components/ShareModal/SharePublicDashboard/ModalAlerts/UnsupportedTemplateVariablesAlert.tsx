import { selectors as e2eSelectors } from '@grafana/e2e-selectors';
import { Trans, t } from '@grafana/i18n';
import { config } from '@grafana/runtime';
import { Alert } from '@grafana/ui';

const selectors = e2eSelectors.pages.ShareDashboardModal.PublicDashboard;

export const UnsupportedTemplateVariablesAlert = ({ showDescription = true }: { showDescription?: boolean }) => {
  // With variable support on, the remaining variables are not unsupported so much as frozen: they
  // still resolve, but only ever to the value the dashboard was saved with.
  const variablesEnabled = config.featureToggles.publicDashboardsVariables;

  return (
    <Alert
      severity="warning"
      title={
        variablesEnabled
          ? t(
              'public-dashboard.modal-alerts.locked-template-variable-alert-title',
              'Some template variables are locked'
            )
          : t(
              'public-dashboard.modal-alerts.unsupported-template-variable-alert-title',
              'Template variables are not supported'
            )
      }
      data-testid={selectors.TemplateVariablesWarningAlert}
      bottomSpacing={0}
    >
      {showDescription &&
        (variablesEnabled ? (
          <Trans i18nKey="public-dashboard.modal-alerts.locked-template-variable-alert-desc">
            Query, data source and ad hoc variables keep the value this dashboard was saved with. Only custom and
            interval variables can be changed by viewers, through the dashboard URL.
          </Trans>
        ) : (
          <Trans i18nKey="public-dashboard.modal-alerts.unsupported-template-variable-alert-desc">
            This public dashboard may not work since it uses template variables
          </Trans>
        ))}
    </Alert>
  );
};
