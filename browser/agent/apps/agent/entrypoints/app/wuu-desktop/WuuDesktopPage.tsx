import type { FC } from 'react'
import { useMemo } from 'react'
import { installWuuBrowserOSAdapter } from './browseros-wuu-adapter'
import { App as WuuApp } from './renderer/App'
import 'overlayscrollbars/overlayscrollbars.css'
import './renderer/styles.css'
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
