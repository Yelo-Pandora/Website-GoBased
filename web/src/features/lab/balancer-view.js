const STATUS_TEXT = {
  converging: '自适应调整中',
  stable: '自适应权重已稳定',
  degraded: '自适应调整异常，将自动重试',
};

const CAPACITY_NOTICE_TEXT = {
  cluster_overloaded: '当前负载超过集群容量，调整权重无法创造容量。请扩容、恢复性能或降低流量。',
  cluster_underused: '当前集群容量明显高于负载，可以继续观察或尝试缩容。',
};

export function buildBalancerView(instances, balancer) {
  const targets = new Map(
    (balancer?.targetWeights || []).map((item) => [item.instanceId, item.weight]),
  );
  return {
    status: balancer?.status || 'converging',
    statusText: STATUS_TEXT[balancer?.status] || STATUS_TEXT.converging,
    processingSpeedRatio: instances.map((instance) => instance.processingSpeed).join(':'),
    weightRatio: instances.map((instance) => targets.get(instance.instanceId) ?? '—').join(':'),
    targetFor(instanceId) {
      return targets.get(instanceId) ?? '—';
    },
    notice: CAPACITY_NOTICE_TEXT[balancer?.capacityNotice] || '',
    error: balancer?.lastError || null,
  };
}
