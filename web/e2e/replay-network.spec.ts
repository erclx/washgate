import { expect, type Request,test } from '@playwright/test'

const staticResourceTypes = new Set([
  'document',
  'script',
  'stylesheet',
  'image',
  'font',
  'manifest',
])
const recordingLengthMs = 40_000

function describeStrayRequest(request: Request, pageOrigin: string) {
  const isSameOrigin = new URL(request.url()).origin === pageOrigin
  const isStatic = staticResourceTypes.has(request.resourceType())
  return isSameOrigin && isStatic
    ? null
    : `${request.method()} ${request.url()} (${request.resourceType()})`
}

test.describe('replay build network silence', () => {
  test('plays the whole recording with only static requests to its own origin', async ({
    page,
    baseURL,
  }) => {
    const pageOrigin = new URL(baseURL ?? '').origin
    const strayRequests: string[] = []
    let requestCount = 0
    page.on('request', (request) => {
      requestCount += 1
      const stray = describeStrayRequest(request, pageOrigin)
      if (stray) strayRequests.push(stray)
    })

    await page.clock.install()
    await page.goto('/')
    await expect(page.getByText('Replay of a recorded run')).toBeVisible()
    await page.clock.runFor(recordingLengthMs)
    await page.waitForLoadState('networkidle')

    expect(requestCount).toBeGreaterThan(0)
    expect(strayRequests).toEqual([])
  })
})
