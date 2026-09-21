import type { DataSource } from '@/data-source/data-source'

import rawRecording from './fixtures/demo-session.json'
import { parseRecording } from './recording'
import { createReplayDataSource } from './replay-data-source'

const photoFiles = import.meta.glob<string>('./fixtures/photos/*.jpg', {
  eager: true,
  query: '?url',
  import: 'default',
})

function photosByDecisionId(): Record<string, string> {
  return Object.fromEntries(
    Object.entries(photoFiles).map(([path, url]) => [
      path.slice(path.lastIndexOf('/') + 1, -'.jpg'.length),
      url,
    ]),
  )
}

export function createDemoReplayDataSource(): DataSource {
  return createReplayDataSource({
    recording: parseRecording(rawRecording),
    photos: photosByDecisionId(),
  })
}
