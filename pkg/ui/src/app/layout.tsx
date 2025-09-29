import type { Metadata } from 'next'
import './globals.css'

export const metadata: Metadata = {
  title: 'Podboard',
  description: 'Kubernetes Pod Dashboard - Monitor pod status and deployments',
  icons: {
    icon: '/favicon.ico',
  },
}

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode
}>) {
  return (
    <html lang="en">
      <body>
        {children}
      </body>
    </html>
  )
}