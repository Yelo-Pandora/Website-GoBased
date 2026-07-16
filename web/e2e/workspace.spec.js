import {expect, test} from '@playwright/test';

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
  await expect(page.getByText('实验网关')).toBeVisible();
  await expect(page.getByText('app-1', {exact: true})).toBeVisible();
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
