import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { buildDecision, buildStaffDecision } from '@/test/factories'

import { CurrentCar } from './current-car'

describe('CurrentCar', () => {
  it('shows the photo, the plate read with its confidence, the decision, and the trace', () => {
    render(
      <CurrentCar
        decision={buildDecision({ has_photo: true, confidence: 0.97 })}
        photoUrl="/photo.jpg"
        staffPrompt={null}
      />,
    )

    expect(
      screen.getByRole('img', { name: 'Lane photo of ABC 123' }),
    ).toHaveAttribute('src', '/photo.jpg')
    expect(screen.getByText('Confidence 0.97')).toBeInTheDocument()
    expect(screen.getByText('Admit')).toBeInTheDocument()
    expect(screen.getByRole('heading', { name: 'Trace' })).toBeInTheDocument()
  })

  it('shows the staff prompt in place of the trace for a staff decision', () => {
    render(
      <CurrentCar
        decision={buildStaffDecision()}
        photoUrl={null}
        staffPrompt={<p>staff prompt</p>}
      />,
    )

    expect(screen.getByText('staff prompt')).toBeInTheDocument()
    expect(
      screen.queryByRole('heading', { name: 'Trace' }),
    ).not.toBeInTheDocument()
  })

  it('says no photo is held when the site kept none', () => {
    render(
      <CurrentCar
        decision={buildDecision({ has_photo: false })}
        photoUrl={null}
        staffPrompt={null}
      />,
    )

    expect(screen.getByText('No photo held')).toBeInTheDocument()
  })
})
