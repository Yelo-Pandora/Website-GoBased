import assert from 'node:assert/strict';
import test from 'node:test';

import {
  arriveAtGateway,
  createBall,
  estimateInstanceState,
  failBall,
  receiveResult,
} from './traffic-state.js';

const partialResult = {
  status: 'partially_accepted',
  acceptedUnits: 6,
  droppedUnits: 4,
  targetInstanceId: 'app-2',
};

test('waits at gateway when animation arrives first', () => {
  const [waiting] = arriveAtGateway(createBall('batch-1', 10));
  assert.equal(waiting.phase, 'waiting_response');
  const resolved = receiveResult(waiting, partialResult);
  assert.deepEqual(resolved.map((ball) => ball.displayUnits), [6, 4]);
  assert.deepEqual(resolved.map((ball) => ball.kind), ['accepted', 'dropped']);
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

test('estimates continuous load decay from the latest observation', () => {
  const state = estimateInstanceState({
    processingSpeed: 20,
    maxLoad: 100,
    currentLoad: 80,
    observedAt: '2026-07-17T12:00:00.000Z',
  }, Date.parse('2026-07-17T12:00:02.000Z'));
  assert.equal(state.currentLoad, 40);
  assert.equal(state.loadRatio, 0.4);
  assert.equal(state.loadState, 'normal');
});

test('never estimates a negative load', () => {
  const state = estimateInstanceState({
    processingSpeed: 20,
    maxLoad: 100,
    currentLoad: 10,
    observedAt: '2026-07-17T12:00:00.000Z',
  }, Date.parse('2026-07-17T12:00:02.000Z'));
  assert.equal(state.currentLoad, 0);
  assert.equal(state.loadRatio, 0);
  assert.equal(state.loadState, 'idle');
});
