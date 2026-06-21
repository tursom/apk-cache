export function formatBytes(value?: number | null) {
  const n = Number(value || 0);
  if (n < 1024) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let size = n / 1024;
  let unit = units[0];
  for (let i = 1; i < units.length && size >= 1024; i += 1) {
    size /= 1024;
    unit = units[i];
  }
  return `${size.toFixed(size >= 10 ? 1 : 2)} ${unit}`;
}

export function formatTime(value?: string | null) {
  if (!value) return '';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

export function json(value: unknown) {
  return JSON.stringify(value, null, 2);
}

export function lines(value: string) {
  return value.split('\n').map(item => item.trim()).filter(Boolean);
}

export function includesAll(value: unknown, query: string) {
  if (!query) return true;
  return String(value || '').toLowerCase().includes(query.toLowerCase());
}
