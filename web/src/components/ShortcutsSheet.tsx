import { Modal } from './ui'

const GROUPS: { title: string; keys: [string[], string][] }[] = [
  { title: 'Go to', keys: [[['h'], 'All boards'], [['s'], 'Starred'], [[','], 'Settings']] },
  { title: 'Make', keys: [[['n'], 'New board']] },
  { title: 'Find', keys: [[['/'], 'Search & commands'], [['⌘', 'K'], 'Search & commands']] },
  { title: 'Window', keys: [[['['], 'Show or hide the sidebar'], [['esc'], 'Close']] },
  { title: 'On a board', keys: [[['n'], 'Sticky note'], [['t'], 'Text'], [['s'], 'Shape'], [['l'], 'Line'], [['p'], 'Pen'], [['f'], 'Frame'], [['?'], 'All board shortcuts']] },
  { title: 'Help', keys: [[['?'], 'This sheet']] },
]

export function ShortcutsSheet({ onClose }: { onClose: () => void }) {
  return (
    <Modal title="Keyboard shortcuts" onClose={onClose} size="lg">
      <div className="shortcuts">
        {GROUPS.map(g => (
          <section key={g.title}>
            <h4 className="sc-h">{g.title}</h4>
            {g.keys.map(([keys, label]) => (
              <div key={label + keys.join('')} className="sc-row"><span>{label}</span><span className="sc-keys">{keys.map(k => <kbd key={k}>{k}</kbd>)}</span></div>
            ))}
          </section>
        ))}
      </div>
    </Modal>
  )
}
