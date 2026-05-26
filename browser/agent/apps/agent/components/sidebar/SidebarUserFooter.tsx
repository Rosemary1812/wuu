import { Info, Keyboard } from 'lucide-react'
import type { FC } from 'react'
import { Button } from '@/components/ui/button'
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

interface SidebarUserFooterProps {
  expanded?: boolean
  onOpenShortcuts?: () => void
}

export const SidebarUserFooter: FC<SidebarUserFooterProps> = ({
  expanded = true,
  onOpenShortcuts,
}) => {
  const aboutLink = (
    <a
      href="https://github.com/blueberrycongee/wuu"
      target="_blank"
      rel="noopener noreferrer"
      className="flex h-9 items-center gap-2 overflow-hidden whitespace-nowrap rounded-md px-3 font-medium text-sm transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
    >
      <Info className="size-4 shrink-0" />
      <span
        className={cn(
          'truncate transition-opacity duration-200',
          expanded ? 'opacity-100' : 'opacity-0',
        )}
      >
        About Wuu
      </span>
    </a>
  )

  const shortcutsButton = (
    <Button
      variant="ghost"
      onClick={onOpenShortcuts}
      className="flex h-9 w-full items-center justify-start gap-2 overflow-hidden whitespace-nowrap rounded-md px-3 font-medium text-sm transition-colors hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
    >
      <Keyboard className="size-4 shrink-0" />
      <span
        className={cn(
          'truncate transition-opacity duration-200',
          expanded ? 'opacity-100' : 'opacity-0',
        )}
      >
        Shortcuts
      </span>
    </Button>
  )

  return (
    <TooltipProvider delayDuration={0}>
      <div className="mt-auto space-y-1 border-t p-2">
        {expanded ? (
          shortcutsButton
        ) : (
          <Tooltip>
            <TooltipTrigger asChild>{shortcutsButton}</TooltipTrigger>
            <TooltipContent side="right">Shortcuts</TooltipContent>
          </Tooltip>
        )}

        {expanded ? (
          aboutLink
        ) : (
          <Tooltip>
            <TooltipTrigger asChild>{aboutLink}</TooltipTrigger>
            <TooltipContent side="right">About Wuu</TooltipContent>
          </Tooltip>
        )}
      </div>
    </TooltipProvider>
  )
}
