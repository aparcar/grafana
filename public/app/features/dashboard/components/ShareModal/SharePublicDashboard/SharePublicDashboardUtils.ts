import { type TypedVariableModel } from '@grafana/data';
import { config, DataSourceWithBackend, featureEnabled } from '@grafana/runtime';
import { getDataSourceInstance } from '@grafana/runtime/unstable';
import { getConfig } from 'app/core/config';

import { type PanelModel } from '../../../state/PanelModel';
import { shareDashboardType } from '../utils';

import { supportedDatasources } from './SupportedPubdashDatasources';

export enum PublicDashboardShareType {
  PUBLIC = 'public',
  EMAIL = 'email',
}

export interface PublicDashboardSettings {
  annotationsEnabled: boolean;
  isEnabled: boolean;
  timeSelectionEnabled: boolean;
  share: PublicDashboardShareType;
}

export interface PublicDashboard extends PublicDashboardSettings {
  accessToken?: string;
  uid: string;
  dashboardUid: string;
  timeSettings?: object;
  recipients?: Array<{ uid: string; recipient: string }>;
}

export interface SessionDashboard {
  dashboardTitle: string;
  dashboardUid: string;
  publicDashboardAccessToken: string;
  slug: string;
}

export interface SessionUser {
  email: string;
  firstSeenAtAge: string;
  lastSeenAtAge: string;
  totalDashboards: number;
}

// A viewer can pick a value for these through ?var-name=value, because the author enumerated the
// options up front and the backend can check a requested value against them.
const OVERRIDABLE_VARIABLE_TYPES = ['custom', 'interval'];

// These resolve to a fixed value that is always what the author configured, so freezing them on a
// public dashboard is not surprising.
const FIXED_VARIABLE_TYPES = ['constant', 'textbox'];

/**
 * True when a variable keeps working on a public dashboard, either because viewers can choose its
 * value or because it has a fixed one. Anything else (query, datasource, ad hoc, group by) is
 * frozen at the value the dashboard was saved with, which can go stale.
 */
export const isVariableSupportedOnPublicDashboard = (variable: { type: string }): boolean =>
  OVERRIDABLE_VARIABLE_TYPES.includes(variable.type) || FIXED_VARIABLE_TYPES.includes(variable.type);

// Instance methods
export const dashboardHasTemplateVariables = (variables: TypedVariableModel[]): boolean => {
  if (!config.featureToggles.publicDashboardsVariables) {
    return variables.length > 0;
  }

  return variables.some((variable) => !isVariableSupportedOnPublicDashboard(variable));
};

export const publicDashboardPersisted = (publicDashboard?: PublicDashboard): boolean => {
  return publicDashboard?.uid !== '' && publicDashboard?.uid !== undefined;
};

/**
 * Get unique datasource names from all panels that are not currently supported by public dashboards.
 */
export const getUnsupportedDashboardDatasources = async (panels: PanelModel[]): Promise<string[]> => {
  let unsupportedDS = new Set<string>();

  for (const panel of panels) {
    for (const target of panel.targets) {
      const dsType = target?.datasource?.type;
      if (dsType) {
        if (!supportedDatasources.has(dsType)) {
          unsupportedDS.add(dsType);
        } else {
          const ds = await getDataSourceInstance(target.datasource);
          if (!(ds instanceof DataSourceWithBackend)) {
            unsupportedDS.add(dsType);
          }
        }
      }
    }
  }

  return Array.from(unsupportedDS).sort();
};

/**
 * Generate the public dashboard url. Uses the appUrl from the Grafana boot config, so urls will also be correct
 * when Grafana is hosted on a subpath.
 *
 * All app urls from the Grafana boot config end with a slash.
 *
 * @param accessToken
 */
export const generatePublicDashboardUrl = (accessToken: string): string => {
  return `${getConfig().appUrl}public-dashboards/${accessToken}`;
};

export const generatePublicDashboardConfigUrl = (dashboardUid: string, dashboardName: string): string => {
  return `/d/${dashboardUid}/${dashboardName}?shareView=${shareDashboardType.publicDashboard}`;
};

export const validEmailRegex = /^[A-Z\d._%+-]+@[A-Z\d.-]+\.[A-Z]{2,}$/i;

export const isPublicDashboardsEnabled = () => {
  return config.publicDashboardsEnabled;
};

export const isEmailSharingEnabled = () =>
  isPublicDashboardsEnabled() &&
  !!config.featureToggles.publicDashboardsEmailSharing &&
  featureEnabled('publicDashboardsEmailSharing');
