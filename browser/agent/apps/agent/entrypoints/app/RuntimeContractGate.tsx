import { AlertTriangle, RotateCw } from 'lucide-react'
import type { FC, ReactNode } from 'react'
import { useEffect, useState } from 'react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import type { RuntimeContractStatus } from '@/lib/browseros/runtime-contract'
import { getRuntimeContractStatus } from '@/lib/browseros/runtime-contract'

type RuntimeContractGateProps = {
  children: ReactNode
}

export const RuntimeContractGate: FC<RuntimeContractGateProps> = ({
  children,
}) => {
  const [status, setStatus] = useState<RuntimeContractStatus | null>(null)

  useEffect(() => {
    let cancelled = false

    getRuntimeContractStatus().then((nextStatus) => {
      if (!cancelled) setStatus(nextStatus)
    })

    return () => {
      cancelled = true
    }
  }, [])

  if (status?.state === 'incompatible') {
    return <RuntimeContractError status={status} />
  }

  return (
    <>
      {children}
      {status?.state === 'compatible' ? (
        <RuntimeStatusBadge status={status} />
      ) : null}
    </>
  )
}

const RuntimeContractError: FC<{
  status: Extract<RuntimeContractStatus, { state: 'incompatible' }>
}> = ({ status }) => {
  return (
    <main className="flex min-h-dvh items-center justify-center bg-background p-6">
      <div className="w-full max-w-xl">
        <Alert variant="destructive">
          <AlertTriangle className="size-4" />
          <AlertTitle>Runtime update required</AlertTitle>
          <AlertDescription>
            <div className="space-y-3">
              <ul className="list-disc space-y-1 pl-4">
                {status.reasons.map((reason) => (
                  <li key={reason}>{reason}</li>
                ))}
              </ul>
              <div className="grid gap-1 text-xs">
                <span>Agent: {status.versions.agent ?? 'unknown'}</span>
                <span>Server: {status.versions.server ?? 'unknown'}</span>
                <span>Browser: {status.versions.browseros ?? 'unknown'}</span>
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => window.location.reload()}
              >
                <RotateCw className="size-3.5" />
                Reload
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      </div>
    </main>
  )
}

const RuntimeStatusBadge: FC<{
  status: Extract<RuntimeContractStatus, { state: 'compatible' }>
}> = ({ status }) => {
  const hasLocalOverride =
    status.localOverrides?.wuuBin ||
    status.localOverrides?.wuuSourceRoot ||
    status.localOverrides?.serverResources

  if (
    status.channel === 'stable' &&
    !hasLocalOverride &&
    !status.healthUnavailable
  ) {
    return null
  }

  const label = status.healthUnavailable
    ? 'Runtime reconnecting'
    : status.channel === 'local' || hasLocalOverride
      ? 'Local Agent'
      : 'Dogfood Agent'

  return (
    <div className="pointer-events-none fixed right-3 bottom-3 z-50 rounded border bg-background/90 px-2 py-1 text-muted-foreground text-xs shadow-sm backdrop-blur">
      {label}
    </div>
  )
}
