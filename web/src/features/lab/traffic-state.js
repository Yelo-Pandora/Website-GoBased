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
  if (result.status === 'processed') {
    return [{
      ...ball,
      phase: 'moving_to_instance',
      displayUnits: result.processedUnits,
      targetInstanceId: result.targetInstanceId,
      kind: 'processed',
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
      id: `${ball.id}-processed`,
      phase: 'moving_to_instance',
      displayUnits: result.processedUnits,
      targetInstanceId: result.targetInstanceId,
      kind: 'processed',
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
