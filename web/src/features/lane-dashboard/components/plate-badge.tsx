import { formatPlate } from '../format'

interface PlateBadgeProps {
  plate: string
  isUnsure: boolean
}

export function PlateBadge({ plate, isUnsure }: PlateBadgeProps) {
  return (
    <span
      className={`inline-block w-fit rounded-plate border-2 bg-surface px-sm py-xs font-mono text-code text-text ${
        isUnsure ? 'border-dashed border-staff' : 'border-solid border-text'
      }`}
    >
      {formatPlate(plate)}
    </span>
  )
}
