import assert from 'node:assert/strict';
import test from 'node:test';

import {buildBalancerView} from './balancer-view.js';

test('builds a simple capacity-to-weight summary', () => {
  const view = buildBalancerView(
    [
      {instanceId: 'app-1', effectiveCapacity: 100},
      {instanceId: 'app-2', effectiveCapacity: 100},
      {instanceId: 'app-3', effectiveCapacity: 30},
    ],
    {
      status: 'stable',
      targetWeights: [
        {instanceId: 'app-1', weight: 10},
        {instanceId: 'app-2', weight: 10},
        {instanceId: 'app-3', weight: 3},
      ],
    },
  );
  assert.equal(view.statusText, '自适应权重已稳定');
  assert.equal(view.capacityRatio, '100:100:30');
  assert.equal(view.weightRatio, '10:10:3');
  assert.equal(view.targetFor('app-3'), 3);
});

test('explains that overloaded capacity cannot be fixed by weights', () => {
  const view = buildBalancerView([], {
    status: 'degraded',
    capacityNotice: 'cluster_overloaded',
    lastError: {code: 'NGINX_CONFIG_INVALID', message: 'retry later'},
  });
  assert.match(view.notice, /调整权重无法创造容量/);
  assert.equal(view.error.code, 'NGINX_CONFIG_INVALID');
});
