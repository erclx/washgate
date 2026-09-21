// The site agent and the invoicing month both run on Stockholm time, so the dashboard shows the site's clock.
const SITE_TIME_ZONE = 'Europe/Stockholm'

const timeFormat = new Intl.DateTimeFormat('en-GB', {
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
  timeZone: SITE_TIME_ZONE,
})

const dayFormat = new Intl.DateTimeFormat('en-GB', {
  day: '2-digit',
  month: 'numeric',
  timeZone: SITE_TIME_ZONE,
})

// Short month names vary by ICU build ("Sep" or "Sept"), so the dashboard spells its own.
const MONTHS = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
]

export function formatTime(timestamp: string): string {
  return timeFormat.format(new Date(timestamp))
}

export function formatDay(timestamp: string): string {
  const parts = dayFormat.formatToParts(new Date(timestamp))
  const day = parts.find((part) => part.type === 'day')?.value
  const month = Number(parts.find((part) => part.type === 'month')?.value)
  return `${day} ${MONTHS[month - 1]}`
}

export function formatAge(ageMs: number): string {
  const seconds = Math.max(0, Math.floor(ageMs / 1000))
  if (seconds < 60) return `${seconds} s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} min`
  return `${Math.floor(minutes / 60)} h`
}

export function formatPlate(plate: string): string {
  return plate.length === 6 ? `${plate.slice(0, 3)} ${plate.slice(3)}` : plate
}
