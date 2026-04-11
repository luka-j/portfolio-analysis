/** ErrorAlert renders a standard error banner. Returns null when message is empty. */
export default function ErrorAlert({ message, className = '' }: { message: string; className?: string }) {
  if (!message) return null
  return (
    <div className={`w-full px-5 py-3.5 rounded-2xl bg-red-500/10 text-red-400 text-sm border border-red-500/20 text-center ${className}`}>
      {message}
    </div>
  )
}
