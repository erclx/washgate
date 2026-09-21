import { useState } from 'react'

import type { Site } from '@/data-source/data-source'
import { useDataSource } from '@/data-source/data-source-context'

import { CurrentCar } from './components/current-car'
import { EarlierCars } from './components/earlier-cars'
import { HealthStrip } from './components/health-strip'
import { LookupPanel, type LookupState } from './components/lookup-panel'
import { StaffPrompt } from './components/staff-prompt'
import { TopBar } from './components/top-bar'
import { useLookup } from './hooks/use-lookup'
import { useNow } from './hooks/use-now'
import { useSiteLane } from './hooks/use-site-lane'

interface SiteLaneProps {
  site: Site
  sites: Site[]
  lookupState: LookupState | null
  onCloseLookup: () => void
}

function SiteLane({ site, sites, lookupState, onCloseLookup }: SiteLaneProps) {
  const { lane, site: siteData } = useDataSource()
  const { feed, status, isAgentDown } = useSiteLane(site.id)
  const now = useNow()
  const [hasLinkFailed, setHasLinkFailed] = useState(false)

  async function handleToggleLink(isCut: boolean) {
    setHasLinkFailed(false)
    try {
      await siteData.setLinkCut(site.id, isCut)
    } catch {
      setHasLinkFailed(true)
    }
  }

  const current = feed.current
  return (
    <>
      <HealthStrip
        status={status}
        isAgentDown={isAgentDown}
        canSwitchLink={siteData.canSwitchLink()}
        onToggleLink={(isCut) => void handleToggleLink(isCut)}
        now={now}
      />
      {hasLinkFailed && (
        <p role="alert" className="px-lg pt-sm text-label text-error">
          {"The link switch didn't reach the site agent. Try again."}
        </p>
      )}
      <main className="relative grid grid-cols-1 gap-lg p-lg xl:grid-cols-[minmax(0,1fr)_22rem]">
        <div className="flex flex-col gap-lg">
          {isAgentDown ? (
            <p className="text-body text-muted">
              Can&apos;t reach the site agent for {site.name}. Showing the last
              known state.
            </p>
          ) : current === null ? (
            <p className="text-body text-muted">
              No cars yet. Decisions appear here as the lane reads them.
            </p>
          ) : (
            <>
              <CurrentCar
                key={current.id}
                decision={current}
                photoUrl={lane.photoUrl(site.id, current.id)}
                staffPrompt={
                  <StaffPrompt
                    key={current.id}
                    plate={current.plate}
                    enabledActions={lane.enabledStaffActions(current.id)}
                    onConfirm={(plate) =>
                      lane.confirmPlate(site.id, current.id, plate)
                    }
                    onSendToPay={() => lane.sendToPay(site.id, current.id)}
                  />
                }
              />
              <EarlierCars queued={feed.queued} earlier={feed.earlier} />
            </>
          )}
        </div>
        {lookupState && (
          <div className="absolute inset-lg z-10 xl:static xl:inset-auto">
            <LookupPanel
              lookup={lookupState}
              sites={sites}
              onClose={onCloseLookup}
            />
          </div>
        )}
      </main>
    </>
  )
}

export function LaneDashboard() {
  const { kind, site } = useDataSource()
  const sites = site.sites()
  const [siteId, setSiteId] = useState(sites[0].id)
  const { lookupState, search, close } = useLookup()
  const selectedSite =
    sites.find((candidate) => candidate.id === siteId) ?? sites[0]

  return (
    <div className="min-h-svh bg-background text-text">
      <TopBar
        sites={sites}
        siteId={selectedSite.id}
        onSiteChange={setSiteId}
        onSearch={(plate) => void search(plate)}
      />
      {kind === 'replay' && (
        <p className="bg-accent px-lg py-xs text-label text-surface">
          Replay of a recorded run
        </p>
      )}
      <SiteLane
        key={selectedSite.id}
        site={selectedSite}
        sites={sites}
        lookupState={lookupState}
        onCloseLookup={close}
      />
    </div>
  )
}
