import { useState } from 'react';
import { deleteTransaction, ImportedTransaction } from '../api';
import Spinner from './Spinner';
import HoverTooltip from './HoverTooltip';

interface Props {
  open: boolean;
  uploading: boolean;
  error: string | null;
  transactions: ImportedTransaction[];
  onClose: () => void;
}

type DedupeState = 'pending' | 'accepting' | 'accepted' | 'rejected';

function sideBadge(side: string) {
  const isBuy =
    side === 'BUY' || side === 'TRANSFER_IN' || side === 'ESPP_VEST' || side === 'RSU_VEST';
  const isSell = side === 'SELL' || side === 'TRANSFER_OUT';
  const cls = isBuy
    ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20'
    : isSell
      ? 'bg-red-500/10 text-red-400 border-red-500/20'
      : 'bg-slate-500/10 text-slate-500 border-slate-500/20';
  return (
    <span
      className={`px-2.5 py-0.5 rounded-2xl text-[9px] font-black uppercase tracking-[0.15em] border ${cls}`}
    >
      {side}
    </span>
  );
}

function formatNumber(n: number) {
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 2, minimumFractionDigits: 2 }).format(n);
}

function formatQty(n: number) {
  const abs = Math.abs(n);
  return new Intl.NumberFormat('en-US', { maximumFractionDigits: 6 }).format(abs);
}

export default function UploadResultModal({ open, uploading, error, transactions, onClose }: Props) {
  const [dedupeStates, setDedupeStates] = useState<Record<string, DedupeState>>({});
  const [bulkAccepting, setBulkAccepting] = useState(false);

  if (!open) return null;

  const hasSuspected = transactions.some((t) => t.suspected_duplicate_id != null);
  const pendingSuspected = transactions.filter(
    (t) => t.suspected_duplicate_id != null && (dedupeStates[t.id] ?? 'pending') === 'pending',
  );

  const newCount = transactions.filter((t) => !t.is_duplicate && !t.suspected_duplicate_id).length;
  const dupCount = transactions.filter((t) => t.is_duplicate).length;
  const reviewCount = transactions.filter((t) => t.suspected_duplicate_id != null).length;

  function summaryText() {
    const parts: string[] = [];
    if (newCount > 0) parts.push(`${newCount} new`);
    if (dupCount > 0) parts.push(`${dupCount} duplicate${dupCount !== 1 ? 's' : ''}`);
    if (reviewCount > 0) parts.push(`${reviewCount} need review`);
    return parts.join(' · ');
  }

  async function handleAccept(txn: ImportedTransaction) {
    if (!txn.suspected_duplicate_id) return;
    setDedupeStates((s) => ({ ...s, [txn.id]: 'accepting' }));
    try {
      await deleteTransaction(txn.suspected_duplicate_id);
      setDedupeStates((s) => ({ ...s, [txn.id]: 'accepted' }));
    } catch {
      setDedupeStates((s) => ({ ...s, [txn.id]: 'pending' }));
    }
  }

  function handleReject(txn: ImportedTransaction) {
    setDedupeStates((s) => ({ ...s, [txn.id]: 'rejected' }));
  }

  async function handleAcceptAll() {
    setBulkAccepting(true);
    await Promise.all(pendingSuspected.map((t) => handleAccept(t)));
    setBulkAccepting(false);
    onClose();
  }

  return (
    <div
      className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50"
      onClick={onClose}
    >
      <div
        className="bg-panel/95 border border-white/8 rounded-2xl shadow-2xl backdrop-blur-xl ring-1 ring-white/5 max-w-4xl w-full mx-4 max-h-[85vh] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-baseline justify-between px-6 pt-6 pb-4 border-b border-white/5 shrink-0">
          <div>
            <h2 className="text-sm font-black text-slate-100 uppercase tracking-widest">
              Import Results
            </h2>
            {!uploading && !error && transactions.length > 0 && (
              <p className="text-[10px] text-slate-500 font-bold mt-0.5 uppercase tracking-wider">
                {summaryText()}
              </p>
            )}
          </div>
          <button
            onClick={onClose}
            className="text-slate-500 hover:text-slate-300 transition-colors p-1 rounded-lg hover:bg-white/5"
            aria-label="Close"
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
              <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
            </svg>
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto">
          {uploading ? (
            <Spinner label="Processing upload…" className="py-20" />
          ) : error ? (
            <div className="px-6 py-8 text-center">
              <p className="text-red-400 text-sm">{error}</p>
            </div>
          ) : transactions.length === 0 ? (
            <div className="px-6 py-16 text-center text-slate-500 text-xs">No transactions processed.</div>
          ) : (
            <table className="w-full text-xs">
              <thead className="sticky top-0 bg-panel/95 backdrop-blur-sm">
                <tr className="text-[9px] font-black text-slate-500 uppercase tracking-widest border-b border-white/5">
                  <th className="text-left px-6 py-3">Ticker</th>
                  <th className="text-left px-3 py-3">Date</th>
                  <th className="text-left px-3 py-3">Side</th>
                  <th className="text-right px-3 py-3">Qty</th>
                  <th className="text-right px-3 py-3">Price</th>
                  <th className="text-right px-3 py-3">Total</th>
                  <th className="text-center px-3 py-3">Status</th>
                  {hasSuspected && <th className="text-right px-6 py-3">Action</th>}
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {transactions.map((txn) => {
                  const state = dedupeStates[txn.id] ?? 'pending';
                  return (
                    <tr key={txn.id} className="hover:bg-white/[0.02] transition-colors">
                      <td className="px-6 py-2.5 font-black text-slate-100">{txn.symbol}</td>
                      <td className="px-3 py-2.5 text-slate-400 tabular-nums">{txn.date}</td>
                      <td className="px-3 py-2.5">{sideBadge(txn.side)}</td>
                      <td className="px-3 py-2.5 text-right text-slate-200 tabular-nums font-bold">
                        {formatQty(txn.quantity)}
                      </td>
                      <td className="px-3 py-2.5 text-right text-slate-400 tabular-nums">
                        {formatNumber(txn.price)}{' '}
                        <span className="text-slate-600 text-[9px]">{txn.currency}</span>
                      </td>
                      <td className="px-3 py-2.5 text-right text-slate-300 tabular-nums font-bold">
                        {formatNumber(txn.total_cost)}
                      </td>
                      <td className="px-3 py-2.5 text-center">
                        <StatusBadge txn={txn} state={state} />
                      </td>
                      {hasSuspected && (
                        <td className="px-6 py-2.5 text-right">
                          {txn.suspected_duplicate_id == null ? null : (
                            <ActionCell
                              state={state}
                              onAccept={() => handleAccept(txn)}
                              onReject={() => handleReject(txn)}
                            />
                          )}
                        </td>
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>

        {/* Footer */}
        {!uploading && !error && (
          <div className="px-6 py-4 border-t border-white/5 shrink-0 flex items-center justify-between gap-3">
            {hasSuspected && pendingSuspected.length > 0 ? (
              <>
                <p className="text-[10px] text-slate-500 font-bold uppercase tracking-wider">
                  Review suspected duplicates — accept to remove the original manual entry.
                </p>
                <div className="flex gap-2 shrink-0">
                  <button
                    onClick={onClose}
                    disabled={bulkAccepting}
                    className="px-4 py-2 rounded-xl text-[10px] font-black uppercase tracking-widest text-slate-400 hover:text-slate-200 border border-white/8 hover:bg-white/5 transition-all disabled:opacity-40"
                  >
                    Keep remaining dups
                  </button>
                  <button
                    onClick={handleAcceptAll}
                    disabled={bulkAccepting}
                    className="flex items-center gap-2 px-4 py-2 rounded-xl text-[10px] font-black uppercase tracking-widest bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 hover:bg-emerald-500/20 transition-all disabled:opacity-40"
                  >
                    {bulkAccepting ? (
                      <div className="w-3.5 h-3.5 border-2 border-emerald-400 border-t-transparent rounded-full animate-spin" />
                    ) : null}
                    Remove remaining dups
                  </button>
                </div>
              </>
            ) : (
              <div className="flex justify-end w-full">
                <button
                  onClick={onClose}
                  className="px-5 py-2 rounded-xl text-[10px] font-black uppercase tracking-widest text-slate-300 border border-white/8 hover:bg-white/5 hover:text-slate-100 transition-all"
                >
                  Close
                </button>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

function StatusBadge({ txn, state }: { txn: ImportedTransaction; state: DedupeState }) {
  if (txn.suspected_duplicate_id != null) {
    if (state === 'accepted') {
      return (
        <span className="px-2 py-0.5 rounded-xl text-[9px] font-black uppercase tracking-widest border bg-emerald-500/10 text-emerald-400 border-emerald-500/20">
          Removed
        </span>
      );
    }
    if (state === 'rejected') {
      return (
        <span className="px-2 py-0.5 rounded-xl text-[9px] font-black uppercase tracking-widest border bg-slate-500/10 text-slate-400 border-slate-500/20">
          Kept both
        </span>
      );
    }
    return (
      <span className="px-2 py-0.5 rounded-xl text-[9px] font-black uppercase tracking-widest border bg-amber-500/10 text-amber-400 border-amber-500/20">
        Suspected dup
      </span>
    );
  }
  if (txn.is_duplicate) {
    return (
      <div className="relative group inline-flex">
        <span className="px-2 py-0.5 rounded-xl text-[9px] font-black uppercase tracking-widest border bg-slate-500/10 text-slate-500 border-slate-500/20 cursor-default">
          Duplicate
        </span>
        <HoverTooltip align="right" className="whitespace-nowrap">
          {txn.confident_dedup ? 'Matched by Transaction ID' : 'Matched by value'}
        </HoverTooltip>
      </div>
    );
  }
  return (
    <span className="px-2 py-0.5 rounded-xl text-[9px] font-black uppercase tracking-widest border bg-emerald-500/10 text-emerald-400 border-emerald-500/20">
      New
    </span>
  );
}

function ActionCell({
  state,
  onAccept,
  onReject,
}: {
  state: DedupeState;
  onAccept: () => void;
  onReject: () => void;
}) {
  if (state === 'accepting') {
    return (
      <div className="flex justify-end">
        <div className="w-4 h-4 border-2 border-indigo-400 border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }
  if (state === 'accepted' || state === 'rejected') return null;
  return (
    <div className="flex items-center justify-end gap-1.5">
      <button
        onClick={onAccept}
        className="px-2.5 py-1 rounded-lg text-[9px] font-black uppercase tracking-widest bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 hover:bg-emerald-500/20 transition-all"
      >
        Remove dup
      </button>
      <button
        onClick={onReject}
        className="px-2.5 py-1 rounded-lg text-[9px] font-black uppercase tracking-widest text-slate-500 border border-white/8 hover:text-slate-300 hover:bg-white/5 transition-all"
      >
        Keep dup
      </button>
    </div>
  );
}
