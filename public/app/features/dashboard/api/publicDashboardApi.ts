import { createApi } from '@reduxjs/toolkit/query/react';

import { createBaseQuery } from '@grafana/api-clients/rtkq';
import { t } from '@grafana/i18n';
import { config, type FetchError, isFetchError } from '@grafana/runtime';
import { createErrorNotification, createSuccessNotification } from 'app/core/copy/appNotification';
import { notifyApp } from 'app/core/reducers/appNotification';
import {
  type PublicDashboard,
  type PublicDashboardSettings,
  PublicDashboardShareType,
  type SessionDashboard,
  type SessionUser,
  type TemplateVariables,
} from 'app/features/dashboard/components/ShareModal/SharePublicDashboard/SharePublicDashboardUtils';
import { type DashboardModel } from 'app/features/dashboard/state/DashboardModel';
import { DashboardScene } from 'app/features/dashboard-scene/scene/DashboardScene';
import {
  type PublicDashboardListWithPagination,
  type PublicDashboardListWithPaginationResponse,
} from 'app/features/manage-dashboards/types';

function isFetchBaseQueryError(error: unknown): error is { error: FetchError } {
  return typeof error === 'object' && error != null && 'error' in error;
}

const ALL_VALUE = '$__all';

function toOptionValues(options: unknown): string[] {
  if (!Array.isArray(options)) {
    return [];
  }

  return options
    .map((option) => (option && typeof option === 'object' && 'value' in option ? option.value : undefined))
    .filter((value): value is string | number => typeof value === 'string' || typeof value === 'number')
    .map(String)
    .filter((value) => value !== ALL_VALUE);
}

/**
 * Records the resolved options of every query variable on the dashboard.
 *
 * A query variable's options come from running its query through the datasource, which only ever
 * happens in the browser. A public dashboard viewer must not be able to trigger that, so the
 * options are captured here, while an authenticated author is present, and the backend uses them
 * as the set of values a viewer is allowed to choose. They are a snapshot: a value that appears in
 * the datasource later is not selectable until the dashboard is shared again.
 *
 * Returns undefined when the dashboard has no query variables, which leaves anything already
 * recorded untouched.
 */
export function getRecordedTemplateVariables(
  dashboard: DashboardModel | DashboardScene | Pick<DashboardModel, 'uid'>
): TemplateVariables | undefined {
  const options: Record<string, string[]> = {};

  if (dashboard instanceof DashboardScene) {
    for (const variable of dashboard.state.$variables?.state.variables ?? []) {
      if (variable.state.type === 'query') {
        options[variable.state.name] = toOptionValues('options' in variable.state ? variable.state.options : undefined);
      }
    }
  } else if ('getVariables' in dashboard && typeof dashboard.getVariables === 'function') {
    for (const variable of dashboard.getVariables()) {
      if (variable.type === 'query') {
        options[variable.name] = toOptionValues('options' in variable ? variable.options : undefined);
      }
    }
  }

  return Object.keys(options).length ? { version: 1, options } : undefined;
}

export const getConfigError = (err: unknown) => ({
  error: isFetchError(err) && err.data.messageId !== 'publicdashboards.notFound' ? err : null,
});

export const publicDashboardApi = createApi({
  reducerPath: 'publicDashboardApi',
  baseQuery: createBaseQuery({ baseURL: '/api' }),
  tagTypes: ['PublicDashboard', 'AuditTablePublicDashboard', 'UsersWithActiveSessions', 'ActiveUserDashboards'],
  refetchOnMountOrArgChange: true,
  endpoints: (builder) => ({
    getPublicDashboard: builder.query<PublicDashboard | undefined, string>({
      query: (dashboardUid) => ({
        url: `/dashboards/uid/${dashboardUid}/public-dashboards`,
        manageError: getConfigError,
        showErrorAlert: false,
      }),
      async onQueryStarted(_, { dispatch, queryFulfilled }) {
        try {
          await queryFulfilled;
        } catch (e) {
          if (isFetchBaseQueryError(e) && isFetchError(e.error) && config.publicDashboardsEnabled) {
            dispatch(notifyApp(createErrorNotification(e.error.data.message)));
          }
        }
      },
      providesTags: (result, error, dashboardUid) => [{ type: 'PublicDashboard', id: dashboardUid }],
    }),
    createPublicDashboard: builder.mutation<
      PublicDashboard,
      { dashboard: DashboardModel | DashboardScene; payload: Partial<PublicDashboardSettings> }
    >({
      query: (params) => {
        const dashUid = params.dashboard instanceof DashboardScene ? params.dashboard.state.uid : params.dashboard.uid;
        return {
          url: `/dashboards/uid/${dashUid}/public-dashboards`,
          method: 'POST',
          body: {
            templateVariables: getRecordedTemplateVariables(params.dashboard),
            ...params.payload,
          },
        };
      },
      async onQueryStarted({ dashboard, payload: { share } }, { dispatch, queryFulfilled }) {
        const { data } = await queryFulfilled;
        const message =
          share === PublicDashboardShareType.PUBLIC
            ? t('public-dashboard.public-sharing.success-creation', 'Your dashboard is now publicly accessible')
            : t('public-dashboard.email-sharing.success-creation', 'Your dashboard is ready for external sharing');
        dispatch(notifyApp(createSuccessNotification(message)));

        if (dashboard instanceof DashboardScene) {
          dashboard.setState({
            meta: { ...dashboard.state.meta, publicDashboardEnabled: data.isEnabled },
          });
        } else {
          dashboard.updateMeta({
            publicDashboardEnabled: data.isEnabled,
          });
        }
      },
      invalidatesTags: (result, error, { dashboard }) => [
        { type: 'PublicDashboard', id: dashboard instanceof DashboardScene ? dashboard.state.uid : dashboard.uid },
      ],
    }),
    updatePublicDashboard: builder.mutation<
      PublicDashboard,
      {
        dashboard: (Pick<DashboardModel, 'uid'> & Partial<Pick<DashboardModel, 'updateMeta'>>) | DashboardScene;
        payload: Partial<PublicDashboard>;
      }
    >({
      query: ({ payload, dashboard }) => {
        const dashUid = dashboard instanceof DashboardScene ? dashboard.state.uid : dashboard.uid;
        return {
          url: `/dashboards/uid/${dashUid}/public-dashboards/${payload.uid}`,
          method: 'PATCH',
          // Refresh the recorded query variable options on every save, so re-sharing is how an
          // author picks up values that have appeared in the datasource since.
          body: {
            templateVariables: getRecordedTemplateVariables(dashboard),
            ...payload,
          },
        };
      },
      async onQueryStarted({ dashboard }, { dispatch, queryFulfilled }) {
        await queryFulfilled;
        dispatch(
          notifyApp(
            createSuccessNotification(
              t('public-dashboard.configuration.success-update', 'Settings have been successfully updated')
            )
          )
        );
      },
      invalidatesTags: (result, error, { payload }) => [
        { type: 'PublicDashboard', id: payload.dashboardUid },
        'AuditTablePublicDashboard',
      ],
    }),
    pauseOrResumePublicDashboard: builder.mutation<
      PublicDashboard,
      {
        dashboard: (Pick<DashboardModel, 'uid'> & Partial<Pick<DashboardModel, 'updateMeta'>>) | DashboardScene;
        payload: Partial<PublicDashboard>;
      }
    >({
      query: ({ payload, dashboard }) => {
        const dashUid = dashboard instanceof DashboardScene ? dashboard.state.uid : dashboard.uid;
        return {
          url: `/dashboards/uid/${dashUid}/public-dashboards/${payload.uid}`,
          method: 'PATCH',
          body: payload,
        };
      },
      async onQueryStarted({ dashboard, payload: { isEnabled } }, { dispatch, queryFulfilled }) {
        const { data } = await queryFulfilled;
        const message = isEnabled
          ? t('public-dashboard.configuration.success-resume', 'Your dashboard access has been resumed')
          : t('public-dashboard.configuration.success-pause', 'Your dashboard access has been paused');
        dispatch(notifyApp(createSuccessNotification(message)));

        if (dashboard instanceof DashboardScene) {
          dashboard.setState({
            meta: { ...dashboard.state.meta, publicDashboardEnabled: data.isEnabled },
          });
        } else {
          dashboard.updateMeta?.({
            publicDashboardEnabled: data.isEnabled,
          });
        }
      },
      invalidatesTags: (result, error, { payload }) => [
        { type: 'PublicDashboard', id: payload.dashboardUid },
        'AuditTablePublicDashboard',
      ],
    }),
    updatePublicDashboardAccess: builder.mutation<
      PublicDashboard,
      {
        dashboard: (Pick<DashboardModel, 'uid'> & Partial<Pick<DashboardModel, 'updateMeta'>>) | DashboardScene;
        payload: Partial<PublicDashboard>;
      }
    >({
      query: ({ payload, dashboard }) => {
        const dashUid = dashboard instanceof DashboardScene ? dashboard.state.uid : dashboard.uid;
        return {
          url: `/dashboards/uid/${dashUid}/public-dashboards/${payload.uid}`,
          method: 'PATCH',
          body: payload,
        };
      },
      async onQueryStarted({ dashboard, payload: { share } }, { dispatch, queryFulfilled }) {
        await queryFulfilled;
        const message =
          share === PublicDashboardShareType.PUBLIC
            ? t(
                'public-dashboard.public-sharing.success-share-type-change',
                'Dashboard access updated: Anyone with the link can now access'
              )
            : t(
                'public-dashboard.email-sharing.success-share-type-change',
                'Dashboard access updated: Only specific people can now access with the link'
              );
        dispatch(notifyApp(createSuccessNotification(message)));
      },
      invalidatesTags: (result, error, { payload }) => [
        { type: 'PublicDashboard', id: payload.dashboardUid },
        'AuditTablePublicDashboard',
      ],
    }),
    addRecipient: builder.mutation<void, { recipient: string; dashboardUid: string; uid: string }>({
      query: () => ({
        url: '',
      }),
    }),
    deleteRecipient: builder.mutation<
      void,
      { recipientUid: string; recipientEmail: string; dashboardUid: string; uid: string }
    >({
      query: () => ({
        url: '',
      }),
    }),
    reshareAccessToRecipient: builder.mutation<void, { recipientUid: string; uid: string }>({
      query: () => ({
        url: '',
      }),
    }),
    getActiveUsers: builder.query<SessionUser[], void>({
      query: () => ({
        url: '/',
      }),
      providesTags: ['UsersWithActiveSessions'],
    }),
    getActiveUserDashboards: builder.query<SessionDashboard[], string>({
      query: () => ({
        url: '',
      }),
      providesTags: (result, _, email) => [{ type: 'ActiveUserDashboards', id: email }],
    }),
    listPublicDashboards: builder.query<PublicDashboardListWithPagination, number | void>({
      query: (page = 1) => ({
        url: `/dashboards/public-dashboards?page=${page}&perpage=8`,
      }),
      transformResponse: (response: PublicDashboardListWithPaginationResponse) => ({
        ...response,
        totalPages: Math.ceil(response.totalCount / response.perPage),
      }),
      providesTags: ['AuditTablePublicDashboard'],
    }),
    deletePublicDashboard: builder.mutation<
      void,
      { dashboard?: DashboardModel | DashboardScene; dashboardUid: string; uid: string }
    >({
      query: (params) => ({
        url: `/dashboards/uid/${params.dashboardUid}/public-dashboards/${params.uid}`,
        method: 'DELETE',
      }),
      async onQueryStarted({ dashboard }, { dispatch, queryFulfilled }) {
        await queryFulfilled;
        dispatch(
          notifyApp(
            createSuccessNotification(
              t('public-dashboard.share.success-delete', 'Your dashboard is no longer shareable')
            )
          )
        );
        dispatch(publicDashboardApi.util?.resetApiState());

        if (dashboard instanceof DashboardScene) {
          dashboard.setState({
            meta: { ...dashboard.state.meta, publicDashboardEnabled: false },
          });
        } else {
          dashboard?.updateMeta({
            publicDashboardEnabled: false,
          });
        }
      },
      invalidatesTags: (result, error, { dashboardUid }) => [
        { type: 'PublicDashboard', id: dashboardUid },
        'AuditTablePublicDashboard',
        'UsersWithActiveSessions',
        'ActiveUserDashboards',
      ],
    }),
    revokeAllAccess: builder.mutation<void, { email: string }>({
      query: () => ({
        url: '',
      }),
    }),
  }),
});

export const {
  useGetPublicDashboardQuery,
  useCreatePublicDashboardMutation,
  useUpdatePublicDashboardMutation,
  useDeletePublicDashboardMutation,
  useListPublicDashboardsQuery,
  useAddRecipientMutation,
  useDeleteRecipientMutation,
  useReshareAccessToRecipientMutation,
  useGetActiveUsersQuery,
  useGetActiveUserDashboardsQuery,
  useRevokeAllAccessMutation,
  usePauseOrResumePublicDashboardMutation,
  useUpdatePublicDashboardAccessMutation,
} = publicDashboardApi;
