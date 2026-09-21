import { expect, test } from '@playwright/test'

test.describe('replay build', () => {
  test('shows the first recorded decision as the current car', async ({
    page,
  }) => {
    await page.goto('/')

    await expect(page.getByText('Replay of a recorded run')).toBeVisible()
    await expect(
      page.getByRole('article', { name: 'Current car' }),
    ).toContainText('Premium, wash 3 of 8 this month')
  })

  test('accepts the recorded staff action and plays its resolution', async ({
    page,
  }) => {
    await page.clock.install()
    await page.goto('/')
    await expect(page.getByText('Outbox 0')).toBeVisible()
    await page.clock.runFor(13_000)

    const currentCar = page.getByRole('article', { name: 'Current car' })
    await expect(
      page.getByRole('button', { name: 'Send to pay' }),
    ).toBeDisabled()
    await page.getByLabel('Confirm the plate').fill('MLB482')
    await page.getByRole('button', { name: 'Confirm' }).click()

    await expect(currentCar).toContainText('Confirmed by staff')
  })
})
