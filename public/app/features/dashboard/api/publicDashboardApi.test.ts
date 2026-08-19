import { type DashboardModel } from 'app/features/dashboard/state/DashboardModel';
import { DashboardScene } from 'app/features/dashboard-scene/scene/DashboardScene';

import { getRecordedTemplateVariables } from './publicDashboardApi';

// getRecordedTemplateVariables narrows with `instanceof DashboardScene`, so the stand-in has to
// share the prototype. `state` is a getter on SceneObjectBase and cannot simply be assigned.
function asScene(variables: Array<{ type: string; name: string; options?: unknown }>): DashboardScene {
  const scene = Object.create(DashboardScene.prototype);
  Object.defineProperty(scene, 'state', {
    value: { $variables: { state: { variables: variables.map((state) => ({ state })) } } },
  });
  return scene;
}

function asModel(variables: Array<{ type: string; name: string; options?: unknown }>): DashboardModel {
  return { getVariables: () => variables } as unknown as DashboardModel;
}

describe('getRecordedTemplateVariables', () => {
  it('records nothing when the dashboard has no query variables', () => {
    expect(getRecordedTemplateVariables(asScene([{ type: 'custom', name: 'dev' }]))).toBeUndefined();
    expect(getRecordedTemplateVariables(asScene([]))).toBeUndefined();
  });

  it('records the resolved options of a query variable', () => {
    const recorded = getRecordedTemplateVariables(
      asScene([{ type: 'query', name: 'host', options: [{ value: 'web-1' }, { value: 'web-2' }] }])
    );

    expect(recorded).toEqual({ version: 1, options: { host: ['web-1', 'web-2'] } });
  });

  it('does not record custom variables, whose options the backend reads from the dashboard', () => {
    const recorded = getRecordedTemplateVariables(
      asScene([
        { type: 'query', name: 'host', options: [{ value: 'web-1' }] },
        { type: 'custom', name: 'dev', options: [{ value: '01' }] },
      ])
    );

    expect(recorded?.options).toEqual({ host: ['web-1'] });
  });

  it('drops the All sentinel, which is not a value in its own right', () => {
    const recorded = getRecordedTemplateVariables(
      asScene([{ type: 'query', name: 'host', options: [{ value: '$__all' }, { value: 'web-1' }] }])
    );

    expect(recorded?.options.host).toEqual(['web-1']);
  });

  it('coerces non-string option values', () => {
    const recorded = getRecordedTemplateVariables(
      asScene([{ type: 'query', name: 'port', options: [{ value: 80 }, { value: 443 }] }])
    );

    expect(recorded?.options.port).toEqual(['80', '443']);
  });

  it('records an empty list so that a query variable losing all its options clears the old one', () => {
    const recorded = getRecordedTemplateVariables(asScene([{ type: 'query', name: 'host', options: [] }]));

    expect(recorded).toEqual({ version: 1, options: { host: [] } });
  });

  it('reads the legacy dashboard model too', () => {
    const recorded = getRecordedTemplateVariables(
      asModel([{ type: 'query', name: 'host', options: [{ value: 'web-1' }] }])
    );

    expect(recorded).toEqual({ version: 1, options: { host: ['web-1'] } });
  });

  it('tolerates a dashboard reference with no variables at all', () => {
    expect(getRecordedTemplateVariables({ uid: 'abc' })).toBeUndefined();
  });
});
