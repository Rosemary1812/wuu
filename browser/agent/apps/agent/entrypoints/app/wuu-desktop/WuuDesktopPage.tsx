import type { FC } from 'react'
import { useMemo } from 'react'
import { App as WuuApp } from '@browseros/workbench-ui/renderer/App'
import '@browseros/workbench-ui/renderer/styles.css'
import { installWuuBrowserOSAdapter } from './browseros-wuu-adapter'
import 'overlayscrollbars/overlayscrollbars.css'
import './wuu-desktop.css'

export const WuuDesktopPage: FC = () => {
  useMemo(() => {
    installWuuBrowserOSAdapter()
  }, [])

  return (
    <div className="wuu-desktop-host">
      <WuuApp />
    </div>
  )
}
