import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useRef, useState } from 'react'
import { useUI } from '../store/ui'
import { ApiError, request } from './client'
import * as S from './schemas'

export const keys = {
  me: ['me'] as const,
  route: (user?: string) => ['route', user ?? 'me'] as const,
  chat: ['chat'] as const,
  actions: ['actions'] as const,
  notifications: ['notifications'] as const,
  questions: ['questions'] as const,
  team: ['team'] as const,
  approvals: ['approvals'] as const,
  insights: ['insights'] as const,
  flags: (team?: string) => ['flags', team ?? 'own'] as const,
}

export function useMe() {
  return useQuery({
    queryKey: keys.me,
    queryFn: () => request('GET', '/api/me', S.MeSchema),
    retry: (n, e) => !(e instanceof ApiError && e.status === 401) && n < 2,
    retryDelay: 300,
  })
}

export function useLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { login: string; password: string }) => request('POST', '/api/auth/login', S.OkSchema, v),
    onSuccess: () => qc.invalidateQueries(),
  })
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => request('POST', '/api/auth/logout', S.OkSchema),
    onSuccess: async () => {
      // drop everything of the previous user, then re-check the session → login screen
      useUI.setState({ tab: '', viewIntern: null, draft: '', outbox: null, pane: 'chat', toasts: [] })
      await qc.cancelQueries()
      qc.removeQueries({ predicate: (q) => q.queryKey[0] !== 'me' })
      await qc.resetQueries({ queryKey: keys.me })
    },
  })
}

export function useRoute(user?: string, enabled = true) {
  return useQuery({
    queryKey: keys.route(user),
    queryFn: () => request('GET', '/api/route' + (user ? `?user=${encodeURIComponent(user)}` : ''), S.RouteSchema),
    enabled,
  })
}

/** Invalidates everything a side effect may have changed (optionally keeping the chat cache). */
export function useRefreshAll() {
  const qc = useQueryClient()
  return useCallback(
    (opts?: { keepChat?: boolean }) => {
      const ks: readonly (readonly string[])[] = [['route'], keys.actions, keys.notifications, keys.team, keys.approvals, keys.questions, keys.insights, keys.me]
      for (const k of opts?.keepChat ? ks : [...ks, keys.chat]) {
        void qc.invalidateQueries({ queryKey: k })
      }
    },
    [qc],
  )
}

export function useTaskAction() {
  const refresh = useRefreshAll()
  return useMutation({
    mutationFn: (v: { task: string; op: 'start' | 'complete' | 'nudge'; user?: string }) =>
      request('POST', `/api/route/tasks/${v.task}/${v.op}${v.user ? `?user=${encodeURIComponent(v.user)}` : ''}`, S.OkSchema),
    onSuccess: () => refresh(),
  })
}

export function useChat(enabled = true) {
  return useQuery({ queryKey: keys.chat, queryFn: () => request('GET', '/api/chat', S.ChatSchema), enabled })
}

/**
 * Sends a chat message without blocking the UI: shows the pending text,
 * supports cancellation and keeps the last error for a retry.
 */
export function useSendMessage() {
  const qc = useQueryClient()
  const refresh = useRefreshAll()
  const [pending, setPending] = useState<string | null>(null)
  const [error, setError] = useState<{ text: string; message: string } | null>(null)
  const ctrl = useRef<AbortController | null>(null)

  const send = useCallback(
    async (text: string) => {
      const t = text.trim()
      if (!t || ctrl.current) return
      setError(null)
      setPending(t)
      const c = new AbortController()
      ctrl.current = c
      try {
        const reply = await request('POST', '/api/chat', S.ReplySchema, { text: t }, { signal: c.signal, timeoutMs: 45000 })
        qc.setQueryData(keys.chat, (old: { messages: S.Message[] } | undefined) => ({
          messages: [...(old?.messages ?? []), reply.user, reply.buddy],
        }))
        if (reply.actions.length) {
          qc.setQueryData(keys.actions, (old: { actions: S.Action[] } | undefined) => ({
            actions: [...reply.actions, ...(old?.actions ?? []).filter((a) => !reply.actions.some((n) => n.id === a.id))],
          }))
        }
        refresh({ keepChat: true })
      } catch (e) {
        const msg = e instanceof ApiError ? e.message : 'Не удалось отправить сообщение'
        if (!(e instanceof ApiError && e.code === 'aborted')) setError({ text: t, message: msg })
      } finally {
        ctrl.current = null
        setPending(null)
      }
    },
    [qc, refresh],
  )
  const cancel = useCallback(() => ctrl.current?.abort(), [])
  return { send, cancel, pending, error, clearError: () => setError(null) }
}

export function useFeedback() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { id: string; helpful: boolean }) => request('POST', `/api/chat/messages/${v.id}/feedback`, S.MessageSchema, { helpful: v.helpful }),
    onSuccess: (m) =>
      qc.setQueryData(keys.chat, (old: { messages: S.Message[] } | undefined) => ({
        messages: (old?.messages ?? []).map((x) => (x.id === m.id ? m : x)),
      })),
  })
}

export function useActions(enabled = true) {
  return useQuery({ queryKey: keys.actions, queryFn: () => request('GET', '/api/actions', S.ActionsSchema), enabled })
}

export function useActionOp() {
  const qc = useQueryClient()
  const refresh = useRefreshAll()
  return useMutation({
    mutationFn: (v: { id: string; op: 'confirm' | 'cancel' } | { id: string; op: 'edit'; date_from: string; date_to: string; comment: string }) =>
      v.op === 'edit'
        ? request('PATCH', `/api/actions/${v.id}`, S.ActionSchema, { date_from: v.date_from, date_to: v.date_to, comment: v.comment })
        : request('POST', `/api/actions/${v.id}/${v.op}`, S.ActionSchema, {}, { timeoutMs: 30000 }),
    onSuccess: (a) => {
      qc.setQueryData(keys.actions, (old: { actions: S.Action[] } | undefined) => ({
        actions: [a, ...(old?.actions ?? []).filter((x) => x.id !== a.id)],
      }))
      refresh({ keepChat: true })
    },
  })
}

export function useNotifications(enabled = true) {
  return useQuery({
    queryKey: keys.notifications,
    queryFn: () => request('GET', '/api/notifications', S.NotificationsSchema),
    refetchInterval: 10000,
    enabled,
  })
}

export function useMarkRead() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => request('POST', '/api/notifications/read', S.OkSchema),
    onSuccess: () => qc.invalidateQueries({ queryKey: keys.notifications }),
  })
}

export function useQuestions(enabled = true) {
  return useQuery({ queryKey: keys.questions, queryFn: () => request('GET', '/api/mentor/questions', S.QuestionsSchema), enabled })
}

export function useAnswer() {
  const refresh = useRefreshAll()
  return useMutation({
    mutationFn: (v: { id: string; answer: string; publish: boolean }) =>
      request('POST', `/api/mentor/questions/${v.id}/answer`, S.OkSchema, { answer: v.answer, publish: v.publish }),
    onSuccess: () => refresh(),
  })
}

export function useTeam(enabled = true) {
  return useQuery({ queryKey: keys.team, queryFn: () => request('GET', '/api/team', S.TeamSchema), enabled })
}

export function useApprovals(enabled = true) {
  return useQuery({ queryKey: keys.approvals, queryFn: () => request('GET', '/api/approvals', S.ApprovalsSchema), enabled, refetchInterval: 15000 })
}

export function useDecide() {
  const refresh = useRefreshAll()
  return useMutation({
    mutationFn: (v: { id: string; approve: boolean; comment: string }) =>
      request('POST', `/api/approvals/${v.id}/decide`, S.ActionSchema, { approve: v.approve, comment: v.comment }, { timeoutMs: 30000 }),
    onSuccess: () => refresh(),
  })
}

export function useInsights(enabled = true) {
  return useQuery({ queryKey: keys.insights, queryFn: () => request('GET', '/api/insights', S.InsightsSchema), enabled })
}

export function useFlags(enabled = true) {
  return useQuery({ queryKey: keys.flags(), queryFn: () => request('GET', '/api/flags', S.FlagsSchema), enabled })
}

export function useSetFlag() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { key: string; enabled: boolean; scope: string }) =>
      request('PUT', `/api/flags/${v.key}`, S.FlagsSchema, { enabled: v.enabled, scope: v.scope }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['flags'] })
      void qc.invalidateQueries({ queryKey: keys.me })
    },
  })
}

export function useDemo() {
  const qc = useQueryClient()
  const onSuccess = () => qc.invalidateQueries()
  const advance = useMutation({ mutationFn: (days: number) => request('POST', '/api/demo/advance', S.DemoSchema, { days }), onSuccess })
  const reset = useMutation({ mutationFn: () => request('POST', '/api/demo/reset', S.DemoSchema), onSuccess })
  const fault = useMutation({ mutationFn: (enabled: boolean) => request('POST', '/api/demo/corp-fault', S.DemoSchema, { enabled }), onSuccess })
  return { advance, reset, fault }
}
