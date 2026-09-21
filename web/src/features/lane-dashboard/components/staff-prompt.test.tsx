import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import type { StaffAction } from '@/data-source/data-source'

import { StaffPrompt } from './staff-prompt'

function renderPrompt({
  enabledActions = ['confirm', 'send_to_pay'] as StaffAction[],
  onConfirm = vi.fn(async () => {}),
  onSendToPay = vi.fn(async () => {}),
} = {}) {
  render(
    <StaffPrompt
      plate="MLB48Z"
      enabledActions={enabledActions}
      onConfirm={onConfirm}
      onSendToPay={onSendToPay}
    />,
  )
  return { onConfirm, onSendToPay }
}

describe('StaffPrompt', () => {
  it('prefills the plate the reader returned', () => {
    renderPrompt()

    expect(screen.getByLabelText('Confirm the plate')).toHaveValue('MLB48Z')
  })

  it('sends the corrected plate on confirm', async () => {
    const user = userEvent.setup()
    const { onConfirm } = renderPrompt()

    await user.clear(screen.getByLabelText('Confirm the plate'))
    await user.type(screen.getByLabelText('Confirm the plate'), 'MLB482')
    await user.click(screen.getByRole('button', { name: 'Confirm' }))

    expect(onConfirm).toHaveBeenCalledWith('MLB482')
  })

  it('routes the car to pay on send to pay', async () => {
    const user = userEvent.setup()
    const { onSendToPay } = renderPrompt()

    await user.click(screen.getByRole('button', { name: 'Send to pay' }))

    expect(onSendToPay).toHaveBeenCalledOnce()
  })

  it('disables an action the data source does not allow', () => {
    renderPrompt({ enabledActions: ['confirm'] })

    expect(screen.getByRole('button', { name: 'Send to pay' })).toBeDisabled()
  })

  it('says so when the answer does not reach the site agent', async () => {
    const user = userEvent.setup()
    renderPrompt({
      onSendToPay: vi.fn(async () => Promise.reject(new Error('down'))),
    })

    await user.click(screen.getByRole('button', { name: 'Send to pay' }))

    expect(await screen.findByRole('alert')).toHaveTextContent(
      "The answer didn't reach the site agent. Try again.",
    )
  })
})
