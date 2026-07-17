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
  await expect(trafficPanel.getByText(/已处理\s+60/)).toBeVisible();

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
  await expect(appTwo.getByText(/容量 30/)).toBeVisible();

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
  await expect(appThree.getByText(/容量 30/)).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');

  await controls.getByRole('button', {name: '自适应', exact: true}).click();
  await expect(page.getByText('切换负载均衡模式', {exact: true})).toBeVisible();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  await expect(controls.getByText('自适应权重已稳定', {exact: true})).toBeVisible({
    timeout: 10_000,
  });
  await expect(controls.getByText('容量 100:100:30 → 权重 10:10:3', {exact: true})).toBeVisible();

  const topologyAppThree = page.locator('.topology-panel .resource-node').filter({hasText: 'app-3'});
  await expect(topologyAppThree.locator('dd').last()).toHaveText('3');

  await controls.getByRole('button', {name: '固定', exact: true}).click();
  await expect(page.locator('.operation-row .status-badge')).toHaveText('succeeded');
  await appThree.getByRole('combobox').selectOption('40');
  await appThree.getByRole('button', {name: '应用'}).click();
  await expect(appThree.getByText(/容量 40/)).toBeVisible();
  await expect(topologyAppThree.locator('dd').last()).toHaveText('3');

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
  await expect(appTwo.getByText(/容量 30/)).toBeVisible();
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
  await expect(controls.getByText('容量 100:30 → 权重 10:3', {exact: true})).toBeVisible();

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
