# 阶段七真实用户验收指南

## 验收范围

阶段七完成应用集群与固定权重闭环：浏览器串行生成教学等效请求批次，
请求真实经过 Edge、Platform API、Lab Gateway 和 Lab App。后端决定实际实例、
接纳量、丢弃量和负载状态，前端只根据同步结果播放动画。

## 标准流程

1. 登录测试账号并创建“应用集群与负载均衡”实验。
2. 在“真实请求流”发送一批 10 个教学等效订单。
3. 确认小球经过实验网关并进入响应指定的实例。
4. 增加 `app-2`，等待最新操作变为 `succeeded`。
5. 将固定权重调整为 `app-1 = 20`、`app-2 = 80`。
6. 将 `app-2` 性能调整为 30%，确认处理速度降为 6，最大负载仍为 100。
7. 连续发送批次，观察 `accepted`、`partially_accepted` 或 `dropped`。
8. 确认 `acceptedUnits + droppedUnits = receivedUnits`。
9. 删除一个实例，确认集群仍保留至少一台应用。
10. 结束实验并确认受管容器、网络、Nginx 片段和实验数据库被清理。

活动实验期间重建 Lab Gateway 时，上游使用 Docker DNS 动态解析；生命周期资源核对会把
重建后的 Gateway 和 MySQL 重新接入预期实验网络，避免旧片段阻止网关启动。共享片段卷
固定由编排器 UID `10001` 写入，Gateway 只读加载，单独重建不会改变写入权限。

固定权重范围为 1 至 100，表示 Nginx 相对权重，不要求总和等于 100。
单实验最多四台应用，最少保留一台。

## 自动化验证

```powershell
go test ./...
go vet ./...
npm --prefix web test
npm --prefix web run build
npm --prefix web run test:e2e
docker compose config --quiet
```

E2E 覆盖真实批次、扩容、固定权重、性能调整、生命周期和 390px 移动视口。
短生命周期自动回收用例需要额外设置 `RUN_SHORT_LIFECYCLE=1`。
