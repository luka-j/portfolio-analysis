import NavBar from './NavBar'

export default function PageLayout({
  children,
  maxWidth = 'max-w-7xl',
  mainClassName = '',
}: {
  children: React.ReactNode
  maxWidth?: string
  mainClassName?: string
}) {
  return (
    <div className="min-h-screen bg-[#0f1117] flex flex-col">
      <NavBar />
      <div className="w-full flex-1 flex justify-center">
        <main className={`py-6 px-4 md:py-10 md:px-12 ${maxWidth} w-full flex flex-col items-center ${mainClassName}`}>
          {children}
        </main>
      </div>
    </div>
  )
}
