import { type FormEvent, useId, useState } from 'react'

import type { StaffAction } from '@/data-source/data-source'

interface StaffPromptProps {
  plate: string
  enabledActions: StaffAction[]
  onConfirm: (plate: string) => Promise<void>
  onSendToPay: () => Promise<void>
}

export function StaffPrompt({
  plate,
  enabledActions,
  onConfirm,
  onSendToPay,
}: StaffPromptProps) {
  const fieldId = useId()
  const [typedPlate, setTypedPlate] = useState(plate)
  const [isSending, setIsSending] = useState(false)
  const [hasFailed, setHasFailed] = useState(false)

  async function answer(send: () => Promise<void>) {
    setIsSending(true)
    setHasFailed(false)
    try {
      await send()
    } catch {
      setHasFailed(true)
    } finally {
      setIsSending(false)
    }
  }

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    void answer(() => onConfirm(typedPlate.trim()))
  }

  const canConfirm =
    enabledActions.includes('confirm') && typedPlate.trim() !== '' && !isSending
  const canSendToPay = enabledActions.includes('send_to_pay') && !isSending

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-sm">
      <label htmlFor={fieldId} className="text-label text-staff">
        Confirm the plate
      </label>
      <input
        id={fieldId}
        value={typedPlate}
        onChange={(event) => setTypedPlate(event.target.value.toUpperCase())}
        autoComplete="off"
        spellCheck={false}
        className="rounded-card border-2 border-staff bg-surface px-sm py-xs font-mono text-code text-text"
      />
      <div className="flex gap-sm">
        <button
          type="submit"
          disabled={!canConfirm}
          className="rounded-pill bg-staff px-md py-sm text-body text-surface hover:opacity-90 disabled:opacity-40"
        >
          Confirm
        </button>
        <button
          type="button"
          disabled={!canSendToPay}
          onClick={() => void answer(onSendToPay)}
          className="rounded-pill bg-background px-md py-sm text-body text-text hover:text-accent disabled:opacity-40"
        >
          Send to pay
        </button>
      </div>
      {hasFailed && (
        <p role="alert" className="text-label text-error">
          {"The answer didn't reach the site agent. Try again."}
        </p>
      )}
    </form>
  )
}
