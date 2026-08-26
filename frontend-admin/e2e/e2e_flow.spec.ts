import { expect, test } from '@playwright/test'

test.describe('Mini Eureka dashboard', () => {
  test('renders the real cluster snapshot, semantic statuses and instance details', async ({ page, request }) => {
    const snapshotResponse = await request.get('/api/v1/dashboard/snapshot')
    expect(snapshotResponse.ok()).toBeTruthy()
    const snapshot = await snapshotResponse.json()
    expect(snapshot.data.instances.length).toBeGreaterThan(0)

    await page.goto('/')
    await expect(page.getByRole('heading', { level: 1, name: 'Mini Eureka' })).toBeVisible()
    await expect(page.getByRole('heading', { name: '实例状态全景墙' })).toBeVisible()
    await expect(page.getByRole('region', { name: '集群概览' })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Gossip 传播事件' })).toBeVisible()

    const status = page.locator('.status-badge').first()
    await expect(status).toBeVisible()
    await expect(status).toContainText(/活跃|心跳延迟|已摘除/)

    const renderedTiles = page.locator('.instance-tile')
    expect(await renderedTiles.count()).toBeLessThan(300)
    await page.locator('.instance-primary').first().click()
    const drawer = page.getByRole('dialog')
    await expect(drawer).toBeVisible()
    await expect(drawer.getByText('版本与租约')).toBeVisible()
    await page.getByRole('button', { name: '关闭实例详情' }).click()
    await expect(drawer).toBeHidden()
  })

  test('searches instances and keeps destructive simulation behind confirmation', async ({ page }) => {
    await page.goto('/')
    const firstTile = page.locator('.instance-tile').first()
    await expect(firstTile).toBeVisible()
    const instanceID = (await firstTile.locator('.instance-primary strong').textContent())?.trim()
    expect(instanceID).toBeTruthy()

    await page.getByRole('searchbox', { name: '搜索服务或实例 ID' }).fill(instanceID!)
    await expect(page.locator('.instance-tile')).toHaveCount(1)
    await expect(page.locator('.instance-primary strong')).toHaveText(instanceID!)

    await page.getByRole('searchbox', { name: '搜索服务或实例 ID' }).fill('')
    const offline = page.getByRole('button', { name: /^模拟下线 / }).first()
    if (await offline.count()) {
      await offline.click()
      const dialog = page.getByRole('dialog', { name: '确认模拟实例下线' })
      await expect(dialog).toBeVisible()
      await expect(dialog).toContainText('真实 Gossip 传播')
      await dialog.getByRole('button', { name: '取消' }).click()
      await expect(dialog).toBeHidden()
    }
  })

  test('fits a narrow viewport and honors reduced-motion preference', async ({ page }) => {
    await page.setViewportSize({ width: 480, height: 820 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/')
    await expect(page.getByRole('heading', { name: '实例状态全景墙' })).toBeVisible()
    const dimensions = await page.evaluate(() => ({
      client: document.documentElement.clientWidth,
      scroll: document.documentElement.scrollWidth,
    }))
    expect(dimensions.scroll).toBeLessThanOrEqual(dimensions.client + 1)

    const motionDurationSeconds = await page.locator('.instance-tile').first().evaluate((element) => {
      const durations = getComputedStyle(element).transitionDuration.split(',')
      return Math.max(...durations.map((duration) => {
        const value = duration.trim()
        const numeric = Number.parseFloat(value)
        return value.endsWith('ms') ? numeric / 1_000 : numeric
      }))
    })
    expect(motionDurationSeconds).toBeLessThanOrEqual(1e-6)
  })

  test('confirms a demo offline action and traces its real Gossip delivery', async ({ page }) => {
    await page.goto('/')
    const offline = page.getByRole('button', { name: /^模拟下线 / }).first()
    test.skip(await offline.count() === 0, 'the demo dataset has no remaining online instance')
    await offline.click()
    const dialog = page.getByRole('dialog', { name: '确认模拟实例下线' })
    const responsePromise = page.waitForResponse((response) =>
      response.request().method() === 'POST'
      && response.url().includes('/api/v1/demo/services/')
      && response.url().endsWith('/offline'),
    )
    await dialog.getByRole('button', { name: '模拟下线' }).click()
    const response = await responsePromise
    expect(response.status()).toBe(202)
    const payload = await response.json()
    const eventID = payload.data.event_id as string
    await expect(page.getByText(eventID, { exact: false })).toBeVisible()
    await expect(page.locator(`[data-event-id="${eventID}"]`).first()).toBeVisible({ timeout: 15_000 })
  })
})
