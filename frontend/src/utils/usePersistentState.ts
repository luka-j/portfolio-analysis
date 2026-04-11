import { useState, useEffect, useCallback } from 'react';

// In-process event bus so all components sharing the same key stay in sync.
const listeners = new Map<string, Set<(v: unknown) => void>>();

function subscribe(key: string, fn: (v: unknown) => void) {
  if (!listeners.has(key)) listeners.set(key, new Set());
  listeners.get(key)!.add(fn);
  return () => listeners.get(key)?.delete(fn);
}

function broadcast(key: string, value: unknown) {
  listeners.get(key)?.forEach(fn => fn(value));
}

function readStorage<T>(key: string, initial: T): T {
  try {
    const item = window.localStorage.getItem(key);
    return item !== null ? (JSON.parse(item) as T) : initial;
  } catch {
    return initial;
  }
}

/**
 * Like useState, but persisted to localStorage and reactive across all
 * components that share the same key — including across browser tabs.
 */
export function usePersistentState<T>(key: string, initialValue: T): [T, (val: T | ((prev: T) => T)) => void] {
  const [state, setState] = useState<T>(() => readStorage(key, initialValue));

  // Sync when another component (or tab) writes the same key.
  useEffect(() => {
    const unsub = subscribe(key, (v) => setState(v as T));

    // Cross-tab sync via the native storage event.
    const onStorage = (e: StorageEvent) => {
      if (e.key === key && e.newValue !== null) {
        try { setState(JSON.parse(e.newValue) as T); } catch { /* ignore */ }
      }
    };
    window.addEventListener('storage', onStorage);

    return () => {
      unsub();
      window.removeEventListener('storage', onStorage);
    };
  }, [key]);

  const setValue = useCallback((val: T | ((prev: T) => T)) => {
    setState(prev => {
      const next = val instanceof Function ? val(prev) : val;
      try {
        window.localStorage.setItem(key, JSON.stringify(next));
        broadcast(key, next);
      } catch { /* ignore */ }
      return next;
    });
  }, [key]);

  return [state, setValue];
}
