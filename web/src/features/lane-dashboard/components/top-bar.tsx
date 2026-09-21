import { type FormEvent, useId, useState } from 'react'

import type { Site } from '@/data-source/data-source'

interface TopBarProps {
  sites: Site[]
  siteId: string
  onSiteChange: (siteId: string) => void
  onSearch: (plate: string) => void
}

export function TopBar({ sites, siteId, onSiteChange, onSearch }: TopBarProps) {
  const siteFieldId = useId()
  const [query, setQuery] = useState('')

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (query.trim() !== '') onSearch(query.trim())
  }

  return (
    <header className="flex flex-wrap items-center gap-lg border-b border-border bg-surface px-lg py-sm">
      <h1 className="font-display text-heading text-accent">washgate</h1>
      <label htmlFor={siteFieldId} className="sr-only">
        Site
      </label>
      <select
        id={siteFieldId}
        value={siteId}
        onChange={(event) => onSiteChange(event.target.value)}
        className="rounded-card border border-border bg-surface px-sm py-xs text-body text-text"
      >
        {sites.map((site) => (
          <option key={site.id} value={site.id}>
            {site.name}
          </option>
        ))}
      </select>
      <form
        role="search"
        onSubmit={handleSubmit}
        className="mx-auto w-full max-w-[24rem]"
      >
        <input
          type="search"
          aria-label="Search a plate"
          placeholder="Search a plate"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          className="w-full rounded-card border border-border bg-surface px-sm py-xs font-mono text-code text-text placeholder:font-sans placeholder:text-muted"
        />
      </form>
    </header>
  )
}
