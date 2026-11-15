import { create } from 'zustand'

export type Pane = 'route' | 'chat' | 'now'

export interface Toast {
  id: number
  text: string
  kind: 'info' | 'error' | 'success'
}

interface UIState {
  tab: string
  setTab: (t: string) => void
  pane: Pane
  setPane: (p: Pane) => void
  viewIntern: string | null
  setViewIntern: (id: string | null) => void
  draft: string
  setDraft: (s: string) => void
  /** text queued by buttons outside the chat (route, cards) to be sent by the chat */
  outbox: string | null
  ask: (text: string) => void
  takeOutbox: () => string | null
  toasts: Toast[]
  toast: (text: string, kind?: Toast['kind']) => void
  dismiss: (id: number) => void
}

let seq = 0

export const useUI = create<UIState>((set, get) => ({
  tab: '',
  setTab: (tab) => set({ tab }),
  pane: 'chat',
  setPane: (pane) => set({ pane }),
  viewIntern: null,
  setViewIntern: (viewIntern) => set({ viewIntern }),
  draft: '',
  setDraft: (draft) => set({ draft }),
  outbox: null,
  ask: (text) => set({ outbox: text, pane: 'chat' }),
  takeOutbox: () => {
    const o = get().outbox
    if (o !== null) set({ outbox: null })
    return o
  },
  toasts: [],
  toast: (text, kind = 'info') => {
    const id = ++seq
    set((s) => ({ toasts: [...s.toasts, { id, text, kind }] }))
    setTimeout(() => get().dismiss(id), 5000)
  },
  dismiss: (id) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) })),
}))
