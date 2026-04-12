export default function Spinner({ label, className = 'py-20' }: {
  label?: string
  className?: string
}) {
  return (
    <div className={`flex flex-col items-center justify-center gap-4 text-slate-500 ${className}`}>
      <div className="w-6 h-6 border-2 border-indigo-500 border-t-transparent rounded-full animate-spin" />
      {label && <span className="text-[10px] font-black uppercase tracking-widest">{label}</span>}
    </div>
  )
}
