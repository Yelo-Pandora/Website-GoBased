# 阶段十 OpenAPI 实验动作契约收敛设计

## 目标

使公开 OpenAPI 契约完整描述平台已经实现的九个实验动作，补齐阶段九的单实例 L1 和会话 Redis
层级操作，不修改现有运行时行为。

## 方案

`LabTopologyActionRequest` 使用三个互斥的 `oneOf` 分支：

* 集群拓扑与负载均衡动作沿用现有五个动作及参数结构。
* `REMOVE_INSTANCE_L1` 和 `ADD_INSTANCE_L1` 必须提供字符串 `targetInstanceId`，参数为空对象。
* `REMOVE_SESSION_REDIS` 和 `ADD_SESSION_REDIS` 的 `targetInstanceId` 必须为 `null`，参数为空对象。

三个分支的 `actionType` 枚举互不重叠，客户端和 Swagger 可以据此识别请求结构。

## 验证

契约测试解析组件 schema，递归收集三个分支的动作枚举，断言九个运行时动作全部存在；同时断言
L1 与 Redis 分支的目标字段类型分别为 `string` 和 `null`。现有路径与 OpenAPI 版本检查继续保留。
