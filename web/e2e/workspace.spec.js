import {expect, test} from '@playwright/test';
import {execFile} from 'node:child_process';
import path from 'node:path';
import {promisify} from 'node:util';

const execFileAsync = promisify(execFile);
const workspaceRoot = path.resolve(import.meta.dirname, '..', '..');

async function login(page) {
  await page.goto('/');
  await page.getByLabel('账号').fill('learner');
  await page.getByLabel('密码').fill('example-password');
  await page.getByRole('button', {name: '登录', exact: true}).click();
  await expect(page.getByRole('tab', {name: '实验'})).toBeVisible();
}

async function terminateActiveLab(page) {
  const terminate = page.getByRole('button', {name: '结束', exact: true});
  if (await terminate.isEnabled().catch(() => false)) {
    page.once('dialog', (dialog) => dialog.accept());
    await terminate.click();
    await expect(page.getByText('已结束', {exact: true})).toBeVisible();
  }
}

async function currentSnapshot(page) {
  const labId = (await page.locator('.topology-panel code').textContent())?.trim();
  return page.evaluate(async (id) => {
    const response = await fetch(`/api/v1/labs/${id}`);
    const body = await response.json();
    return body.data;
  }, labId);
}

test('learner completes the stage 4-6 lab lifecycle', async ({page}) => {
  await login(page);
  await terminateActiveLab(page);

  await page.getByRole('button', {name: /应用集群与负载均衡/}).click();
  await page.getByRole('button', {name: /创建实验|新建实验/}).click();

  await expect(page.getByText('运行中', {exact: true})).toBeVisible();
  await expect(page.getByRole('region', {name: '实验拓扑'}).getByText('实验网关')).toBeVisible();
  await expect(page.getByRole('region', {name: '实验拓扑'}).getByText('app-1', {exact: true})).toBeVisible();
  await expect(page.getByText('空闲时限')).toBeVisible();
  await expect(page.getByText('最长时限')).toBeVisible();
  await expect(page.locator('.deadline-item__header strong').first()).not.toHaveText('--:--');
  if (process.env.PLAYWRIGHT_SCREENSHOT_DIR) {
    await page.screenshot({
      path: `${process.env.PLAYWRIGHT_SCREENSHOT_DIR}/stage46-workspace-desktop.png`,
      fullPage: true,
    });
  }

  await page.getByRole('button', {name: '重置', exact: true}).click();
  await expect(page.getByText('重置实验', {exact: true})).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  await expect(page.getByText('运行中', {exact: true})).toBeVisible();

  await page.getByRole('tab', {name: 'API'}).click();
  await expect(page.getByText('Platform API', {exact: false}).first()).toBeVisible();
  await page.getByRole('tab', {name: '实验'}).click();

  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', {name: '结束', exact: true}).click();
  await expect(page.getByText('已结束', {exact: true})).toBeVisible();
  await expect(page.getByText('用户主动结束', {exact: true})).toBeVisible();
});

test('workspace remains usable at a mobile viewport', async ({page}) => {
  await page.setViewportSize({width: 390, height: 844});
  await login(page);
  await expect(page.getByRole('tab', {name: '实验'})).toBeVisible();
  await expect(page.getByText('实验课程')).toBeVisible();
  if (process.env.PLAYWRIGHT_SCREENSHOT_DIR) {
    await page.screenshot({
      path: `${process.env.PLAYWRIGHT_SCREENSHOT_DIR}/stage46-workspace-mobile.png`,
      fullPage: true,
    });
  }
  const dimensions = await page.evaluate(() => ({
    clientWidth: document.documentElement.clientWidth,
    scrollWidth: document.documentElement.scrollWidth,
  }));
  expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth);
});

test('learner drives real traffic and fixed cluster controls', async ({page}) => {
  await login(page);
  await terminateActiveLab(page);
  await page.getByRole('button', {name: /应用集群与负载均衡/}).click();
  await page.getByRole('button', {name: /创建实验|新建实验/}).click();
  await expect(page.getByText('运行中', {exact: true})).toBeVisible();

  const trafficPanel = page.locator('.traffic-panel');
  await expect(trafficPanel.getByText('真实请求流')).toBeVisible();
  await trafficPanel.getByRole('button', {name: '发送一批'}).click();
  await expect(trafficPanel.getByText(/已接纳\s+10/)).toBeVisible();

  const controls = page.locator('.cluster-controls');
  await controls.getByRole('button', {name: '增加实例'}).click();
  await expect(page.getByText('app-2', {exact: true}).first()).toBeVisible();
  await expect(page.getByText('增加实例', {exact: true}).last()).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');

  const weightInputs = controls.locator('.weight-editor input');
  await weightInputs.nth(0).fill('20');
  await weightInputs.nth(1).fill('80');
  await controls.getByRole('button', {name: '应用权重'}).click();
  await expect(page.getByText('调整固定权重', {exact: true})).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');

  const appTwo = controls.locator('.cluster-instance-row').filter({hasText: 'app-2'});
  await appTwo.getByRole('combobox').selectOption('30');
  await appTwo.getByRole('button', {name: '应用'}).click();
  await expect(page.getByText('调整实例性能', {exact: true})).toBeVisible();
  await expect(appTwo.getByText(/处理速度 6\/秒/)).toBeVisible();

  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', {name: '结束', exact: true}).click();
  await expect(page.getByText('已结束', {exact: true})).toBeVisible();
});

test('manual traffic batch does not create another generation loop', async ({page}) => {
  await login(page);
  await terminateActiveLab(page);
  await page.getByRole('button', {name: /应用集群与负载均衡/}).click();
  await page.getByRole('button', {name: /创建实验|新建实验/}).click();
  await expect(page.getByText('运行中', {exact: true})).toBeVisible();

  const trafficRequests = [];
  page.on('request', (request) => {
    const path = new URL(request.url()).pathname;
    if (request.method() === 'POST' && path.endsWith('/traffic-batches')) {
      trafficRequests.push(Date.now());
    }
  });

  const trafficPanel = page.locator('.traffic-panel');
  await trafficPanel.getByLabel('批次间隔').selectOption('1000');
  await trafficPanel.getByRole('button', {name: '开始', exact: true}).click();
  await expect.poll(() => trafficRequests.length).toBeGreaterThanOrEqual(1);

  await page.waitForTimeout(400);
  await trafficPanel.getByRole('button', {name: '发送一批', exact: true}).click();
  await expect.poll(() => trafficRequests.length).toBeGreaterThanOrEqual(2);
  await page.waitForTimeout(2200);
  expect(trafficRequests).toHaveLength(4);

  await trafficPanel.getByRole('button', {name: '停止', exact: true}).click();
  const stoppedRequestCount = trafficRequests.length;
  await page.waitForTimeout(1200);
  expect(trafficRequests).toHaveLength(stoppedRequestCount);

  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', {name: '结束', exact: true}).click();
  await expect(page.getByText('已结束', {exact: true})).toBeVisible();
});

test('lab editors survive snapshots and reset after structural changes', async ({page}) => {
  test.setTimeout(90_000);
  await login(page);
  await terminateActiveLab(page);
  await page.getByRole('button', {name: /应用集群与负载均衡/}).click();
  await page.getByRole('button', {name: /创建实验|新建实验/}).click();
  await expect(page.getByText('运行中', {exact: true})).toBeVisible();

  const trafficPanel = page.locator('.traffic-panel');
  const controls = page.locator('.cluster-controls');
  const requestUnits = trafficPanel.getByLabel('每批等效订单');
  const generationInterval = trafficPanel.getByLabel('批次间隔');
  const appOne = controls.locator('.cluster-instance-row').filter({hasText: 'app-1'});

  await requestUnits.fill('25');
  await generationInterval.selectOption('1000');
  await appOne.getByRole('combobox').selectOption('30');
  await controls.locator('.weight-editor input').first().fill('70');

  const labId = (await page.locator('.topology-panel code').textContent())?.trim();
  await Promise.all([
    page.waitForResponse((response) => {
      const path = new URL(response.url()).pathname;
      return response.request().method() === 'GET' && path === `/api/v1/labs/${labId}`;
    }),
    page.locator('.workspace-toolbar .icon-button').click(),
  ]);
  await expect(requestUnits).toHaveValue('25');
  await expect(generationInterval).toHaveValue('1000');
  await expect(appOne.getByRole('combobox')).toHaveValue('30');
  await expect(controls.locator('.weight-editor input').first()).toHaveValue('70');

  await controls.getByRole('button', {name: '增加实例'}).click();
  await expect(page.getByText('app-2', {exact: true}).first()).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  await expect(requestUnits).toHaveValue('10');
  await expect(generationInterval).toHaveValue('250');

  let snapshot = await currentSnapshot(page);
  for (const instance of snapshot.topology.instances) {
    const row = controls.locator('.cluster-instance-row').filter({hasText: instance.instanceId});
    await expect(row.getByRole('combobox')).toHaveValue(String(instance.performancePercent));
  }
  const expandedWeights = controls.locator('.weight-editor input');
  for (const [index, instance] of snapshot.topology.instances.entries()) {
    await expect(expandedWeights.nth(index)).toHaveValue(String(instance.currentWeight));
  }

  await requestUnits.fill('25');
  await generationInterval.selectOption('1000');
  await appOne.getByRole('combobox').selectOption('30');
  await expandedWeights.first().fill('70');
  await controls.getByRole('button', {name: '自适应', exact: true}).click();
  await expect(controls.getByRole('button', {name: '自适应', exact: true}))
    .toHaveAttribute('aria-pressed', 'true');
  await expect(requestUnits).toHaveValue('10');
  await expect(generationInterval).toHaveValue('250');
  await expect(appOne.getByRole('combobox')).toHaveValue('100');

  await controls.getByRole('button', {name: '固定', exact: true}).click();
  await expect(controls.getByRole('button', {name: '固定', exact: true}))
    .toHaveAttribute('aria-pressed', 'true');
  snapshot = await currentSnapshot(page);
  const fixedWeights = controls.locator('.weight-editor input');
  for (const [index, instance] of snapshot.topology.instances.entries()) {
    await expect(fixedWeights.nth(index)).toHaveValue(String(instance.currentWeight));
  }

  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', {name: '结束', exact: true}).click();
  await expect(page.getByText('已结束', {exact: true})).toBeVisible();
  await page.getByRole('button', {name: '新建实验', exact: true}).click();
  await expect(page.getByText('运行中', {exact: true})).toBeVisible();
  await expect(trafficPanel.getByLabel('每批等效订单')).toHaveValue('10');
  await expect(trafficPanel.getByLabel('批次间隔')).toHaveValue('250');

  snapshot = await currentSnapshot(page);
  const newAppOne = controls.locator('.cluster-instance-row').filter({hasText: 'app-1'});
  await expect(newAppOne.getByRole('combobox'))
    .toHaveValue(String(snapshot.topology.instances[0].performancePercent));
  await expect(controls.locator('.weight-editor input').first())
    .toHaveValue(String(snapshot.topology.instances[0].currentWeight));

  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', {name: '结束', exact: true}).click();
  await expect(page.getByText('已结束', {exact: true})).toBeVisible();
});

test('adaptive weights converge without generated traffic', async ({page}) => {
  await login(page);
  await terminateActiveLab(page);
  await page.getByRole('button', {name: /应用集群与负载均衡/}).click();
  await page.getByRole('button', {name: /创建实验|新建实验/}).click();
  await expect(page.getByText('运行中', {exact: true})).toBeVisible();

  const controls = page.locator('.cluster-controls');
  await controls.getByRole('button', {name: '增加实例'}).click();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  await controls.getByRole('button', {name: '增加实例'}).click();
  await expect(page.getByText('app-3', {exact: true}).first()).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');

  const appThree = controls.locator('.cluster-instance-row').filter({hasText: 'app-3'});
  await appThree.getByRole('combobox').selectOption('30');
  await appThree.getByRole('button', {name: '应用'}).click();
  await expect(appThree.getByText(/处理速度 6\/秒/)).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');

  await controls.getByRole('button', {name: '自适应', exact: true}).click();
  await expect(page.getByText('切换负载均衡模式', {exact: true})).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  await expect(controls.getByText('自适应权重已稳定', {exact: true})).toBeVisible({
    timeout: 10_000,
  });
  await expect(controls.getByText('处理速度 20:20:6 → 权重 10:10:3', {exact: true})).toBeVisible();

  const topologyAppThree = page.locator('.topology-panel .resource-node').filter({hasText: 'app-3'});
  await expect(topologyAppThree.locator('dd').last()).toHaveText('3');

  await controls.getByRole('button', {name: '固定', exact: true}).click();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  await appThree.getByRole('combobox').selectOption('40');
  await appThree.getByRole('button', {name: '应用'}).click();
  await expect(appThree.getByText(/处理速度 8\/秒/)).toBeVisible();
  await expect(topologyAppThree.locator('dd').last()).toHaveText('3');

  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', {name: '结束', exact: true}).click();
  await expect(page.getByText('已结束', {exact: true})).toBeVisible();
});

test('adaptive feedback reduces traffic share for a loaded slow instance', async ({page}) => {
  test.setTimeout(60_000);
  await login(page);
  await terminateActiveLab(page);
  await page.getByRole('button', {name: /应用集群与负载均衡/}).click();
  await page.getByRole('button', {name: /创建实验|新建实验/}).click();
  await expect(page.getByText('运行中', {exact: true})).toBeVisible();

  const controls = page.locator('.cluster-controls');
  await controls.getByRole('button', {name: '增加实例'}).click();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  await controls.getByRole('button', {name: '增加实例'}).click();
  await expect(page.getByText('app-3', {exact: true}).first()).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');

  const appThree = controls.locator('.cluster-instance-row').filter({hasText: 'app-3'});
  await appThree.getByRole('combobox').selectOption('30');
  await appThree.getByRole('button', {name: '应用'}).click();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');

  const trafficPanel = page.locator('.traffic-panel');
  await trafficPanel.getByRole('button', {name: '开始', exact: true}).click();
  await expect.poll(async () => {
    const snapshot = await currentSnapshot(page);
    const ratios = new Map(
      snapshot.topology.instances.map((instance) => [instance.instanceId, instance.loadRatio]),
    );
    return (ratios.get('app-3') || 0) - Math.max(
      ratios.get('app-1') || 0,
      ratios.get('app-2') || 0,
    );
  }, {timeout: 20_000}).toBeGreaterThan(0.2);

  await controls.getByRole('button', {name: '自适应', exact: true}).click();
  await expect(page.getByText('切换负载均衡模式', {exact: true})).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  await expect.poll(async () => {
    const snapshot = await currentSnapshot(page);
    if (!snapshot.balancer) return 1;
    const weights = new Map(
      snapshot.balancer.targetWeights.map((item) => [item.instanceId, item.weight]),
    );
    const total = [...weights.values()].reduce((sum, weight) => sum + weight, 0);
    return total > 0 ? (weights.get('app-3') || 0) / total : 1;
  }, {timeout: 20_000}).toBeLessThan(0.1);

  await trafficPanel.getByRole('button', {name: '停止', exact: true}).click();
  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', {name: '结束', exact: true}).click();
  await expect(page.getByText('已结束', {exact: true})).toBeVisible();
});

test('adaptive mode recovers after a platform API restart', async ({page}) => {
  test.skip(!process.env.RUN_PLATFORM_RESTART, 'restarts the platform API container');
  test.setTimeout(90_000);
  await login(page);
  await terminateActiveLab(page);
  await page.getByRole('button', {name: /应用集群与负载均衡/}).click();
  await page.getByRole('button', {name: /创建实验|新建实验/}).click();
  await expect(page.getByText('运行中', {exact: true})).toBeVisible();

  const controls = page.locator('.cluster-controls');
  await controls.getByRole('button', {name: '增加实例'}).click();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  const appTwo = controls.locator('.cluster-instance-row').filter({hasText: 'app-2'});
  await appTwo.getByRole('combobox').selectOption('30');
  await appTwo.getByRole('button', {name: '应用'}).click();
  await expect(appTwo.getByText(/处理速度 6\/秒/)).toBeVisible();
  await controls.getByRole('button', {name: '自适应', exact: true}).click();
  await expect(controls.getByText('自适应权重已稳定', {exact: true})).toBeVisible({
    timeout: 10_000,
  });

  await execFileAsync('docker', ['compose', 'restart', 'platform-api'], {cwd: workspaceRoot});
  await execFileAsync(
    'docker',
    ['compose', 'up', '-d', '--wait', 'platform-api'],
    {cwd: workspaceRoot},
  );
  await page.reload();
  await expect(page.getByRole('button', {name: '自适应', exact: true})).toHaveAttribute(
    'aria-pressed',
    'true',
  );
  await expect(controls.getByText('自适应权重已稳定', {exact: true})).toBeVisible({
    timeout: 10_000,
  });
  await expect(controls.getByText('处理速度 20:6 → 权重 10:3', {exact: true})).toBeVisible();

  page.once('dialog', (dialog) => dialog.accept());
  await page.getByRole('button', {name: '结束', exact: true}).click();
  await expect(page.getByText('已结束', {exact: true})).toBeVisible();
});

test('learner observes automatic idle expiration', async ({page}) => {
  test.skip(!process.env.RUN_SHORT_LIFECYCLE, 'requires shortened lifecycle configuration');
  await login(page);
  await terminateActiveLab(page);
  await page.getByRole('button', {name: /应用集群与负载均衡/}).click();
  await page.getByRole('button', {name: /创建实验|新建实验/}).click();
  await expect(page.getByText('运行中', {exact: true})).toBeVisible();
  await expect(page.getByText('即将过期', {exact: true})).toBeVisible({timeout: 35_000});
  await expect(page.getByText('已结束', {exact: true})).toBeVisible({timeout: 35_000});
  await expect(page.getByText('空闲超时', {exact: true})).toBeVisible();
});
