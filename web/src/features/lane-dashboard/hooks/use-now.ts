import { useEffect, useState } from 'react'

export function useNow(intervalMs = 1000): number {
  const [now, setNow] = useState(Date.now)

  useEffect(() => {
    const interval = setInterval(() => setNow(Date.now()), intervalMs)
    return () => clearInterval(interval)
  }, [intervalMs])

  return now
}
