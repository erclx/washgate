import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { PlateBadge } from './plate-badge'

describe('PlateBadge', () => {
  it('shows the plate in two groups of three', () => {
    render(<PlateBadge plate="ABC123" isUnsure={false} />)

    expect(screen.getByText('ABC 123')).toBeInTheDocument()
  })

  it('draws a dashed border on an unsure read', () => {
    render(<PlateBadge plate="MLB48Z" isUnsure />)

    expect(screen.getByText('MLB 48Z')).toHaveClass('border-dashed')
  })

  it('draws a solid border on a sure read', () => {
    render(<PlateBadge plate="ABC123" isUnsure={false} />)

    expect(screen.getByText('ABC 123')).toHaveClass('border-solid')
  })
})
