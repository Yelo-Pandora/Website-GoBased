import assert from 'node:assert/strict';
import test from 'node:test';

import {arriveAtGateway, createBall, failBall, receiveResult} from './traffic-state.js';

const partialResult = {
  status: 'partially_processed',
  processedUnits: 6,
  droppedUnits: 4,
  targetInstanceId: 'app-2',
};

test('waits at gateway when animation arrives first', () => {
  const [waiting] = arriveAtGateway(createBall('batch-1', 10));
  assert.equal(waiting.phase, 'waiting_response');
  const resolved = receiveResult(waiting, partialResult);
  assert.deepEqual(resolved.map((ball) => ball.displayUnits), [6, 4]);
  assert.deepEqual(resolved.map((ball) => ball.kind), ['processed', 'dropped']);
});

test('caches response until animation reaches gateway', () => {
  const [moving] = receiveResult(createBall('batch-1', 10), partialResult);
  assert.equal(moving.phase, 'moving_to_gateway');
  const resolved = arriveAtGateway(moving);
  assert.equal(resolved[0].targetInstanceId, 'app-2');
});

test('distinguishes transport errors from dropped orders', () => {
  const [failed] = failBall(createBall('batch-1', 10));
  assert.equal(failed.kind, 'error');
  assert.equal(failed.phase, 'error');
});
