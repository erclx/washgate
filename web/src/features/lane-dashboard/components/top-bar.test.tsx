import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { TopBar } from './top-bar'

const sites = [
  { id: 'site-1', name: 'Site 1' },
  { id: 'site-2', name: 'Site 2' },
]

function renderTopBar() {
  const onSiteChange = vi.fn()
  const onSearch = vi.fn()
  render(
    <TopBar
      sites={sites}
      siteId="site-1"
      onSiteChange={onSiteChange}
      onSearch={onSearch}
    />,
  )
  return { onSiteChange, onSearch }
}

describe('TopBar', () => {
  it('switches the view to the site picked', async () => {
    const user = userEvent.setup()
    const { onSiteChange } = renderTopBar()

    await user.selectOptions(screen.getByLabelText('Site'), 'Site 2')

    expect(onSiteChange).toHaveBeenCalledWith('site-2')
  })

  it('searches the plate typed into the search field', async () => {
    const user = userEvent.setup()
    const { onSearch } = renderTopBar()

    await user.type(
      screen.getByPlaceholderText('Search a plate'),
      'abc 123{Enter}',
    )

    expect(onSearch).toHaveBeenCalledWith('abc 123')
  })

  it('does not search an empty field', async () => {
    const user = userEvent.setup()
    const { onSearch } = renderTopBar()

    await user.type(screen.getByPlaceholderText('Search a plate'), '   {Enter}')

    expect(onSearch).not.toHaveBeenCalled()
  })
})
