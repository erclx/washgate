import { readFile } from 'node:fs/promises'

import { expect, test } from '@playwright/test'

const stackUrl = process.env.WASHGATE_STACK_URL
const plateReaderUrl = process.env.PLATE_READER_URL ?? 'http://localhost:8000'
const fixturePhoto = new URL(
  '../../plate-reader/tests/fixtures/31-Volvo-S90-3.0-15285826772-.jpg',
  import.meta.url,
)

test.describe('lane decision against the compose stack', () => {
  test.skip(
    !stackUrl,
    'Set WASHGATE_STACK_URL to the running web service, such as http://localhost:8090',
  )

  test('shows a photo read at the lane as a pay decision with its trace', async ({
    page,
    request,
    browserName,
  }) => {
    test.skip(browserName !== 'chromium', 'One lane read per run is enough')
    test.slow()
    await page.goto(stackUrl ?? '/')

    const response = await request.post(`${plateReaderUrl}/read`, {
      data: await readFile(fixturePhoto),
      headers: { 'Content-Type': 'image/jpeg' },
    })
    expect(response.ok()).toBe(true)

    const currentCar = page.getByRole('article', { name: 'Current car' })
    await expect(currentCar).toContainText('EEK 828')
    await expect(currentCar).toContainText('Pay')
    await expect(
      currentCar.getByRole('heading', { name: 'Trace' }),
    ).toBeVisible()
  })
})
