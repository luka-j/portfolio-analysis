import React, { useState, useEffect, useRef } from 'react';
import type { ChatThreadSummary, ChatSearchResult } from '../api';
import HoverTooltip from './HoverTooltip';

const formatRelativeTime = (dateStr: string) => {
  const d = new Date(dateStr);
  const now = new Date();
  const diffMs = now.getTime() - d.getTime();
  const diffSecs = Math.floor(diffMs / 1000);
  const diffMins = Math.floor(diffSecs / 60);
  const diffHours = Math.floor(diffMins / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffDays > 0) return `${diffDays}d ago`;
  if (diffHours > 0) return `${diffHours}h ago`;
  if (diffMins > 0) return `${diffMins}m ago`;
  return 'Just now';
};

// SVG Icon components
const SearchIcon = ({ className }: { className?: string }) => (
  <svg className={className} width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line>
  </svg>
);

const MessageSquareIcon = ({ className }: { className?: string }) => (
  <svg className={className} width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"></path>
  </svg>
);

const Trash2Icon = ({ className }: { className?: string }) => (
  <svg className={className} width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polyline points="3 6 5 6 21 6"></polyline><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path><line x1="10" y1="11" x2="10" y2="17"></line><line x1="14" y1="11" x2="14" y2="17"></line>
  </svg>
);

const Edit2Icon = ({ className }: { className?: string }) => (
  <svg className={className} width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M17 3a2.828 2.828 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5L17 3z"></path>
  </svg>
);

const CheckIcon = ({ className }: { className?: string }) => (
  <svg className={className} width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <polyline points="20 6 9 17 4 12"></polyline>
  </svg>
);

const XIcon = ({ className }: { className?: string }) => (
  <svg className={className} width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line>
  </svg>
);

const ClockIcon = ({ className }: { className?: string }) => (
  <svg className={className} width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="12" cy="12" r="10"></circle><polyline points="12 6 12 12 16 14"></polyline>
  </svg>
);

interface ChatHistorySidebarProps {
  threads: ChatThreadSummary[];
  threadsTotal: number;
  activeThreadId: number | null;
  loading: boolean;
  searchMode: boolean;
  searchQuery: string;
  searchResults: ChatSearchResult[];
  onSelectThread: (id: number) => void;
  onLoadMore: () => void;
  onSearchToggle: (active: boolean) => void;
  onSearchQuery: (query: string) => void;
  onDeleteThread: (id: number) => void;
  onRenameThread: (id: number, newTitle: string) => void;
}

export function ChatHistorySidebar({
  threads,
  threadsTotal,
  activeThreadId,
  loading,
  searchMode,
  searchQuery,
  searchResults,
  onSelectThread,
  onLoadMore,
  onSearchToggle,
  onSearchQuery,
  onDeleteThread,
  onRenameThread,
}: ChatHistorySidebarProps) {
  const [editingId, setEditingId] = useState<number | null>(null);
  const [editTitle, setEditTitle] = useState('');
  const searchInputRef = useRef<HTMLInputElement>(null);
  const observerTarget = useRef<HTMLDivElement>(null);
  
  // Focus search input when mode activates
  useEffect(() => {
    if (searchMode && searchInputRef.current) {
      searchInputRef.current.focus();
    }
  }, [searchMode]);

  // Intersection observer for infinite scroll
  useEffect(() => {
    const target = observerTarget.current;
    if (!target) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0].isIntersecting && threads.length < threadsTotal && !loading && !searchMode) {
          onLoadMore();
        }
      },
      { threshold: 0.1 }
    );

    observer.observe(target);
    return () => observer.unobserve(target);
  }, [observerTarget, threads.length, threadsTotal, loading, searchMode, onLoadMore]);

  const handleEditStart = (e: React.MouseEvent, thread: ChatThreadSummary) => {
    e.stopPropagation();
    setEditingId(thread.ID);
    setEditTitle(thread.Title);
  };

  const handleEditCancel = (e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    setEditingId(null);
    setEditTitle('');
  };

  const handleEditSave = (e?: React.MouseEvent) => {
    if (e) e.stopPropagation();
    if (editingId && editTitle.trim()) {
      onRenameThread(editingId, editTitle.trim());
    }
    setEditingId(null);
  };

  const handleEditKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      handleEditSave();
    } else if (e.key === 'Escape') {
      e.preventDefault();
      handleEditCancel();
    }
  };

  const handleDelete = (e: React.MouseEvent, id: number) => {
    e.stopPropagation();
    onDeleteThread(id);
  };

  // Standard history view
  const renderThreads = () => {
    if (threads.length === 0 && !loading) {
      return (
        <div className="px-4 py-8 text-center text-sm text-slate-500">
          No chat history yet.
        </div>
      );
    }

    return (
      <div className="flex flex-col">
        {threads.map((thread) => {
          const isActive = activeThreadId === thread.ID;
          const isEditing = editingId === thread.ID;
          
          return (
            <div
              key={thread.ID}
              onClick={() => !isEditing && onSelectThread(thread.ID)}
              className={`
                group relative flex items-start gap-3 px-4 py-3 cursor-pointer text-left
                hover:bg-slate-800/50 transition-colors border-l-2
                ${isActive ? 'border-indigo-500 bg-slate-800/30' : 'border-transparent'}
              `}
            >
              <MessageSquareIcon className={`w-4 h-4 mt-1 shrink-0 ${isActive ? 'text-indigo-400' : 'text-slate-500 group-hover:text-slate-400'}`} />
              
              <div className="flex-1 min-w-0">
                {isEditing ? (
                  <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                    <input
                      type="text"
                      autoFocus
                      value={editTitle}
                      onChange={(e) => setEditTitle(e.target.value)}
                      onKeyDown={handleEditKeyDown}
                      className="flex-1 bg-slate-900 border border-slate-700 rounded px-2 py-1 text-sm text-slate-200 outline-none focus:border-indigo-500"
                    />
                    <button onClick={handleEditSave} className="p-1 hover:text-emerald-400 text-slate-400">
                      <CheckIcon className="w-4 h-4" />
                    </button>
                    <button onClick={handleEditCancel} className="p-1 hover:text-rose-400 text-slate-400">
                      <XIcon className="w-4 h-4" />
                    </button>
                  </div>
                ) : (
                  <div className={`text-sm leading-tight font-medium line-clamp-2 ${isActive ? 'text-slate-200' : 'text-slate-400 group-hover:text-slate-300'}`}>
                    {thread.Title || 'New Conversation'}
                  </div>
                )}
                
                {!isEditing && (
                  <div className="flex items-center gap-1 mt-1.5 text-xs text-slate-500">
                    <ClockIcon className="w-3 h-3" />
                    <span>{formatRelativeTime(thread.UpdatedAt)}</span>
                  </div>
                )}
              </div>

              {!isEditing && (
                <div className="absolute right-2 top-1/2 -translate-y-1/2 hidden group-hover:flex items-center gap-0 bg-slate-900 border border-slate-700/60 rounded-md shadow-lg">
                  <div className="relative group/action">
                    <button 
                      onClick={(e) => handleEditStart(e, thread)}
                      className="p-1.5 text-slate-400 hover:text-slate-100 hover:bg-slate-700/60 rounded-l-md transition-colors"
                    >
                      <Edit2Icon className="w-3 h-3" />
                    </button>
                    <HoverTooltip direction="up" align="right" className="whitespace-nowrap !opacity-0 group-hover/action:!opacity-100">
                      Rename
                    </HoverTooltip>
                  </div>
                  <div className="relative group/action">
                    <button 
                      onClick={(e) => handleDelete(e, thread.ID)}
                      className="p-1.5 text-slate-400 hover:text-rose-300 hover:bg-rose-900/40 rounded-r-md transition-colors"
                    >
                      <Trash2Icon className="w-3 h-3" />
                    </button>
                    <HoverTooltip direction="up" align="right" className="whitespace-nowrap !opacity-0 group-hover/action:!opacity-100">
                      Delete
                    </HoverTooltip>
                  </div>
                </div>
              )}
            </div>
          );
        })}
        
        {/* Infinite scroll sentinel */}
        {!searchMode && threads.length < threadsTotal && (
          <div ref={observerTarget} className="py-4 text-center">
            {loading && <div className="text-xs text-slate-500">Loading more...</div>}
          </div>
        )}
      </div>
    );
  };

  // Search results view
  const renderSearchResults = () => {
    if (loading) {
      return <div className="px-4 py-8 text-center text-sm text-slate-500">Searching...</div>;
    }
    
    if (searchQuery && searchResults.length === 0) {
      return <div className="px-4 py-8 text-center text-sm text-slate-500">No results found for "{searchQuery}"</div>;
    }

    return (
      <div className="flex flex-col">
        {searchResults.map((res) => {
          const isActive = activeThreadId === res.thread_id;
          return (
            <div
              key={res.thread_id}
              onClick={() => onSelectThread(res.thread_id)}
              className={`
                group flex items-start gap-3 px-4 py-3 cursor-pointer text-left
                hover:bg-slate-800/50 transition-colors border-l-2
                ${isActive ? 'border-indigo-500 bg-slate-800/30' : 'border-transparent'}
              `}
            >
              <MessageSquareIcon className={`w-4 h-4 mt-1 shrink-0 ${isActive ? 'text-indigo-400' : 'text-slate-500 group-hover:text-slate-400'}`} />
              <div className="flex-1 min-w-0">
                <div className={`text-sm font-medium truncate ${isActive ? 'text-slate-200' : 'text-slate-300'}`}>
                  {res.title || 'New Conversation'}
                </div>
                {res.snippet && (
                  <div className="text-xs text-slate-400 mt-1 line-clamp-2 italic">
                    "{res.snippet}"
                  </div>
                )}
              </div>
            </div>
          );
        })}
      </div>
    );
  };

  return (
    <div className="flex flex-col w-full min-w-0 border-t border-slate-800/50 pt-4 mt-4">
      {/* Header / Search Toggle */}
      <div className="px-4 mb-3 flex items-center justify-between h-8 min-w-0">
        {searchMode ? (
          <div className="flex items-center flex-1 min-w-0 bg-slate-900 rounded border border-slate-700/50 focus-within:border-indigo-500/50 transition-colors">
            <SearchIcon className="w-4 h-4 text-slate-500 ml-2" />
            <input
              ref={searchInputRef}
              type="text"
              value={searchQuery}
              onChange={(e) => onSearchQuery(e.target.value)}
              placeholder="Search history..."
              className="flex-1 bg-transparent border-none text-sm text-slate-300 px-2 py-1.5 focus:outline-none"
              onKeyDown={(e) => {
                if (e.key === 'Escape') onSearchToggle(false);
              }}
            />
            <button 
              onClick={() => onSearchToggle(false)}
              className="p-1.5 text-slate-500 hover:text-slate-300 mr-0.5"
            >
              <XIcon className="w-3.5 h-3.5" />
            </button>
          </div>
        ) : (
          <>
            <h3 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">History</h3>
            <button 
              onClick={() => onSearchToggle(true)}
              className="p-1.5 text-slate-500 hover:text-slate-300 hover:bg-slate-800 rounded transition-colors"
              title="Search history"
            >
              <SearchIcon className="w-4 h-4" />
            </button>
          </>
        )}
      </div>

      {/* Scrollable list */}
      <div className="flex-1 overflow-y-auto min-h-0 pb-4 custom-scrollbar">
        {searchMode ? renderSearchResults() : renderThreads()}
      </div>
    </div>
  );
}
