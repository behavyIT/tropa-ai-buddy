import { useEffect, useRef, useState } from 'react'
import { useDemo, useLogout, useMarkRead, useNotifications } from '../api/hooks'
import type { Me } from '../api/schemas'
import { roleTitle, timeAgo } from '../lib/format'
import { useUI } from '../store/ui'

export function TopBar({ me }: { me: Me }) {
  const logout = useLogout()
  const { advance, reset, fault } = useDemo()
  const toast = useUI((s) => s.toast)
  const [menu, setMenu] = useState(false)
  const busy = advance.isPending || reset.isPending || fault.isPending
  return (
    <header className="topbar">
      <div className="logo">
        Тропа<span className="logo-tag">AI-buddy</span>
      </div>
      {me.demo.enabled && (
        <div className="demo" aria-label="Демо-пульт">
          <span className="demo-day" title="Симулированное время">
            {me.user.role === 'intern' ? `День ${me.demo.day} · ` : ''}
            {me.demo.today}
          </span>
          <button
            className="btn btn-sticker btn-sm"
            disabled={busy}
            onClick={() => advance.mutate(1, { onSuccess: () => toast('Машина времени: +1 день. Сработал утренний бриф и напоминания.', 'success') })}
            title="Машина времени: перейти к следующему утру"
          >
            ⏩ +1 день
          </button>
          <div className="menu-wrap">
            <button className="btn btn-ghost btn-sm" aria-expanded={menu} onClick={() => setMenu(!menu)} aria-label="Демо-меню">
              ⋯
            </button>
            {menu && (
              <div className="menu" role="menu">
                <button role="menuitem" onClick={() => { advance.mutate(7); setMenu(false) }}>
                  +7 дней
                </button>
                <button
                  role="menuitem"
                  onClick={() => {
                    fault.mutate(!me.demo.corp_fault, {
                      onSuccess: () => toast(me.demo.corp_fault ? 'Корпоративные сервисы снова доступны' : 'Имитация сбоя корпоративных сервисов включена', 'info'),
                    })
                    setMenu(false)
                  }}
                >
                  {me.demo.corp_fault ? 'Выключить сбой сервисов' : 'Имитировать сбой сервисов'}
                </button>
                <button
                  role="menuitem"
                  onClick={() => {
                    reset.mutate(undefined, { onSuccess: () => toast('Демо сброшено к первому дню', 'success') })
                    setMenu(false)
                  }}
                >
                  Сбросить демо
                </button>
              </div>
            )}
          </div>
          {me.demo.corp_fault && <span className="badge badge-red">Сбой сервисов</span>}
        </div>
      )}
      <div className="topbar-right">
        <span className="badge badge-neutral" title="Режим ответов">
          {me.llm === 'offline' ? 'офлайн-режим' : me.llm}
        </span>
        <Bell />
        <div className="me">
          <span className="me-name">{me.user.name}</span>
          <span className="me-role">
            {roleTitle[me.user.role]}
            {me.user.team_name ? ` · ${me.user.team_name}` : ''}
          </span>
        </div>
        <button className="btn btn-ghost btn-sm" onClick={() => logout.mutate()}>
          Сменить
        </button>
      </div>
    </header>
  )
}

function Bell() {
  const q = useNotifications()
  const read = useMarkRead()
  const [open, setOpen] = useState(false)
  const unread = q.data?.unread ?? 0
  const prev = useRef(unread)
  const toast = useUI((s) => s.toast)
  useEffect(() => {
    if (unread > prev.current && q.data?.notifications[0]) toast(q.data.notifications[0].title, 'info')
    prev.current = unread
  }, [unread, q.data, toast])
  return (
    <div className="menu-wrap">
      <button
        className="btn btn-ghost btn-sm bell"
        aria-label={`Уведомления: ${unread} непрочитанных`}
        onClick={() => {
          setOpen(!open)
          if (!open && unread > 0) read.mutate()
        }}
      >
        🔔{unread > 0 && <span className="bell-count">{unread}</span>}
      </button>
      {open && (
        <div className="menu notif-menu" role="dialog" aria-label="Уведомления">
          {(q.data?.notifications ?? []).length === 0 && <div className="empty">Пока тихо</div>}
          {(q.data?.notifications ?? []).map((n) => (
            <div key={n.id} className={`notif ${n.read_at ? '' : 'notif-unread'}`}>
              <div className="notif-title">{n.title}</div>
              <div className="notif-body">{n.body}</div>
              <div className="notif-time">{timeAgo(n.created_at)}</div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
