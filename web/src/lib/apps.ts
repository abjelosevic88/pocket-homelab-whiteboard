// The other Pocket apps Whiteboard works with.
import type { AppSlug, AppStateName } from './types'

export const APP_INFO: Record<AppSlug, { name: string; icon: string }> = {
  tasks: { name: 'Pocket Tasks', icon: 'check-square' }, notes: { name: 'Pocket Notes', icon: 'note' },
  chat: { name: 'Pocket Chat', icon: 'message' }, files: { name: 'Pocket Files', icon: 'archive' },
}

export const STATE_WORDS: Record<AppStateName, string> = {
  on: 'Connected', partly: 'Partly connected', off: 'Off', link_off: 'Link is off in Pocket Core', missing: 'Not on this box', old: 'Needs an update',
  no_core: 'No Pocket Core', not_set_up: 'Not set up', error: 'Not working',
}
