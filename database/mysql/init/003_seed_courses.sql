USE platform;

INSERT INTO courses (
  slug,
  title,
  category,
  status,
  sort_order,
  summary
) VALUES
  (
    'standalone-architecture',
    '单机架构',
    'foundation',
    'theory',
    10,
    '理解应用、数据和入口集中在单机时的职责与限制。'
  ),
  (
    'application-data-separation',
    '应用与数据分离',
    'application',
    'active',
    20,
    '观察独立 Go 应用容器通过网络访问共享 MySQL。'
  ),
  (
    'application-cluster',
    '应用集群与负载均衡',
    'application',
    'active',
    30,
    '通过一至四个应用容器和内部 Nginx 观察容量与流量分配。'
  ),
  (
    'multi-level-cache',
    '多级缓存',
    'cache',
    'active',
    40,
    '观察应用进程内 L1、会话 Redis L2 和 MySQL 回源链路。'
  ),
  (
    'cache-failures',
    '缓存故障',
    'cache',
    'active',
    50,
    '演示缓存穿透、击穿、雪崩和会话 Redis 重启。'
  ),
  (
    'database-read-write-splitting',
    '数据库读写分离',
    'database',
    'coming_soon',
    60,
    '后续课程占位。'
  ),
  (
    'database-sharding',
    '分库分表与分布式数据库',
    'database',
    'coming_soon',
    70,
    '后续课程占位。'
  ),
  (
    'cdn-reverse-proxy',
    'CDN 与反向代理',
    'network',
    'coming_soon',
    80,
    '后续课程占位。'
  ),
  (
    'search-and-nosql',
    '搜索引擎与 NoSQL',
    'storage',
    'coming_soon',
    90,
    '后续课程占位。'
  ),
  (
    'distributed-systems',
    '业务拆分与分布式系统',
    'distributed',
    'coming_soon',
    100,
    '后续课程占位。'
  ),
  (
    'microservices',
    '微服务架构',
    'distributed',
    'coming_soon',
    110,
    '后续课程占位。'
  ),
  (
    'containers-and-cloud',
    '容器化与云平台',
    'operations',
    'coming_soon',
    120,
    '后续课程占位。'
  )
ON DUPLICATE KEY UPDATE
  title = VALUES(title),
  category = VALUES(category),
  status = VALUES(status),
  sort_order = VALUES(sort_order),
  summary = VALUES(summary);
