// The shapes the API sends.

export interface User { id: number; email: string; name: string; role: 'admin' | 'member'; created_at: string }
export interface APIToken { id: number; user_id: number; name: string; prefix: string; last_used_at: string | null; created_at: string }

export type ConnMode = 'core' | 'direct' | 'off'
export type ConnVia = 'core' | 'direct' | ''
export type AppSlug = 'tasks' | 'notes' | 'chat' | 'files'
type ConnFields = { [K in AppSlug as `${K}_mode`]: ConnMode } & { [K in AppSlug as `${K}_via`]: ConnVia } & { [K in AppSlug as `${K}_detail` | `${K}_url` | `${K}_token` | `${K}_public_url` | `${K}_open_url`]: string } & { [K in AppSlug as `${K}_configured`]: boolean }

export type Settings = ConnFields & {
  core_configured: boolean
  core_registered?: boolean
  title: string; date_format: string; time_format: string; week_start: number
  background: Background; sticky_color: string; snap: boolean; cursors: boolean; wheel_zoom: boolean
  notes_folder: string; files_folder: string; chat_to: string; ai_help: boolean; notify_shared: boolean; notify_mentions: boolean; notify_comments: boolean
  notify_url: string; notify_format: string; notify_configured: boolean
  ai_provider: 'auto' | 'pocket' | 'openai'; ai_pocket_model: string; openai_api_key: string; openai_model: string; openai_base_url: string
  setup_complete: boolean; base_url: string; demo: boolean
}

export type AppStateName = 'on' | 'partly' | 'off' | 'link_off' | 'missing' | 'old' | 'no_core' | 'not_set_up' | 'error'
export interface AppState { slug: AppSlug; name: string; reason: string; mode: ConnMode; via: ConnVia; state: AppStateName; detail: string; fix: string; on: string[]; open_url: string }

export type Background = 'dots' | 'grid' | 'plain'
export type Kind = 'sticky' | 'text' | 'shape' | 'line' | 'frame' | 'pen' | 'image' | 'sticker'
export type Role = 'owner' | 'editor' | 'viewer'

export interface Vote { round: number; per_person: number; open: boolean; done: boolean; hide: boolean; started_at?: string; started_by?: string }
export interface Board {
  id: number; title: string; about: string; template: string; background: Background; share_mode: 'off' | 'view' | 'edit'; share_key?: string
  timer_ends: string; timer_secs: number; vote: Vote; version: number; owner_name: string; owner_id?: number; private?: PrivateMode; created_at: string; updated_at: string; role?: Role
}
export interface PrivateMode { on: boolean; by?: string; started_at?: string }
export interface PrivateCount { voter: string; name: string; n: number }
export interface Reaction { emoji: string; names: string[]; voters: string[] }
export interface View { x: number; y: number; w: number; h: number }
export interface Comment { id: number; board_id: number; parent_id: number | null; object_id: string; x: number; y: number; user_id: number | null; author: string; voter: string; body: string; mentions: number[]; resolved: boolean; created_at: string; updated_at: string }
export interface Version { id: number; name: string; who: string; n: number; created_at: string }
export interface Trashed { id: number; object_id: string; kind: Kind; text: string; deleted_by: string; deleted_at: string }
export interface PreviewShape { k: Kind; x: number; y: number; w: number; h: number; c?: string; s?: string }
export interface BoardRow extends Board { starred: boolean; opened_at: string; objects_n: number; members_n: number; live: number; preview?: PreviewShape[] }

/** What kind-specific things an object keeps. */
export interface ObjData {
  text?: string; color?: string; fill?: string; stroke?: string; size?: number; bold?: boolean; italic?: boolean; align?: 'left' | 'center' | 'right'
  shape?: ShapeForm; title?: string
  x1?: number; y1?: number; x2?: number; y2?: number; from?: string; to?: string; start?: 'none' | 'arrow' | 'dot'; end?: 'none' | 'arrow' | 'dot'
  curve?: 'straight' | 'elbow' | 'curve'; dash?: boolean; width?: number
  points?: number[][]; highlight?: boolean
  upload?: number; src?: string; name?: string
  locked?: boolean; tags?: string[]
  group?: string; rot?: number; html?: string; emoji?: string
}
export type ShapeForm = 'rect' | 'round' | 'ellipse' | 'diamond' | 'triangle' | 'hexagon' | 'star' | 'parallelogram'
export interface Obj { id: string; kind: Kind; x: number; y: number; w: number; h: number; z: number; data: ObjData; author?: string; user_id?: number | null; updated_by?: string; created_at?: string; updated_at?: string; hidden?: boolean }
export type Op = { op: 'put'; id?: string; obj: Obj } | { op: 'del'; id: string }

export interface Member { board_id: number; user_id: number; name: string; email: string; role: 'editor' | 'viewer'; added_at: string }
export interface Person { client: string; name: string; colour: string; voter: string; guest: boolean }
export interface Me { role: Role; can_edit: boolean; voter: string; name: string; colour: string; guest: boolean }
export interface Votes { round: number; open: boolean; done: boolean; per_person: number; hide: boolean; counts: Record<string, number>; mine: Record<string, number>; voters: number; left: number }
export interface BoardDoc { board: Board; objects: Obj[]; people: Person[]; me: Me; votes: Votes; members?: Member[]; starred?: boolean; share_url?: string; reactions?: Record<string, Reaction[]>; private_counts?: PrivateCount[] }
export interface Template { key: string; title: string; about: string; emoji: string; preview: PreviewShape[] }
export interface Summary { boards: number; mine: number; shared_with_me: number; starred: number; things: number; live_now: number; recent: { id: number; title: string; updated_at: string; objects: number; role: Role; live: number }[] }
