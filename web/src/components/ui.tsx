import type { ReactNode } from 'react'

export function Badge({ tone = 'neutral', children }: { tone?: string; children: ReactNode }) {
  return <span className={`badge badge-${tone}`}>{children}</span>
}

export function Progress({ value, label }: { value: number; label?: string }) {
  const v = Math.max(0, Math.min(100, value))
  return (
    <div className="progress" role="progressbar" aria-valuenow={v} aria-valuemin={0} aria-valuemax={100} aria-label={label}>
      <div className="progress-fill" style={{ width: `${v}%` }} />
    </div>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <div className="empty">{children}</div>
}

export function Spinner({ label = 'Загрузка…' }: { label?: string }) {
  return (
    <div className="spinner" role="status">
      <span className="dot" />
      <span className="dot" />
      <span className="dot" />
      <span className="sr-only">{label}</span>
    </div>
  )
}

export function ErrorBox({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const msg = error instanceof Error ? error.message : 'Ошибка'
  return (
    <div className="errorbox" role="alert">
      <span>{msg}</span>
      {onRetry && (
        <button className="btn btn-ghost btn-sm" onClick={onRetry}>
          Повторить
        </button>
      )}
    </div>
  )
}
