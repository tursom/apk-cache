import type { ReactNode } from 'react';
import { AlertTriangle, CheckCircle2, X } from 'lucide-react';
import type { ToastState } from './types';

export function Page({ title, actions, children }: { title: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="page">
      <div className="page-head">
        <h2>{title}</h2>
        {actions ? <div className="toolbar">{actions}</div> : null}
      </div>
      {children}
    </section>
  );
}

export function Panel({ title, children, className = '' }: { title: string; children: ReactNode; className?: string }) {
  return (
    <section className={`panel ${className}`}>
      <h3>{title}</h3>
      <div className="body">{children}</div>
    </section>
  );
}

export function Metric({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="panel metric">
      <div className="label">{label}</div>
      <div className="value">{value}</div>
    </div>
  );
}

export function StatusBadge({ value, tone = 'ok' }: { value: ReactNode; tone?: 'ok' | 'warn' | 'error' }) {
  return <span className={`status ${tone}`}>{value}</span>;
}

export function DataTable({
  columns,
  rows,
  className = ''
}: {
  columns: string[];
  rows: ReactNode[][];
  className?: string;
}) {
  return (
    <div className="table-wrap">
      <table className={className}>
        <thead>
          <tr>{columns.map(column => <th key={column}>{column}</th>)}</tr>
        </thead>
        <tbody>
          {rows.length ? (
            rows.map((row, index) => (
              <tr key={index}>{row.map((cell, cellIndex) => <td key={cellIndex}>{cell}</td>)}</tr>
            ))
          ) : (
            <tr>
              <td colSpan={columns.length}>
                <div className="empty">暂无数据</div>
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

export function Code({ children }: { children: ReactNode }) {
  return <code className="breakable">{children}</code>;
}

export function JsonBlock({ value }: { value: unknown }) {
  return <pre>{JSON.stringify(value, null, 2)}</pre>;
}

export function Toast({ toast, onClose }: { toast: ToastState | null; onClose: () => void }) {
  if (!toast) return null;
  return (
    <div className={`toast ${toast.tone}`}>
      {toast.tone === 'ok' ? <CheckCircle2 size={16} /> : <AlertTriangle size={16} />}
      <span>{toast.message}</span>
      <button className="icon-button" type="button" onClick={onClose} aria-label="关闭通知">
        <X size={14} />
      </button>
    </div>
  );
}

export function Loading() {
  return <p className="muted">Loading...</p>;
}

export function ErrorMessage({ message }: { message: string }) {
  return <p className="error">{message}</p>;
}
