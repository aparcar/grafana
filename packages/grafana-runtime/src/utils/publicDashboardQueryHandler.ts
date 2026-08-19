import { catchError, type Observable, of, switchMap } from 'rxjs';

import { type DataQuery, type DataQueryRequest, type DataQueryResponse } from '@grafana/data';

import { config } from '../config';
import { locationService } from '../services/LocationService';
import { getBackendSrv } from '../services/backendSrv';

import { type BackendDataSourceResponse, toDataQueryResponse } from './queryResponse';

const VARIABLE_PREFIX = 'var-';

/**
 * Collects template variable overrides from the URL, e.g. ?var-dev=01.
 *
 * Only the overrides are sent; the backend fills in the saved value for every other variable and
 * checks each override against the options the dashboard author saved. Queries are built from the
 * stored dashboard, so nothing here can widen what the viewer is able to ask for.
 */
function getVariablesFromUrl(): Record<string, string[]> | undefined {
  const variables: Record<string, string[]> = {};

  for (const [key, value] of locationService.getSearch().entries()) {
    if (!key.startsWith(VARIABLE_PREFIX)) {
      continue;
    }
    const name = key.slice(VARIABLE_PREFIX.length);
    if (!name) {
      continue;
    }
    // Repeated params are how multi-value variables arrive: ?var-dev=01&var-dev=02
    variables[name] = variables[name] ? [...variables[name], value] : [value];
  }

  return Object.keys(variables).length ? variables : undefined;
}

export function publicDashboardQueryHandler(request: DataQueryRequest<DataQuery>): Observable<DataQueryResponse> {
  const {
    intervalMs,
    maxDataPoints,
    requestId,
    panelId,
    queryCachingTTL,
    range: { from: fromRange, to: toRange },
  } = request;
  // Return early if no queries exist
  if (!request.targets.length) {
    return of({ data: [] });
  }

  if (panelId == null || Number.isNaN(panelId)) {
    return of({ data: [] });
  }

  const body = {
    intervalMs,
    maxDataPoints,
    queryCachingTTL,
    timeRange: {
      from: fromRange.valueOf().toString(),
      to: toRange.valueOf().toString(),
      timezone: request.timezone,
    },
    variables: getVariablesFromUrl(),
  };

  return getBackendSrv()
    .fetch<BackendDataSourceResponse>({
      url: `/api/public/dashboards/${config.publicDashboardAccessToken!}/panels/${panelId}/query`,
      method: 'POST',
      data: body,
      requestId,
    })
    .pipe(
      switchMap((raw) => {
        return of(toDataQueryResponse(raw, request.targets));
      }),
      catchError((err) => {
        return of(toDataQueryResponse(err));
      })
    );
}
