import { describe, expect, it } from 'vitest'

import { formatAge, formatDay, formatPlate, formatTime } from './format'

describe('formatAge', () => {
  it.each([
    [2_400, '2 s'],
    [125_000, '2 min'],
    [7_300_000, '2 h'],
  ])('reads %i ms as %s', (ageMs, text) => {
    expect(formatAge(ageMs)).toBe(text)
  })
})

describe('formatPlate', () => {
  it('splits a six-character plate into two groups of three', () => {
    expect(formatPlate('ABC123')).toBe('ABC 123')
  })

  it('leaves a plate of another length as read', () => {
    expect(formatPlate('AB12')).toBe('AB12')
  })
})

describe('site-local dates', () => {
  it('shows a decision time on the site clock', () => {
    expect(formatTime('2026-09-21T12:31:52Z')).toBe('14:31:52')
  })

  it('shows a wash day as day and short month', () => {
    expect(formatDay('2026-09-12T08:10:00Z')).toBe('12 Sep')
  })
})
