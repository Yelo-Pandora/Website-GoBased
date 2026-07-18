export function createBall(id, requestUnits) {
  return {
    id,
    requestUnits,
    displayUnits: requestUnits,
    phase: 'moving_to_gateway',
    arrivedAtGateway: false,
    result: null,
    targetInstanceId: '',
    kind: 'request',
  };
}

export function arriveAtGateway(ball) {
  const next = {...ball, arrivedAtGateway: true, phase: 'waiting_response'};
  return next.result ? resolveBall(next) : [next];
}

export function receiveResult(ball, result) {
  const next = {...ball, result};
  return next.arrivedAtGateway ? resolveBall(next) : [next];
}

export function failBall(ball) {
  return [{...ball, phase: 'error', kind: 'error'}];
}

function resolveBall(ball) {
  const {result} = ball;
  if (result.status === 'accepted') {
    return [{
      ...ball,
      phase: 'moving_to_instance',
      displayUnits: result.acceptedUnits,
      targetInstanceId: result.targetInstanceId,
      kind: 'accepted',
    }];
  }
  if (result.status === 'dropped') {
    return [{
      ...ball,
      phase: 'dropped',
      displayUnits: result.droppedUnits,
      kind: 'dropped',
    }];
  }
  return [
    {
      ...ball,
      id: `${ball.id}-accepted`,
      phase: 'moving_to_instance',
      displayUnits: result.acceptedUnits,
      targetInstanceId: result.targetInstanceId,
      kind: 'accepted',
    },
    {
      ...ball,
      id: `${ball.id}-dropped`,
      phase: 'dropped',
      displayUnits: result.droppedUnits,
      kind: 'dropped',
    },
  ];
}

export function estimateInstanceState(state, now = Date.now()) {
  const observedAt = new Date(state?.observedAt || '').getTime();
  const processingSpeed = Number(state?.processingSpeed || 0);
  const maxLoad = Number(state?.maxLoad || 0);
  const observedLoad = Number(state?.currentLoad || 0);
  if (!Number.isFinite(observedAt) || processingSpeed <= 0 || maxLoad <= 0) {
    return state || {};
  }
  const elapsedSeconds = Math.max(0, (now - observedAt) / 1000);
  const currentLoad = Math.max(0, observedLoad - processingSpeed * elapsedSeconds);
  const loadRatio = currentLoad / maxLoad;
  return {
    ...state,
    currentLoad,
    loadRatio,
    loadState: loadState(loadRatio),
  };
}

function loadState(loadRatio) {
  if (loadRatio <= 0.3) return 'idle';
  if (loadRatio <= 0.7) return 'normal';
  if (loadRatio < 1) return 'high';
  return 'overloaded';
}
